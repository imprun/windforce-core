package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRunStandaloneVerifiesArtifactsAndReportsNoWorker(t *testing.T) {
	fixture := newFixture(t)
	report := Run(context.Background(), fixture.options)
	if report.SchemaVersion != ReportSchemaVersion || report.Status != StatusWarn || ExitCode(report) != 1 {
		t.Fatalf("report summary = %#v", report)
	}
	assertCheck(t, report, CheckSourceArtifactStore, StatusPass, "")
	assertCheck(t, report, CheckExecutionArtifactStore, StatusPass, "")
	assertCheck(t, report, CheckPlacementCompatibility, StatusWarn, state.PlacementReasonNoLiveCapacity)

	marker := filepath.Join(fixture.storeRoot, "gitrepos", "workspace-a", "source-a", "commit-a", ".windforce_clone_complete")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	report = Run(context.Background(), fixture.options)
	assertCheck(t, report, CheckSourceArtifactStore, StatusFail, "")
	if report.Status != StatusFail || ExitCode(report) != 2 {
		t.Fatalf("missing marker status = %s, exit = %d", report.Status, ExitCode(report))
	}
}

func TestRunExplainsExecutionProfileMismatch(t *testing.T) {
	fixture := newFixture(t)
	wrong, err := contract.NewExecutionProfile("worker-image", "linux", "arm64", "bun", "1.2.3", "glibc")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := fixture.state.RegisterWorker(context.Background(), state.WorkerRecord{
		ID: "worker-a", ExecutionProfiles: []contract.ExecutionProfile{wrong}, Slots: 1,
		Status: state.WorkerStatusActive, StartedAt: now, LastHeartbeatAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), fixture.options)
	assertCheck(t, report, CheckPlacementCompatibility, StatusWarn, state.PlacementReasonExecutionProfileMismatch)
}

func TestRunExplainsQueuedJobSchedulingBlocker(t *testing.T) {
	fixture := newFixture(t)
	now := time.Now().UTC()
	runID := state.NewID("run")
	jobID := state.NewID("job")
	if err := fixture.state.CreateRunAndEnqueue(context.Background(), state.Run{
		ID: runID, App: "echo", Action: "run", State: state.RunQueued, CreatedAt: now, UpdatedAt: now,
	}, state.Job{
		ID: jobID, RunID: runID, State: state.JobQueued, Kind: "action", Priority: 100,
		Payload:   state.JobPayload{Workspace: "workspace-a", App: "echo", Action: "run", ExecutionProfile: fixture.profile},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), fixture.options)
	assertCheck(t, report, CheckQueueScheduling, StatusWarn, state.PlacementReasonNoLiveCapacity)
}

func TestRunRemoteWorkerUsesOnlyApplicableChecksAndRedactsFailures(t *testing.T) {
	sensitive := "redaction-sentinel"
	serverURL := "https://user:" + sensitive + "@example.invalid/private"
	report := Run(context.Background(), Options{
		Mode: ModeRemoteWorker, Version: "dev", Revision: "rev", ServerURL: serverURL,
		RuntimeProfiles: func(context.Context) ([]contract.ExecutionProfile, error) {
			return nil, context.DeadlineExceeded
		},
		CapabilityGateway: func(context.Context) error { return context.DeadlineExceeded },
	})
	gotIDs := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		gotIDs = append(gotIDs, check.ID)
	}
	wantIDs := []string{
		CheckBuildIdentity, CheckStateConnectivity, CheckStateSchema, CheckActiveRelease,
		CheckSourceArtifactStore, CheckExecutionArtifactStore, CheckServerReachability,
		CheckAuthConfiguration, CheckRuntimeProfiles, CheckPlacementCompatibility,
		CheckQueueScheduling, CheckSecretBackend, CheckCapabilityGateway,
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("check ids = %#v, want %#v", gotIDs, wantIDs)
	}
	assertCheck(t, report, CheckStateConnectivity, StatusUnsupported, "")
	assertCheck(t, report, CheckServerReachability, StatusFail, "")
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), sensitive) || strings.Contains(string(data), serverURL) || strings.Contains(string(data), "DeadlineExceeded") {
		t.Fatalf("report leaked sensitive input or raw error: %s", data)
	}
}

func TestReadinessProbeDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if err := probeReady(context.Background(), redirect.URL, redirect.Client()); err == nil {
		t.Fatal("probeReady followed redirect")
	}
}

func TestWriteTextAndExitCode(t *testing.T) {
	report := Report{Mode: ModeServer, Status: StatusWarn, Checks: []Check{{ID: "sample", Status: StatusWarn, Message: "warning", Remediation: "fix it"}}}
	var output bytes.Buffer
	if err := WriteText(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "WARNING") && !strings.Contains(output.String(), "WARN") {
		t.Fatalf("text output = %q", output.String())
	}
	if ExitCode(report) != 1 {
		t.Fatalf("exit = %d", ExitCode(report))
	}
}

func TestReportMatchesPublishedJSONSchema(t *testing.T) {
	report := Run(context.Background(), Options{Mode: ModeServer, Version: "dev", Revision: "test", Development: true})
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	schemaJSON, err := os.ReadFile(filepath.Join("..", "..", "docs", "api", "diagnose-report.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "urn:windforce:diagnose:v1"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	reportDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(reportJSON))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(reportDocument); err != nil {
		t.Fatalf("report does not match schema: %v\n%s", err, reportJSON)
	}
}

type fixture struct {
	state     *state.LocalStore
	storeRoot string
	profile   contract.ExecutionProfile
	options   Options
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	storeRoot := filepath.Join(root, "store")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.ts"), []byte("export default 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceStore := bundle.NewLocalStore(storeRoot)
	if err := sourceStore.Materialize(ctx, "workspace-a", "source-a", "commit-a", source); err != nil {
		t.Fatal(err)
	}
	executionStore := executionbundle.NewLocalStore(filepath.Join(storeRoot, "artifacts"))
	descriptor, err := executionStore.Publish(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := contract.NewExecutionProfile("release-image", "linux", "amd64", "bun", "1.2.3", "glibc")
	if err != nil {
		t.Fatal(err)
	}
	stateStore := state.NewLocalStore(filepath.Join(root, "state.json"))
	_, err = stateStore.PublishRelease(ctx, contract.Deployment{
		Workspace: "workspace-a", GitSourceID: "source-a", App: "echo", Commit: "commit-a",
		Entrypoint: "main.ts", Runtime: "bun", ScriptLang: contract.ScriptLangTypeScript,
		BundleDigest: descriptor.Digest, BundleURI: descriptor.URI, ExecutionProfile: profile,
		ObjectURI: "bundle://workspace-a/source-a/commit-a", Actions: map[string]contract.Action{"run": {Action: "run"}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		state: stateStore, storeRoot: storeRoot, profile: profile,
		options: Options{
			Mode: ModeStandalone, Version: "dev", Revision: "test", StateBackend: "local",
			StateConnected: true, Store: stateStore, Catalog: stateStore, SourceStore: sourceStore,
			ExecutionStore: executionStore, AuthConfigured: true, SecretBackendConfigured: true,
			RuntimeProfiles: func(context.Context) ([]contract.ExecutionProfile, error) {
				return []contract.ExecutionProfile{profile}, nil
			},
		},
	}
}

func assertCheck(t *testing.T, report Report, id string, status Status, detailValue string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID != id {
			continue
		}
		if check.Status != status {
			t.Fatalf("check %s status = %s, want %s (%#v)", id, check.Status, status, check)
		}
		if detailValue != "" {
			values, _ := check.Details["reason_codes"].([]string)
			for _, value := range values {
				if value == detailValue {
					return
				}
			}
			t.Fatalf("check %s details = %#v, want %q", id, check.Details, detailValue)
		}
		return
	}
	t.Fatalf("check %s not found", id)
}
