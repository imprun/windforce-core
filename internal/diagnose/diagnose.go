package diagnose

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	CheckBuildIdentity          = "build.identity"
	CheckStateConnectivity      = "state.connectivity"
	CheckStateSchema            = "state.schema"
	CheckSourceArtifactStore    = "artifact.source_store"
	CheckExecutionArtifactStore = "artifact.execution_store"
	CheckServerReachability     = "server.reachability"
	CheckAuthConfiguration      = "auth.configuration"
	CheckRuntimeProfiles        = "runtime.profiles"
	CheckActiveRelease          = "release.active"
	CheckPlacementCompatibility = "placement.compatibility"
	CheckQueueScheduling        = "queue.scheduling"
	CheckSecretBackend          = "secret_backend.configuration"
	CheckCapabilityGateway      = "capability_gateway.readiness"
)

type Options struct {
	Mode                    Mode
	Version                 string
	Revision                string
	Capabilities            map[string]string
	StateBackend            string
	StateConnected          bool
	Store                   state.Store
	Catalog                 catalog.Store
	SourceStore             bundle.Verifier
	ExecutionStore          executionbundle.Store
	RuntimeProfiles         func(context.Context) ([]contract.ExecutionProfile, error)
	ServerURL               string
	HTTPClient              *http.Client
	AuthConfigured          bool
	SecretBackendConfigured bool
	Development             bool
	CapabilityGateway       func(context.Context) error
	Now                     func() time.Time
}

type runState struct {
	observedAt time.Time
	checks     []Check
	snapshot   *catalog.Snapshot
	workspaces []string
}

func Run(ctx context.Context, options Options) Report {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	run := &runState{observedAt: now}
	run.checkBuild(options)
	run.checkState(ctx, options)
	run.checkCatalog(ctx, options)
	run.checkArtifacts(ctx, options)
	run.checkServer(ctx, options)
	run.checkAuth(options)
	run.checkRuntime(ctx, options)
	run.checkPlacement(ctx, options)
	run.checkQueue(ctx, options)
	run.checkSecretBackend(options)
	run.checkCapabilityGateway(ctx, options)
	return Report{
		SchemaVersion: ReportSchemaVersion,
		Mode:          options.Mode,
		Status:        overallStatus(run.checks),
		ObservedAt:    now,
		Checks:        run.checks,
	}
}

func (run *runState) checkBuild(options Options) {
	capabilities := make([]string, 0, len(options.Capabilities))
	for key, capabilityVersion := range options.Capabilities {
		capabilities = append(capabilities, key+"="+capabilityVersion)
	}
	sort.Strings(capabilities)
	run.add(CheckBuildIdentity, StatusPass, "build identity and engine capabilities are available", "", map[string]any{
		"version":          strings.TrimSpace(options.Version),
		"revision":         strings.TrimSpace(options.Revision),
		"capability_count": len(capabilities),
		"capabilities":     capabilities,
	})
}

func (run *runState) checkState(ctx context.Context, options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckStateConnectivity, "remote workers use the Worker API instead of a direct state backend")
		run.unsupported(CheckStateSchema, "remote workers do not inspect server database schema")
		return
	}
	if !options.StateConnected || options.Store == nil {
		run.add(CheckStateConnectivity, StatusFail, "state backend is not reachable", "verify the backend selection and connectivity without printing credentials", map[string]any{"backend": options.StateBackend})
		run.unsupported(CheckStateSchema, "schema compatibility cannot be checked without a state connection")
		return
	}
	run.add(CheckStateConnectivity, StatusPass, "state backend is reachable", "", map[string]any{"backend": options.StateBackend})
	checker, ok := options.Store.(state.SchemaCompatibilityStore)
	if !ok {
		run.unsupported(CheckStateSchema, "state backend does not expose the optional schema compatibility capability")
		return
	}
	compatibility, err := checker.CheckSchemaCompatibility(ctx)
	if err != nil {
		run.add(CheckStateSchema, StatusFail, "state schema compatibility could not be determined", "run the configured backend migration with the matching Core build", nil)
		return
	}
	details := map[string]any{
		"backend":       compatibility.Backend,
		"contract":      compatibility.Contract,
		"missing_count": len(compatibility.Missing),
	}
	if len(compatibility.Missing) > 0 {
		details["missing"] = compatibility.Missing
	}
	if !compatibility.Compatible {
		run.add(CheckStateSchema, StatusFail, "state schema is not compatible with this Core build", "stop the process and run the documented state migration before serving or claiming Jobs", details)
		return
	}
	run.add(CheckStateSchema, StatusPass, "state schema is compatible with this Core build", "", details)
}

func (run *runState) checkCatalog(ctx context.Context, options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckActiveRelease, "remote workers receive immutable Job pins from the Worker API")
		return
	}
	if !options.StateConnected || options.Catalog == nil {
		run.add(CheckActiveRelease, StatusFail, "active Releases cannot be read", "restore state connectivity and the release catalog capability", nil)
		return
	}
	snapshot, err := options.Catalog.LoadCatalog(ctx)
	if err != nil {
		run.add(CheckActiveRelease, StatusFail, "active Releases cannot be read", "verify catalog schema and state backend health", nil)
		return
	}
	run.snapshot = &snapshot
	run.workspaces = diagnoseWorkspaces(ctx, options.Store, snapshot)
	count := len(snapshot.Deployments)
	if count == 0 {
		run.add(CheckActiveRelease, StatusWarn, "no active Release is published", "publish an App Release before expecting Jobs to execute", map[string]any{"active_releases": 0})
		return
	}
	run.add(CheckActiveRelease, StatusPass, "active Releases are readable", "", map[string]any{"active_releases": count})
}

func (run *runState) checkArtifacts(ctx context.Context, options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckSourceArtifactStore, "remote workers never read the Source Store")
		run.unsupported(CheckExecutionArtifactStore, "remote workers fetch pinned artifacts through the Worker API")
		return
	}
	if run.snapshot == nil {
		run.unsupported(CheckSourceArtifactStore, "source snapshots cannot be checked without the active Release catalog")
		run.unsupported(CheckExecutionArtifactStore, "execution bundles cannot be checked without the active Release catalog")
		return
	}
	run.checkSourceArtifacts(ctx, options)
	run.checkExecutionArtifacts(ctx, options)
}

func (run *runState) checkSourceArtifacts(ctx context.Context, options Options) {
	if options.SourceStore == nil {
		run.unsupported(CheckSourceArtifactStore, "Source Store does not expose read-only marker verification")
		return
	}
	verified := 0
	failed := 0
	seen := map[string]struct{}{}
	for _, deployment := range run.snapshot.Deployments {
		workspace := deployment.SourceWorkspace()
		sourceID := deployment.SourceGitSourceID()
		key := workspace + "\x00" + sourceID + "\x00" + deployment.Commit
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := options.SourceStore.Verify(ctx, workspace, sourceID, deployment.Commit); err != nil {
			failed++
			continue
		}
		verified++
	}
	details := map[string]any{"verified": verified, "failed": failed}
	if failed > 0 {
		run.add(CheckSourceArtifactStore, StatusFail, "one or more active Release source snapshots have a missing or stale marker", "synchronize the exact source commit again before republishing the affected Release", details)
		return
	}
	run.add(CheckSourceArtifactStore, StatusPass, "active Release source snapshot markers are consistent", "", details)
}

func (run *runState) checkExecutionArtifacts(ctx context.Context, options Options) {
	if options.ExecutionStore == nil {
		run.unsupported(CheckExecutionArtifactStore, "Execution Artifact Store does not expose read-only verification")
		return
	}
	verified := 0
	failed := 0
	seen := map[string]struct{}{}
	for _, deployment := range run.snapshot.Deployments {
		digest := strings.TrimSpace(deployment.BundleDigest)
		if digest == "" {
			failed++
			continue
		}
		if _, ok := seen[digest]; ok {
			continue
		}
		seen[digest] = struct{}{}
		if _, err := options.ExecutionStore.Verify(ctx, digest); err != nil {
			failed++
			continue
		}
		verified++
	}
	details := map[string]any{"verified": verified, "failed": failed}
	if failed > 0 {
		run.add(CheckExecutionArtifactStore, StatusFail, "one or more active Release execution bundles are missing or inconsistent", "republish the affected immutable Release bundle before allowing new execution", details)
		return
	}
	run.add(CheckExecutionArtifactStore, StatusPass, "active Release execution bundles are consistent", "", details)
}

func (run *runState) checkServer(ctx context.Context, options Options) {
	if strings.TrimSpace(options.ServerURL) == "" {
		if options.Mode == ModeRemoteWorker {
			run.add(CheckServerReachability, StatusFail, "remote Worker API URL is not configured", "provide the Core server URL used by the remote worker", nil)
			return
		}
		run.unsupported(CheckServerReachability, "no running server URL was provided for an optional readiness probe")
		return
	}
	if err := probeReady(ctx, options.ServerURL, options.HTTPClient); err != nil {
		run.add(CheckServerReachability, StatusFail, "Core server readiness probe failed", "verify the configured server address, TLS, network route, and process readiness", nil)
		return
	}
	run.add(CheckServerReachability, StatusPass, "Core server readiness endpoint is reachable", "", nil)
}

func (run *runState) checkAuth(options Options) {
	if options.AuthConfigured {
		run.add(CheckAuthConfiguration, StatusPass, "required authentication material is configured", "", nil)
		return
	}
	message := "server admin authentication material is not configured"
	remediation := "configure the server admin token environment before production startup"
	if options.Mode == ModeRemoteWorker {
		message = "remote worker authentication material is not configured"
		remediation = "configure the worker token environment before connecting to the Worker API"
	}
	if options.Development {
		run.add(CheckAuthConfiguration, StatusWarn, message+"; development mode permits this", remediation, nil)
		return
	}
	run.add(CheckAuthConfiguration, StatusFail, message, remediation, nil)
}

func (run *runState) checkRuntime(ctx context.Context, options Options) {
	if options.Mode == ModeServer {
		run.unsupported(CheckRuntimeProfiles, "server-only mode does not execute Jobs")
		return
	}
	if options.RuntimeProfiles == nil {
		run.add(CheckRuntimeProfiles, StatusFail, "worker execution profiles cannot be inspected", "configure the worker runtime executables and execution profile identity", nil)
		return
	}
	profiles, err := options.RuntimeProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		run.add(CheckRuntimeProfiles, StatusFail, "no usable worker execution profile was detected", "install or configure at least one supported App runtime", nil)
		return
	}
	runtimes := map[string]struct{}{}
	for _, profile := range profiles {
		if runtimeName := strings.TrimSpace(profile.Runtime); runtimeName != "" {
			runtimes[runtimeName] = struct{}{}
		}
	}
	runtimeNames := make([]string, 0, len(runtimes))
	for runtimeName := range runtimes {
		runtimeNames = append(runtimeNames, runtimeName)
	}
	sort.Strings(runtimeNames)
	run.add(CheckRuntimeProfiles, StatusPass, "worker execution profiles are available", "", map[string]any{"profile_count": len(profiles), "runtimes": runtimeNames})
}

func (run *runState) checkPlacement(ctx context.Context, options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckPlacementCompatibility, "remote workers do not own the server placement projection")
		return
	}
	projection, ok := options.Store.(state.PlacementObservationStore)
	if !ok || run.snapshot == nil {
		run.unsupported(CheckPlacementCompatibility, "placement projection is unavailable without a compatible state catalog")
		return
	}
	targets := 0
	blocked := 0
	reasons := map[string]struct{}{}
	for _, deploymentKey := range sortedKeys(run.snapshot.Deployments) {
		deployment := run.snapshot.Deployments[deploymentKey]
		observation, err := projection.GetPlacementCandidates(ctx, deployment.SourceWorkspace(), deployment.App, "", true)
		if err != nil {
			run.add(CheckPlacementCompatibility, StatusFail, "placement compatibility could not be evaluated", "verify state schema and Worker registry health", nil)
			return
		}
		for _, target := range observation.Targets {
			targets++
			if target.MatchingWorkers > 0 {
				continue
			}
			blocked++
			foundReason := false
			for _, candidate := range target.Candidates {
				for _, reason := range candidate.ReasonCodes {
					reasons[reason] = struct{}{}
					foundReason = true
				}
			}
			if !foundReason {
				reasons[state.PlacementReasonNoLiveCapacity] = struct{}{}
			}
		}
	}
	details := map[string]any{"targets": targets, "blocked_targets": blocked, "reason_codes": sortedKeys(reasons)}
	if blocked > 0 {
		run.add(CheckPlacementCompatibility, StatusWarn, "one or more active Release targets have no compatible live Worker", "start a Worker whose route tags, labels, workspace scope, and execution profile match the active Release", details)
		return
	}
	run.add(CheckPlacementCompatibility, StatusPass, "active Release targets have compatible live Workers", "", details)
}

func (run *runState) checkQueue(ctx context.Context, options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckQueueScheduling, "remote workers do not own the durable queue projection")
		return
	}
	projection, ok := options.Store.(state.PlacementObservationStore)
	if !ok || run.snapshot == nil {
		run.unsupported(CheckQueueScheduling, "queue scheduling reasons are unavailable without a compatible state catalog")
		return
	}
	queued := int64(0)
	blockedTargets := 0
	reasons := map[string]struct{}{}
	for _, workspace := range run.workspaces {
		demand, err := projection.GetExecutionDemand(ctx, workspace, "", "", true)
		if err != nil {
			run.add(CheckQueueScheduling, StatusFail, "queued Job scheduling reasons could not be evaluated", "verify state schema and queue projection health", nil)
			return
		}
		queued += demand.QueuedJobs
		for _, target := range demand.Targets {
			if target.QueuedJobs == 0 || (target.MatchingWorkers > 0 && !target.Saturated) {
				continue
			}
			blockedTargets++
			if target.Saturated {
				reasons["no_available_slots"] = struct{}{}
			}
			foundReason := false
			for _, candidate := range target.Candidates {
				for _, reason := range candidate.ReasonCodes {
					reasons[reason] = struct{}{}
					foundReason = true
				}
			}
			if target.MatchingWorkers == 0 && !foundReason {
				reasons[state.PlacementReasonNoLiveCapacity] = struct{}{}
			}
		}
	}
	details := map[string]any{"queued_jobs": queued, "blocked_targets": blockedTargets, "reason_codes": sortedKeys(reasons)}
	if queued > 0 && blockedTargets > 0 {
		run.add(CheckQueueScheduling, StatusWarn, "queued Jobs include targets that cannot currently be scheduled", "use the stable reason codes to restore compatible capacity or placement", details)
		return
	}
	run.add(CheckQueueScheduling, StatusPass, "queued Jobs have no current stable scheduling blocker", "", details)
}

func (run *runState) checkSecretBackend(options Options) {
	if options.Mode == ModeRemoteWorker {
		run.unsupported(CheckSecretBackend, "remote workers do not own the server Secret backend key")
		return
	}
	if options.SecretBackendConfigured {
		run.add(CheckSecretBackend, StatusPass, "Secret backend encryption material is configured", "", nil)
		return
	}
	if options.Development {
		run.add(CheckSecretBackend, StatusWarn, "Secret backend uses the development fallback", "configure an instance Secret key before production startup", nil)
		return
	}
	run.add(CheckSecretBackend, StatusFail, "Secret backend encryption material is not configured", "configure an instance Secret key before production startup", nil)
}

func (run *runState) checkCapabilityGateway(ctx context.Context, options Options) {
	if options.Mode == ModeServer {
		run.unsupported(CheckCapabilityGateway, "server-only mode has no worker-local capability gateway")
		return
	}
	if options.CapabilityGateway == nil {
		run.unsupported(CheckCapabilityGateway, "no optional worker-local capability gateway is configured")
		return
	}
	if err := options.CapabilityGateway(ctx); err != nil {
		run.add(CheckCapabilityGateway, StatusFail, "worker-local capability gateway readiness failed", "verify the loopback endpoint, worker credential, and ready provider set", nil)
		return
	}
	run.add(CheckCapabilityGateway, StatusPass, "worker-local capability gateway is ready", "", nil)
}

func (run *runState) add(id string, status Status, message string, remediation string, details map[string]any) {
	run.checks = append(run.checks, Check{ID: id, Status: status, Message: message, Remediation: remediation, ObservedAt: run.observedAt, Details: details})
}

func (run *runState) unsupported(id string, message string) {
	run.add(id, StatusUnsupported, message, "", nil)
}

func diagnoseWorkspaces(ctx context.Context, store state.Store, snapshot catalog.Snapshot) []string {
	set := map[string]struct{}{contract.DefaultWorkspace: {}}
	for _, deployment := range snapshot.Deployments {
		set[deployment.SourceWorkspace()] = struct{}{}
	}
	if store != nil {
		if workspaces, err := store.ListWorkspaces(ctx); err == nil {
			for _, workspace := range workspaces {
				set[contract.NormalizeWorkspace(workspace.ID)] = struct{}{}
			}
		}
	}
	return sortedKeys(set)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func probeReady(ctx context.Context, rawURL string, client *http.Client) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("invalid server URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/readyz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return errors.New("create readiness request")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	probeClient := *client
	probeClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := probeClient.Do(request)
	if err != nil {
		return errors.New("readiness request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("readiness endpoint is not ready")
	}
	return nil
}
