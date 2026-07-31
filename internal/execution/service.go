package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/runtimeconfig"
	"github.com/imprun/windforce-core/internal/state"
)

type Catalog interface {
	GetDeployment(ctx context.Context, app string) (contract.Deployment, error)
}

type Store interface {
	CreateRunAndEnqueue(ctx context.Context, run state.Run, job state.Job) error
	GetRun(ctx context.Context, runID string) (state.Run, error)
	GetJobByRunID(ctx context.Context, workspaceID string, runID string) (state.Job, state.Run, bool, error)
	CancelRun(ctx context.Context, runID string, reason string) (state.Run, error)
	GetClient(ctx context.Context, workspaceID string, id string) (state.Client, error)
	GetVariable(ctx context.Context, workspaceID string, appKey string, path string) (state.Variable, bool, error)
	GetResource(ctx context.Context, workspaceID string, path string) (state.Resource, bool, error)
	ResolveInput(ctx context.Context, workspaceID string, appKey string, actionKey string, clientID string, request json.RawMessage) (json.RawMessage, error)
}

type FaultKind string

const (
	FaultUnavailable     FaultKind = "unavailable"
	FaultInvalidRequest  FaultKind = "invalid_request"
	FaultForbidden       FaultKind = "forbidden"
	FaultAppNotFound     FaultKind = "app_not_found"
	FaultActionNotFound  FaultKind = "action_not_found"
	FaultRoutingConflict FaultKind = "routing_conflict"
	FaultConflict        FaultKind = "conflict"
	FaultInternal        FaultKind = "internal"
)

type Fault struct {
	Kind    FaultKind
	Message string
	Err     error
}

func (e *Fault) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *Fault) Unwrap() error { return e.Err }

func FaultKindOf(err error) FaultKind {
	var fault *Fault
	if errors.As(err, &fault) {
		return fault.Kind
	}
	return FaultInternal
}

type AdmissionService struct {
	store   Store
	catalog Catalog
	bundles BundleStore
}

type Service = AdmissionService

func NewAdmissionService(store Store, catalog Catalog, bundles BundleStore) *AdmissionService {
	return &AdmissionService{store: store, catalog: catalog, bundles: bundles}
}

func NewService(store Store, catalog Catalog, bundles BundleStore) *AdmissionService {
	return NewAdmissionService(store, catalog, bundles)
}

type CreateRunRequest struct {
	Workspace      string
	App            string
	Action         string
	Input          json.RawMessage
	Adapter        string
	TriggerKind    string
	TriggerHeaders json.RawMessage
	CorrelationID  string
	IdempotencyKey string
	ScheduledFor   time.Time
	Env            []string
	ClientID       string
	CreatedBy      string
	PermissionedAs string
	Principal      Principal
}

type Admission struct {
	Run      state.Run
	Job      state.Job
	Replayed bool
}

type ActionDescription struct {
	Spec         contract.Action `json:"spec"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}

type AppDescription struct {
	Deployment contract.Deployment          `json:"deployment"`
	Actions    map[string]ActionDescription `json:"actions"`
}

func (s *AdmissionService) CreateRun(ctx context.Context, request CreateRunRequest) (Admission, error) {
	if s == nil || s.store == nil || s.catalog == nil {
		return Admission{}, &Fault{Kind: FaultUnavailable, Message: "admission service is not configured"}
	}
	request.Workspace = contract.NormalizeWorkspace(request.Workspace)
	request.App = strings.TrimSpace(request.App)
	request.Action = strings.TrimSpace(request.Action)
	if !contract.ValidAppKey(request.App) || !contract.ValidActionKey(request.Action) {
		return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: "invalid app/action key"}
	}
	if len(request.Input) == 0 {
		request.Input = json.RawMessage([]byte("{}"))
	}
	if !json.Valid(request.Input) {
		return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: "input must be valid JSON"}
	}

	principal := request.Principal.Normalized()
	if principal.Kind != "" {
		if principal.Workspace != request.Workspace {
			return Admission{}, &Fault{Kind: FaultForbidden, Message: "principal workspace mismatch"}
		}
		if !principal.HasScope(ScopeRunsCreate) {
			return Admission{}, &Fault{Kind: FaultForbidden, Message: "principal lacks runs:create"}
		}
		if !principal.AllowsTarget(request.App, request.Action) {
			return Admission{}, &Fault{Kind: FaultForbidden, Message: "principal is not allowed to invoke this app/action"}
		}
		if len(request.Env) > 0 {
			return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: "per-run env is not allowed"}
		}
		request.CreatedBy = principal.Subject
		request.PermissionedAs = principal.Subject
		if principal.Kind == PrincipalClient {
			request.ClientID = principal.ID
		} else {
			request.ClientID = ""
		}
	}

	clientID := strings.TrimSpace(request.ClientID)
	if clientID != "" {
		client, err := s.store.GetClient(ctx, request.Workspace, clientID)
		if err != nil {
			if errors.Is(err, state.ErrNotFound) {
				return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: "unknown client"}
			}
			return Admission{}, &Fault{Kind: FaultInternal, Message: "could not resolve client", Err: err}
		}
		clientID = client.ID
	}

	fingerprint, err := invocationRequestFingerprint(request.App, request.Action, request.Input, request.CorrelationID, request.ScheduledFor)
	if err != nil {
		return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: "input must be valid JSON", Err: err}
	}
	runID := ""
	idempotencyHash := ""
	if key := strings.TrimSpace(request.IdempotencyKey); key != "" {
		scope := "legacy"
		if principal.Kind != "" {
			scope = principal.IdempotencyScope()
		} else if clientID != "" {
			scope = "client:" + clientID
		}
		runID = deterministicRunID(request.Workspace, scope, key)
		idempotencyHash = digestString(key)
		existingJob, existingRun, found, getErr := s.store.GetJobByRunID(ctx, request.Workspace, runID)
		if getErr != nil {
			return Admission{}, &Fault{Kind: FaultInternal, Message: "could not resolve idempotent run", Err: getErr}
		}
		if found {
			return replayAdmission(existingRun, existingJob, fingerprint)
		}
	}

	deployment, err := s.lookupDeployment(ctx, request.Workspace, request.App)
	if err != nil {
		return Admission{}, &Fault{Kind: FaultAppNotFound, Message: "app not found: " + request.App, Err: err}
	}
	actionSpec, ok := deployment.Actions[request.Action]
	if !ok {
		return Admission{}, &Fault{Kind: FaultActionNotFound, Message: "action not found: " + request.App + "/" + request.Action}
	}
	if strings.TrimSpace(deployment.BundleDigest) == "" {
		return Admission{}, &Fault{
			Kind:    FaultUnavailable,
			Message: "active release has no execution bundle; publish the synchronized source again",
		}
	}
	reader := NewSchemaReader(ctx, s.bundles, deployment)
	defer reader.Close()
	resolvedInput, err := s.store.ResolveInput(ctx, request.Workspace, request.App, request.Action, clientID, request.Input)
	if err != nil {
		var locked *state.LockedKeysError
		if errors.As(err, &locked) {
			return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: locked.Error(), Err: err}
		}
		return Admission{}, &Fault{Kind: FaultInternal, Message: "could not validate input settings", Err: err}
	}
	runtimeResolver := runtimeconfig.New(s.store, nil)
	operatorSettingsSchema, err := reader.Read(actionSpec.OperatorSettingsSchema, actionSpec.OperatorSettingsSchemaBody)
	if err != nil {
		return Admission{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("operator settings schema for %s/%s: %v", request.App, request.Action, err), Err: err}
	}
	if _, err := runtimeResolver.ValidateSecretReferences(
		ctx,
		request.Workspace,
		request.App,
		operatorSettingsSchema,
		resolvedInput,
	); err != nil {
		return Admission{}, &Fault{
			Kind:    FaultInvalidRequest,
			Message: "invalid secret setting reference: " + err.Error(),
			Err:     err,
		}
	}
	runtimeAccess, err := runtimeResolver.BuildAccess(
		ctx,
		request.Workspace,
		request.App,
		actionSpec.RuntimeAccess,
		resolvedInput,
	)
	if err != nil {
		return Admission{}, &Fault{
			Kind:    FaultInvalidRequest,
			Message: "invalid runtime configuration references: " + err.Error(),
			Err:     err,
		}
	}
	actionSpec.RuntimeAccess = runtimeAccess
	deployment.Actions[request.Action] = actionSpec

	adapter := strings.TrimSpace(request.Adapter)
	if adapter == "" {
		adapter = "http"
	}
	inputSchema, err := reader.Read(actionSpec.InputSchema, actionSpec.InputSchemaBody)
	if err != nil {
		return Admission{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("input schema for %s/%s: %v", request.App, request.Action, err), Err: err}
	}
	if err := reader.Validate(actionSpec.InputSchema, inputSchema, resolvedInput); err != nil {
		var validation *InputValidationError
		if errors.As(err, &validation) {
			return Admission{}, &Fault{Kind: FaultInvalidRequest, Message: validation.Error(), Err: err}
		}
		return Admission{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("input schema for %s/%s could not be evaluated", request.App, request.Action), Err: err}
	}

	run := state.NewRun(adapter, runID, request.App, request.Action, deployment, cloneRaw(resolvedInput))
	run.InputConfigResolved = true
	run.CorrelationID = state.CleanID(request.CorrelationID)
	run.Env = cloneStrings(request.Env)
	run.CreatedBy = strings.TrimSpace(request.CreatedBy)
	run.PermissionedAs = strings.TrimSpace(request.PermissionedAs)
	run.ClientID = clientID
	run.IdempotencyHash = idempotencyHash
	run.RequestFingerprint = fingerprint
	if principal.Kind != "" {
		run.PrincipalKind = string(principal.Kind)
		run.PrincipalID = principal.ID
	}
	job := state.NewActionJob(run, cloneRaw(resolvedInput))
	job.Payload.RuntimeAccess = runtimeAccess
	job.Payload.TriggerKind = strings.TrimSpace(request.TriggerKind)
	if job.Payload.TriggerKind == "" {
		job.Payload.TriggerKind = adapter
	}
	job.Payload.TriggerHeaders = cloneRaw(request.TriggerHeaders)
	if !request.ScheduledFor.IsZero() {
		job.Payload.ScheduledFor = request.ScheduledFor.UTC().Format(time.RFC3339Nano)
	}

	job.Payload.InputSchema = inputSchema
	job.Payload.OutputSchema, err = reader.Read(actionSpec.OutputSchema, actionSpec.OutputSchemaBody)
	if err != nil {
		return Admission{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("output schema for %s/%s: %v", request.App, request.Action, err), Err: err}
	}
	if err := s.store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		if errors.Is(err, state.ErrConflict) && runID != "" {
			existingJob, existingRun, found, getErr := s.store.GetJobByRunID(ctx, request.Workspace, runID)
			if getErr != nil {
				return Admission{}, &Fault{Kind: FaultInternal, Message: "could not resolve idempotent run", Err: getErr}
			}
			if found {
				return replayAdmission(existingRun, existingJob, fingerprint)
			}
			return Admission{}, &Fault{Kind: FaultConflict, Message: "idempotent run already exists", Err: err}
		}
		kind := FaultInternal
		if errors.Is(err, state.ErrConflict) {
			kind = FaultConflict
		}
		return Admission{}, &Fault{Kind: kind, Err: err}
	}
	return Admission{Run: run, Job: job}, nil
}

func (s *Service) GetRun(ctx context.Context, workspace string, runID string) (state.Run, error) {
	if s == nil || s.store == nil {
		return state.Run{}, &Fault{Kind: FaultUnavailable, Message: "execution service is not configured"}
	}
	run, err := s.store.GetRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return state.Run{}, &Fault{Kind: FaultAppNotFound, Message: "run not found", Err: err}
		}
		return state.Run{}, &Fault{Kind: FaultInternal, Err: err}
	}
	if contract.NormalizeWorkspace(run.Deployment.SourceWorkspace()) != contract.NormalizeWorkspace(workspace) {
		return state.Run{}, &Fault{Kind: FaultAppNotFound, Message: "run not found"}
	}
	return run, nil
}

func (s *Service) CancelRun(ctx context.Context, workspace string, runID string, reason string) (state.Run, error) {
	if _, err := s.GetRun(ctx, workspace, runID); err != nil {
		return state.Run{}, err
	}
	run, err := s.store.CancelRun(ctx, strings.TrimSpace(runID), strings.TrimSpace(reason))
	if err != nil {
		return state.Run{}, &Fault{Kind: FaultInternal, Err: err}
	}
	return run, nil
}

func (s *Service) DescribeApp(ctx context.Context, workspace string, app string) (AppDescription, error) {
	if s == nil || s.catalog == nil {
		return AppDescription{}, &Fault{Kind: FaultUnavailable, Message: "execution service is not configured"}
	}
	app = strings.TrimSpace(app)
	if !contract.ValidAppKey(app) {
		return AppDescription{}, &Fault{Kind: FaultInvalidRequest, Message: "invalid app key"}
	}
	deployment, err := s.lookupDeployment(ctx, contract.NormalizeWorkspace(workspace), app)
	if err != nil {
		return AppDescription{}, &Fault{Kind: FaultAppNotFound, Message: "app not found: " + app, Err: err}
	}
	reader := NewSchemaReader(ctx, s.bundles, deployment)
	defer reader.Close()
	actions := make(map[string]ActionDescription, len(deployment.Actions))
	for key, spec := range deployment.Actions {
		inputSchema, readErr := reader.Read(spec.InputSchema, spec.InputSchemaBody)
		if readErr != nil {
			return AppDescription{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("input schema for %s/%s: %v", app, key, readErr), Err: readErr}
		}
		outputSchema, readErr := reader.Read(spec.OutputSchema, spec.OutputSchemaBody)
		if readErr != nil {
			return AppDescription{}, &Fault{Kind: FaultInternal, Message: fmt.Sprintf("output schema for %s/%s: %v", app, key, readErr), Err: readErr}
		}
		actions[key] = ActionDescription{Spec: spec, InputSchema: inputSchema, OutputSchema: outputSchema}
	}
	return AppDescription{Deployment: deployment, Actions: actions}, nil
}

func (s *AdmissionService) GetRunForPrincipal(ctx context.Context, principal Principal, workspace string, runID string) (state.Run, error) {
	principal = principal.Normalized()
	workspace = contract.NormalizeWorkspace(workspace)
	if principal.Workspace != workspace {
		return state.Run{}, &Fault{Kind: FaultForbidden, Message: "principal workspace mismatch"}
	}
	run, err := s.GetRun(ctx, workspace, runID)
	if err != nil {
		return state.Run{}, err
	}
	if principal.HasScope(ScopeRunsReadAny) {
		return run, nil
	}
	if principal.HasScope(ScopeRunsReadOwn) && principal.Owns(run) {
		return run, nil
	}
	return state.Run{}, &Fault{Kind: FaultForbidden, Message: "principal cannot read this run"}
}

func (s *AdmissionService) CancelRunForPrincipal(ctx context.Context, principal Principal, workspace string, runID string, reason string) (state.Run, error) {
	principal = principal.Normalized()
	run, err := s.GetRun(ctx, workspace, runID)
	if err != nil {
		return state.Run{}, err
	}
	if principal.Workspace != contract.NormalizeWorkspace(workspace) {
		return state.Run{}, &Fault{Kind: FaultForbidden, Message: "principal workspace mismatch"}
	}
	if !principal.HasScope(ScopeRunsCancelAny) && !(principal.HasScope(ScopeRunsCancelOwn) && principal.Owns(run)) {
		return state.Run{}, &Fault{Kind: FaultForbidden, Message: "principal cannot cancel this run"}
	}
	return s.CancelRun(ctx, workspace, runID, reason)
}

func (s *AdmissionService) DescribeAppForPrincipal(ctx context.Context, principal Principal, workspace string, app string) (AppDescription, error) {
	principal = principal.Normalized()
	workspace = contract.NormalizeWorkspace(workspace)
	if principal.Workspace != workspace {
		return AppDescription{}, &Fault{Kind: FaultForbidden, Message: "principal workspace mismatch"}
	}
	if !principal.HasScope(ScopeAppsRead) {
		return AppDescription{}, &Fault{Kind: FaultForbidden, Message: "principal lacks apps:read"}
	}
	if !principal.AllowsTarget(app, "") {
		return AppDescription{}, &Fault{Kind: FaultForbidden, Message: "principal is not allowed to read this app"}
	}
	return s.DescribeApp(ctx, workspace, app)
}

func (s *Service) lookupDeployment(ctx context.Context, workspace string, app string) (contract.Deployment, error) {
	if scoped, ok := s.catalog.(interface {
		GetDeploymentForWorkspace(context.Context, string, string) (contract.Deployment, error)
	}); ok {
		return scoped.GetDeploymentForWorkspace(ctx, workspace, app)
	}
	deployment, err := s.catalog.GetDeployment(ctx, app)
	if err != nil {
		return contract.Deployment{}, err
	}
	if contract.NormalizeWorkspace(deployment.SourceWorkspace()) != contract.NormalizeWorkspace(workspace) {
		return contract.Deployment{}, state.ErrNotFound
	}
	return deployment, nil
}

func deterministicRunID(workspace string, principalScope string, key string) string {
	digest := sha256.Sum256([]byte(workspace + "\x00" + principalScope + "\x00" + key))
	return "run_" + hex.EncodeToString(digest[:12])
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func invocationRequestFingerprint(app string, action string, input json.RawMessage, correlationID string, scheduledFor time.Time) (string, error) {
	var decoded any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return "", err
	}
	value := map[string]any{
		"app":            app,
		"action":         action,
		"input":          decoded,
		"correlation_id": state.CleanID(correlationID),
	}
	if !scheduledFor.IsZero() {
		value["scheduled_for"] = scheduledFor.UTC().Format(time.RFC3339Nano)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return digestString(string(canonical)), nil
}

func replayAdmission(run state.Run, job state.Job, fingerprint string) (Admission, error) {
	if run.RequestFingerprint != "" && run.RequestFingerprint != fingerprint {
		return Admission{}, &Fault{Kind: FaultConflict, Message: "idempotency key was already used for a different request"}
	}
	return Admission{Run: run, Job: job, Replayed: true}, nil
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
