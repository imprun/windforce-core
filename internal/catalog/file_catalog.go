package catalog

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

var ErrDeploymentNotFound = errors.New("deployment not found")
var ErrActionNotFound = errors.New("action not found")

type FileCatalog struct {
	Path string
}

type Snapshot struct {
	Deployments      map[string]contract.Deployment `json:"deployments"`
	RoutingPolicies  map[string]RoutingPolicy       `json:"routingPolicies,omitempty"`
	ActiveHistoryIDs map[string]string              `json:"activeHistoryIds,omitempty"`
	Candidates       map[string]ReleaseCandidate    `json:"releaseCandidates,omitempty"`
	History          []DeploymentHistory            `json:"history,omitempty"`
	Audit            []AuditRecord                  `json:"audit,omitempty"`
	SourceMarkers    map[string]SourceReleaseMarker `json:"sourceReleaseMarkers,omitempty"`
}

type SourceReleaseMarker struct {
	Workspace   string    `json:"workspace"`
	GitSourceID string    `json:"gitSourceId"`
	Commit      string    `json:"commit"`
	ReleasedAt  time.Time `json:"releasedAt"`
}

type Store interface {
	GetDeployment(ctx context.Context, app string) (contract.Deployment, error)
	GetDeploymentForWorkspace(ctx context.Context, workspace string, app string) (contract.Deployment, error)
	LoadCatalog(ctx context.Context) (Snapshot, error)
	PublishRelease(ctx context.Context, deployment contract.Deployment, releasedAt time.Time) (ReleasePublication, error)
	AppendAudit(ctx context.Context, record AuditRecord) error
	AuditTrail(ctx context.Context, workspace string, gitSourceID string) ([]AuditRecord, error)
	SetAppTagOverride(ctx context.Context, workspace string, app string, tagOverride *string) (contract.Deployment, error)
	SetActionTagOverride(ctx context.Context, workspace string, app string, actionKey string, tagOverride *string) (contract.Action, error)
	GetRoutingPolicy(ctx context.Context, workspace string, app string) (RoutingPolicy, error)
	SetInitialAppRoutingPolicy(ctx context.Context, workspace string, app string, patch RoutingPolicyPatch) (RoutingPolicy, error)
	SetAppRoutingPolicy(ctx context.Context, workspace string, app string, patch RoutingPolicyPatch) (contract.Deployment, error)
	SetActionRoutingPolicy(ctx context.Context, workspace string, app string, actionKey string, patch RoutingPolicyPatch) (contract.Action, error)
	ListSourceReleaseMarkers(ctx context.Context) (map[string]SourceReleaseMarker, error)
	ImportCatalog(ctx context.Context, snapshot Snapshot) error
}

type ReleasePublication struct {
	Deployment contract.Deployment
	ReleaseID  string
}

var _ Store = (*FileCatalog)(nil)

// AuditRecord captures a non-release state change (repository settings,
// deletions, route tag overrides) for the audit trail. Releases live in
// DeploymentHistory.
type AuditRecord struct {
	ID          string    `json:"id"`
	Workspace   string    `json:"workspace"`
	GitSourceID string    `json:"gitSourceId"`
	App         string    `json:"app,omitempty"`
	Kind        string    `json:"kind"`
	Detail      string    `json:"detail,omitempty"`
	Actor       string    `json:"actor,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type DeploymentHistory struct {
	ID           string              `json:"id"`
	Workspace    string              `json:"workspace"`
	GitSourceID  string              `json:"gitSourceId,omitempty"`
	App          string              `json:"app"`
	Commit       string              `json:"commit"`
	Entrypoint   string              `json:"entrypoint,omitempty"`
	Source       string              `json:"source"`
	Status       string              `json:"status"`
	DeploymentID *string             `json:"deploymentId,omitempty"`
	Message      *string             `json:"message,omitempty"`
	CreatedBy    *string             `json:"createdBy,omitempty"`
	ObjectURI    string              `json:"objectUri,omitempty"`
	Deployment   contract.Deployment `json:"deployment"`
	CreatedAt    time.Time           `json:"createdAt"`
}

func NewFileCatalog(path string) *FileCatalog {
	return &FileCatalog{Path: path}
}

func (c *FileCatalog) UpsertDeployment(ctx context.Context, deployment contract.Deployment) error {
	_, err := c.PublishRelease(ctx, deployment, time.Now().UTC())
	return err
}

func (c *FileCatalog) PublishRelease(ctx context.Context, deployment contract.Deployment, releasedAt time.Time) (ReleasePublication, error) {
	if err := ctx.Err(); err != nil {
		return ReleasePublication{}, err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return ReleasePublication{}, err
	}
	deployment, history, audit, err := PreparePublication(deployment, releasedAt)
	if err != nil {
		return ReleasePublication{}, err
	}
	deploymentKey := DeploymentKey(deployment.SourceWorkspace(), deployment.App)
	snapshot.Deployments[deploymentKey] = deployment
	snapshot.ActiveHistoryIDs[deploymentKey] = history.ID
	snapshot.History = append(snapshot.History, history)
	snapshot.Audit = append(snapshot.Audit, audit)
	marker := SourceReleaseMarker{
		Workspace:   deployment.SourceWorkspace(),
		GitSourceID: deployment.SourceGitSourceID(),
		Commit:      deployment.Commit,
		ReleasedAt:  history.CreatedAt,
	}
	snapshot.SourceMarkers[SourceReleaseKey(marker.Workspace, marker.GitSourceID)] = marker
	if err := c.write(snapshot); err != nil {
		return ReleasePublication{}, err
	}
	return ReleasePublication{Deployment: deployment, ReleaseID: history.ID}, nil
}

func (c *FileCatalog) RollbackRelease(ctx context.Context, request ReleaseRollbackRequest) (ReleaseRollbackResult, error) {
	if err := ctx.Err(); err != nil {
		return ReleaseRollbackResult{}, err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return ReleaseRollbackResult{}, err
	}
	result, err := ApplyReleaseRollback(&snapshot, request)
	if err != nil {
		return ReleaseRollbackResult{}, err
	}
	if err := c.write(snapshot); err != nil {
		return ReleaseRollbackResult{}, err
	}
	return result, nil
}

func (c *FileCatalog) AppendAudit(ctx context.Context, record AuditRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return err
	}
	record = PrepareAuditRecord(record, time.Now().UTC())
	snapshot.Audit = append(snapshot.Audit, record)
	return c.write(snapshot)
}

func (c *FileCatalog) AuditTrail(ctx context.Context, workspace string, gitSourceID string) ([]AuditRecord, error) {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]AuditRecord, 0)
	for _, record := range snapshot.Audit {
		if record.Workspace == workspace && record.GitSourceID == gitSourceID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (c *FileCatalog) GetDeployment(ctx context.Context, app string) (contract.Deployment, error) {
	return c.GetDeploymentForWorkspace(ctx, contract.DefaultWorkspace, app)
}

func (c *FileCatalog) GetDeploymentForWorkspace(ctx context.Context, workspace string, app string) (contract.Deployment, error) {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return contract.Deployment{}, err
	}
	deployment, ok := snapshot.Deployments[DeploymentKey(workspace, app)]
	if !ok {
		return contract.Deployment{}, ErrDeploymentNotFound
	}
	policy := snapshot.RoutingPolicies[RoutingPolicyKey(workspace, app)]
	return ApplyRoutingPolicy(deployment, policy), nil
}

func (c *FileCatalog) SetAppTagOverride(ctx context.Context, workspace string, app string, tagOverride *string) (contract.Deployment, error) {
	return c.SetAppRoutingPolicy(ctx, workspace, app, RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: tagOverride})
}

func (c *FileCatalog) GetRoutingPolicy(ctx context.Context, workspace string, app string) (RoutingPolicy, error) {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return RoutingPolicy{}, err
	}
	key := DeploymentKey(workspace, app)
	policy, ok := snapshot.RoutingPolicies[key]
	if !ok {
		policy = NewRoutingPolicy(workspace, app)
	}
	return NormalizeRoutingPolicy(policy), nil
}

func (c *FileCatalog) SetInitialAppRoutingPolicy(ctx context.Context, workspace string, app string, patch RoutingPolicyPatch) (RoutingPolicy, error) {
	if err := ctx.Err(); err != nil {
		return RoutingPolicy{}, err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return RoutingPolicy{}, err
	}
	key := DeploymentKey(workspace, app)
	policy := snapshot.RoutingPolicies[key]
	if policy.App == "" {
		policy = NewRoutingPolicy(workspace, app)
	}
	policy = ApplyRoutingPolicyPatch(policy, "", patch, time.Now().UTC())
	snapshot.RoutingPolicies[key] = policy
	if err := c.write(snapshot); err != nil {
		return RoutingPolicy{}, err
	}
	return policy, nil
}

func (c *FileCatalog) SetAppRoutingPolicy(ctx context.Context, workspace string, app string, patch RoutingPolicyPatch) (contract.Deployment, error) {
	if err := ctx.Err(); err != nil {
		return contract.Deployment{}, err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return contract.Deployment{}, err
	}
	key := DeploymentKey(workspace, app)
	deployment, ok := snapshot.Deployments[key]
	if !ok {
		return contract.Deployment{}, ErrDeploymentNotFound
	}
	policy := snapshot.RoutingPolicies[key]
	if policy.App == "" {
		policy = NewRoutingPolicy(workspace, app)
	}
	now := time.Now().UTC()
	previous := policy
	policy = ApplyRoutingPolicyPatch(policy, "", patch, now)
	snapshot.RoutingPolicies[key] = policy
	snapshot.Audit = append(snapshot.Audit, PrepareAuditRecord(AuditRecord{
		Workspace: deployment.SourceWorkspace(), GitSourceID: deployment.SourceGitSourceID(), App: deployment.App,
		Kind: "execution_placement_updated", Detail: RoutingPolicyMutationDetail("app", "", previous, policy), Actor: strings.TrimSpace(patch.Actor),
	}, now))
	if err := c.write(snapshot); err != nil {
		return contract.Deployment{}, err
	}
	return ApplyRoutingPolicy(deployment, policy), nil
}

func (c *FileCatalog) SetActionTagOverride(ctx context.Context, workspace string, app string, actionKey string, tagOverride *string) (contract.Action, error) {
	return c.SetActionRoutingPolicy(ctx, workspace, app, actionKey, RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: tagOverride})
}

func (c *FileCatalog) SetActionRoutingPolicy(ctx context.Context, workspace string, app string, actionKey string, patch RoutingPolicyPatch) (contract.Action, error) {
	if err := ctx.Err(); err != nil {
		return contract.Action{}, err
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return contract.Action{}, err
	}
	key := DeploymentKey(workspace, app)
	deployment, ok := snapshot.Deployments[key]
	if !ok {
		return contract.Action{}, ErrDeploymentNotFound
	}
	if _, ok := deployment.Actions[actionKey]; !ok {
		return contract.Action{}, ErrActionNotFound
	}
	policy := snapshot.RoutingPolicies[key]
	if policy.App == "" {
		policy = NewRoutingPolicy(workspace, app)
	}
	now := time.Now().UTC()
	previous := policy
	policy = ApplyRoutingPolicyPatch(policy, actionKey, patch, now)
	snapshot.RoutingPolicies[key] = policy
	snapshot.Audit = append(snapshot.Audit, PrepareAuditRecord(AuditRecord{
		Workspace: deployment.SourceWorkspace(), GitSourceID: deployment.SourceGitSourceID(), App: deployment.App,
		Kind: "execution_placement_updated", Detail: RoutingPolicyMutationDetail("action", actionKey, previous, policy), Actor: strings.TrimSpace(patch.Actor),
	}, now))
	if err := c.write(snapshot); err != nil {
		return contract.Action{}, err
	}
	return ApplyRoutingPolicy(deployment, policy).Actions[actionKey], nil
}

func (c *FileCatalog) Load(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return NewSnapshot(), nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	NormalizeSnapshot(&snapshot)
	if err := NormalizeSnapshotPublicInterfaces(&snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (c *FileCatalog) LoadCatalog(ctx context.Context) (Snapshot, error) {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return SnapshotWithAppliedRoutingPolicies(snapshot), nil
}

func (c *FileCatalog) ListSourceReleaseMarkers(ctx context.Context) (map[string]SourceReleaseMarker, error) {
	snapshot, err := c.Load(ctx)
	if err != nil {
		return nil, err
	}
	markers := make(map[string]SourceReleaseMarker, len(snapshot.SourceMarkers))
	for key, marker := range snapshot.SourceMarkers {
		markers[key] = marker
	}
	return markers, nil
}

func (c *FileCatalog) ImportCatalog(ctx context.Context, imported Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	NormalizeSnapshot(&imported)
	if err := NormalizeSnapshotPublicInterfaces(&imported); err != nil {
		return fmt.Errorf("import catalog: %w", err)
	}
	snapshot, err := c.Load(ctx)
	if err != nil {
		return err
	}
	MergeSnapshot(&snapshot, imported)
	return c.write(snapshot)
}

func (c *FileCatalog) write(snapshot Snapshot) error {
	NormalizeSnapshot(&snapshot)
	if err := NormalizeSnapshotPublicInterfaces(&snapshot); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpPath := c.Path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, c.Path)
}

func EnsureDeploymentUpdatedAt(deployment contract.Deployment, updatedAt time.Time) contract.Deployment {
	if deployment.UpdatedAt == nil {
		deployment.UpdatedAt = timePtr(updatedAt)
	}
	for key, action := range deployment.Actions {
		if action.UpdatedAt == nil {
			action.UpdatedAt = timePtr(updatedAt)
			deployment.Actions[key] = action
		}
	}
	return deployment
}

func NormalizeDeploymentDefaults(deployment contract.Deployment) contract.Deployment {
	if deployment.Tag == "" {
		deployment.Tag = contract.DefaultRouteTag
	}
	if deployment.TimeoutS == 0 {
		deployment.TimeoutS = contract.DefaultTimeoutS
	}
	if deployment.ScriptLang == "" {
		deployment.ScriptLang = "typescript"
	}
	return deployment
}

func normalizeDeploymentMap(deployments map[string]contract.Deployment) map[string]contract.Deployment {
	normalized := make(map[string]contract.Deployment, len(deployments))
	for key, deployment := range deployments {
		deployment = normalizeStoredDeployment(deployment)
		normalizedKey := DeploymentKey(deployment.SourceWorkspace(), deployment.App)
		if deployment.App == "" {
			normalizedKey = key
		}
		normalized[normalizedKey] = deployment
	}
	return normalized
}

func normalizeStoredDeployment(deployment contract.Deployment) contract.Deployment {
	normalized, err := contract.NormalizeDeploymentPublicInterfaces(deployment)
	if err == nil {
		deployment = normalized
	} else {
		deployment = contract.CloneDeployment(deployment)
	}
	return NormalizeDeploymentDefaults(deployment)
}

func DeploymentKey(workspace string, app string) string {
	return contract.NormalizeWorkspace(workspace) + "/" + app
}

func SourceReleaseKey(workspace string, gitSourceID string) string {
	return contract.NormalizeWorkspace(workspace) + "/" + gitSourceID
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func PreparePublication(deployment contract.Deployment, releasedAt time.Time) (contract.Deployment, DeploymentHistory, AuditRecord, error) {
	if releasedAt.IsZero() {
		releasedAt = time.Now().UTC()
	}
	var err error
	deployment, err = contract.NormalizeDeploymentPublicInterfaces(deployment)
	if err != nil {
		return contract.Deployment{}, DeploymentHistory{}, AuditRecord{}, err
	}
	deployment = NormalizeDeploymentDefaults(deployment)
	// A release is an immutable manifest snapshot. Operator overrides belong to
	// RoutingPolicy and must never be copied into release history.
	_ = ExtractEmbeddedRoutingPolicy(&deployment, NewRoutingPolicy(deployment.SourceWorkspace(), deployment.App))
	deployment = EnsureDeploymentUpdatedAt(deployment, releasedAt)
	history := NewDeploymentHistory(deployment, releasedAt)
	return deployment, history, NewReleaseAudit(history), nil
}

func NewDeploymentHistory(deployment contract.Deployment, createdAt time.Time) DeploymentHistory {
	workspace := deployment.SourceWorkspace()
	gitSourceID := deployment.SourceGitSourceID()
	source := strings.TrimSpace(deployment.Source)
	if source == "" {
		source = "external_sync"
	}
	return DeploymentHistory{
		ID:           newAppVersionID(createdAt),
		Workspace:    workspace,
		GitSourceID:  gitSourceID,
		App:          deployment.App,
		Commit:       deployment.Commit,
		Entrypoint:   deployment.Entrypoint,
		Source:       source,
		Status:       "deployed",
		DeploymentID: cloneStringPtr(deployment.DeploymentID),
		Message:      deployment.Message,
		CreatedBy:    cloneStringPtr(deployment.CreatedBy),
		ObjectURI:    deployment.ObjectURI,
		Deployment:   contract.CloneDeployment(deployment),
		CreatedAt:    createdAt,
	}
}

func NewReleaseAudit(history DeploymentHistory) AuditRecord {
	detail := "commit " + history.Commit
	if history.Message != nil && strings.TrimSpace(*history.Message) != "" {
		detail = strings.TrimSpace(*history.Message)
	}
	actor := "system"
	if history.CreatedBy != nil && strings.TrimSpace(*history.CreatedBy) != "" {
		actor = strings.TrimSpace(*history.CreatedBy)
	}
	return AuditRecord{
		ID:          history.ID,
		Workspace:   history.Workspace,
		GitSourceID: history.GitSourceID,
		App:         history.App,
		Kind:        "release_published",
		Detail:      detail,
		Actor:       actor,
		CreatedAt:   history.CreatedAt,
	}
}

func NewSnapshot() Snapshot {
	return Snapshot{
		Deployments:      map[string]contract.Deployment{},
		RoutingPolicies:  map[string]RoutingPolicy{},
		ActiveHistoryIDs: map[string]string{},
		Candidates:       map[string]ReleaseCandidate{},
		History:          []DeploymentHistory{},
		Audit:            []AuditRecord{},
		SourceMarkers:    map[string]SourceReleaseMarker{},
	}
}

func PrepareAuditRecord(record AuditRecord, createdAt time.Time) AuditRecord {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = createdAt
	}
	if record.ID == "" {
		record.ID = newAppVersionID(record.CreatedAt)
	}
	return record
}

func NormalizeSnapshot(snapshot *Snapshot) {
	if snapshot.Deployments == nil {
		snapshot.Deployments = map[string]contract.Deployment{}
	}
	snapshot.Deployments = normalizeDeploymentMap(snapshot.Deployments)
	if snapshot.RoutingPolicies == nil {
		snapshot.RoutingPolicies = map[string]RoutingPolicy{}
	}
	for key, deployment := range snapshot.Deployments {
		policy := snapshot.RoutingPolicies[key]
		policy = ExtractEmbeddedRoutingPolicy(&deployment, policy)
		snapshot.Deployments[key] = deployment
		if !RoutingPolicyEmpty(policy) {
			snapshot.RoutingPolicies[key] = policy
		}
	}
	if snapshot.ActiveHistoryIDs == nil {
		snapshot.ActiveHistoryIDs = map[string]string{}
	}
	if snapshot.Candidates == nil {
		snapshot.Candidates = map[string]ReleaseCandidate{}
	}
	for key, candidate := range snapshot.Candidates {
		candidate.Deployment = normalizeStoredDeployment(candidate.Deployment)
		snapshot.Candidates[key] = candidate
	}
	if snapshot.History == nil {
		snapshot.History = []DeploymentHistory{}
	}
	for index := range snapshot.History {
		snapshot.History[index].Deployment = normalizeStoredDeployment(snapshot.History[index].Deployment)
	}
	if snapshot.Audit == nil {
		snapshot.Audit = []AuditRecord{}
	}
	if snapshot.SourceMarkers == nil {
		snapshot.SourceMarkers = map[string]SourceReleaseMarker{}
	}
	backfillActiveHistoryIDs(snapshot)
}

// NormalizeSnapshotPublicInterfaces validates and canonicalizes every
// Deployment snapshot that may be activated, rolled back, or published. It is
// intentionally error-returning so file and import boundaries can fail closed
// while the legacy structural NormalizeSnapshot API remains compatible.
func NormalizeSnapshotPublicInterfaces(snapshot *Snapshot) error {
	for key, deployment := range snapshot.Deployments {
		normalized, err := contract.NormalizeDeploymentPublicInterfaces(deployment)
		if err != nil {
			return fmt.Errorf("deployment %s: %w", key, err)
		}
		snapshot.Deployments[key] = normalized
	}
	for key, candidate := range snapshot.Candidates {
		normalized, err := contract.NormalizeDeploymentPublicInterfaces(candidate.Deployment)
		if err != nil {
			return fmt.Errorf("release candidate %s: %w", key, err)
		}
		candidate.Deployment = normalized
		snapshot.Candidates[key] = candidate
	}
	for index := range snapshot.History {
		normalized, err := contract.NormalizeDeploymentPublicInterfaces(snapshot.History[index].Deployment)
		if err != nil {
			return fmt.Errorf("release history %s: %w", snapshot.History[index].ID, err)
		}
		snapshot.History[index].Deployment = normalized
	}
	return nil
}

func MergeSnapshot(target *Snapshot, imported Snapshot) {
	NormalizeSnapshot(target)
	NormalizeSnapshot(&imported)
	for key, deployment := range imported.Deployments {
		if _, exists := target.Deployments[key]; !exists {
			target.Deployments[key] = deployment
		}
	}
	for key, policy := range imported.RoutingPolicies {
		if _, exists := target.RoutingPolicies[key]; !exists {
			target.RoutingPolicies[key] = NormalizeRoutingPolicy(policy)
		}
	}
	for key, historyID := range imported.ActiveHistoryIDs {
		if _, exists := target.ActiveHistoryIDs[key]; !exists {
			target.ActiveHistoryIDs[key] = historyID
		}
	}
	for key, candidate := range imported.Candidates {
		if _, exists := target.Candidates[key]; !exists {
			target.Candidates[key] = candidate
		}
	}
	historyIDs := make(map[string]struct{}, len(target.History))
	for _, record := range target.History {
		historyIDs[record.ID] = struct{}{}
	}
	for _, record := range imported.History {
		if _, exists := historyIDs[record.ID]; !exists {
			target.History = append(target.History, record)
			historyIDs[record.ID] = struct{}{}
		}
	}
	auditIDs := make(map[string]struct{}, len(target.Audit))
	for _, record := range target.Audit {
		auditIDs[record.ID] = struct{}{}
	}
	for _, record := range imported.Audit {
		if _, exists := auditIDs[record.ID]; !exists {
			target.Audit = append(target.Audit, record)
			auditIDs[record.ID] = struct{}{}
		}
	}
	for key, marker := range imported.SourceMarkers {
		if _, exists := target.SourceMarkers[key]; !exists {
			target.SourceMarkers[key] = marker
		}
	}
}

func SnapshotWithAppliedRoutingPolicies(snapshot Snapshot) Snapshot {
	NormalizeSnapshot(&snapshot)
	deployments := make(map[string]contract.Deployment, len(snapshot.Deployments))
	for key, deployment := range snapshot.Deployments {
		deployments[key] = ApplyRoutingPolicy(deployment, snapshot.RoutingPolicies[key])
	}
	snapshot.Deployments = deployments
	return snapshot
}

func newAppVersionID(createdAt time.Time) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	}
	return fmt.Sprintf("%d", createdAt.UnixNano())
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
