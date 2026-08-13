package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	actionruntime "github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/telemetry"
)

type tracingTestRunner struct {
	spanContext trace.SpanContext
	carrier     telemetry.TraceContextV1
}

type mismatchGuardRunner struct{ called bool }

func (r *mismatchGuardRunner) Run(context.Context, actionruntime.RunRequest) (contract.JobResult, error) {
	r.called = true
	return contract.JobResult{}, nil
}

type mismatchingClaimStore struct {
	*state.LocalStore
	requiredLabel string
}

func (s *mismatchingClaimStore) ClaimJobForWorker(ctx context.Context, workerID string, tags []string, _ []string, leaseTTL time.Duration) (state.Job, state.Lease, error) {
	return s.LocalStore.ClaimJobForWorker(ctx, workerID, tags, []string{s.requiredLabel}, leaseTTL)
}

func TestProcessorDefendsAgainstMismatchedClaimedExecutionProfile(t *testing.T) {
	required, err := contract.NewExecutionProfile("", "linux", "amd64", "bun", "1.2.3", "glibc-2.39")
	if err != nil {
		t.Fatal(err)
	}
	offered, err := contract.NewExecutionProfile("", "windows", "amd64", "bun", "1.2.3", "none")
	if err != nil {
		t.Fatal(err)
	}
	requiredLabel, _ := contract.ExecutionProfileLabel(required)
	deployment := contract.Deployment{
		Workspace: "ws-a", App: "profiled", Commit: "commit-a", ExecutionProfile: required,
		RequiredLabels: []string{requiredLabel}, Actions: map[string]contract.Action{"run": {Action: "run"}},
	}
	run := state.NewRun("test", "run-profile-mismatch", deployment.App, "run", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, nil)
	local := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := local.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	runner := &mismatchGuardRunner{}
	processor := Processor{
		Store: &mismatchingClaimStore{LocalStore: local, requiredLabel: requiredLabel}, Runner: runner,
		WorkerID: "wrong-profile", ExecutionProfiles: []contract.ExecutionProfile{offered}, LeaseTTL: time.Minute,
	}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	if runner.called {
		t.Fatal("launcher ran despite execution profile mismatch")
	}
	stored, err := local.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != state.RunFailed || !strings.Contains(string(stored.Error), "incompatible") {
		t.Fatalf("run state/error = %s/%s", stored.State, stored.Error)
	}
}

func (r *tracingTestRunner) Run(ctx context.Context, request actionruntime.RunRequest) (contract.JobResult, error) {
	r.spanContext = trace.SpanContextFromContext(ctx)
	r.carrier = request.TraceContext
	return contract.JobResult{ExitCode: 0, Output: json.RawMessage(`{"ok":true}`)}, nil
}

func TestProcessorUsesCreationTraceInsteadOfPollingTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	creation, _ := telemetry.ParseCarrier("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "", "http")
	polling, _ := telemetry.ParseCarrier("00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01", "", "worker_plane")
	deployment := contract.Deployment{
		Workspace: "ws-a",
		App:       "echo",
		Commit:    "commit-a",
		Actions:   map[string]contract.Action{"run": {Action: "run"}},
	}
	run := state.NewRun("http", "run-trace", "echo", "run", deployment, json.RawMessage(`{}`))
	run.TraceContext = creation
	job := state.NewActionJob(run, nil)
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	runner := &tracingTestRunner{}
	processor := Processor{Store: store, Runner: runner, WorkerID: "worker-a", Group: "test", LeaseTTL: time.Minute}
	processed, err := processor.ProcessOne(telemetry.ContextWithCarrier(context.Background(), polling))
	if err != nil || !processed {
		t.Fatalf("ProcessOne() = %v, %v", processed, err)
	}
	if got := runner.spanContext.TraceID().String(); got != telemetry.TraceID(creation) {
		t.Fatalf("execution trace id = %s, want creation %s", got, telemetry.TraceID(creation))
	}
	if runner.spanContext.TraceID().String() == telemetry.TraceID(polling) {
		t.Fatal("polling transport context became the execution parent")
	}
	if telemetry.TraceID(runner.carrier) != telemetry.TraceID(creation) || runner.carrier.TraceParent == creation.TraceParent {
		t.Fatalf("launcher carrier = %#v, want child in creation trace", runner.carrier)
	}
}

func TestProcessorRecoveryAttemptStartsLinkedRoot(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	creation, _ := telemetry.ParseCarrier("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "", "http")
	deployment := contract.Deployment{
		Workspace: "ws-a",
		App:       "echo",
		Commit:    "commit-a",
		Actions:   map[string]contract.Action{"run": {Action: "run"}},
	}
	run := state.NewRun("http", "run-recovery-trace", "echo", "run", deployment, json.RawMessage(`{}`))
	run.TraceContext = creation
	job := state.NewActionJob(run, nil)
	job.Attempt = 1
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	runner := &tracingTestRunner{}
	processor := Processor{Store: store, Runner: runner, WorkerID: "worker-a", Group: "test", LeaseTTL: time.Minute}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne() = %v, %v", processed, err)
	}
	if !runner.spanContext.IsValid() || runner.spanContext.TraceID().String() == telemetry.TraceID(creation) {
		t.Fatalf("recovery execution trace = %s, creation = %s", runner.spanContext.TraceID(), telemetry.TraceID(creation))
	}
	var attempt sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "windforce.job.attempt" {
			attempt = span
			break
		}
	}
	if attempt == nil || attempt.Parent().IsValid() || len(attempt.Links()) != 1 ||
		attempt.Links()[0].SpanContext.TraceID().String() != telemetry.TraceID(creation) {
		t.Fatalf("recovery attempt span = %#v", attempt)
	}
}

func TestDevelopmentPayloadLogsIncludeCompleteValues(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	logJobInput(true, "job-a", "app-a", "action-a", []byte(`{"account":"visible-local-value"}`), nil)
	logJobExecution(true, "job-a", "app-a", "action-a", contract.JobResult{
		ExitCode: 7,
		Stdout:   "complete stdout",
		Stderr:   "complete stderr",
		Output:   json.RawMessage(`{"result":"complete output"}`),
	}, nil)

	logged := output.String()
	for _, expected := range []string{
		`{"account":"visible-local-value"}`,
		`complete stdout`,
		`complete stderr`,
		`{"result":"complete output"}`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("payload log missing %q: %s", expected, logged)
		}
	}
}

func TestPayloadLogsAreDisabledByDefault(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	logJobInput(false, "job-a", "app-a", "action-a", []byte(`{"secret":"hidden"}`), nil)
	logJobExecution(false, "job-a", "app-a", "action-a", contract.JobResult{Output: json.RawMessage(`{"secret":"hidden"}`)}, nil)
	if output.Len() != 0 {
		t.Fatalf("disabled payload logging wrote: %s", output.String())
	}
}

func TestPayloadLogsMaskResolvedSecrets(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	const secret = "do-not-log-this"
	logJobInput(true, "job-a", "app-a", "action-a", []byte(`{"token":"do-not-log-this"}`), []string{secret})
	logJobExecution(true, "job-a", "app-a", "action-a", contract.JobResult{
		Stdout: secret,
		Output: json.RawMessage(`{"token":"do-not-log-this"}`),
	}, []string{secret})
	if strings.Contains(output.String(), secret) {
		t.Fatalf("payload log exposed resolved secret: %s", output.String())
	}
}

type maskingTestResolver struct{}

func (maskingTestResolver) ResolveRuntimeInput(context.Context, state.Job, json.RawMessage) (json.RawMessage, []string, error) {
	return json.RawMessage(`{"token":"cross-boundary-secret"}`), []string{"cross-boundary-secret"}, nil
}

type maskingTestRunner struct{}

func (maskingTestRunner) Run(_ context.Context, request actionruntime.RunRequest) (contract.JobResult, error) {
	request.LogSink([]byte("before cross-bound"))
	request.LogSink([]byte("ary-secret after"))
	return contract.JobResult{
		Output: json.RawMessage(`{"token":"cross-boundary-secret"}`),
		Stdout: "cross-boundary-secret",
		Stderr: "cross-boundary-secret",
		Error:  "cross-boundary-secret",
	}, nil
}

func TestProcessOneMasksResolvedSecretsBeforeLogsAndResultStorage(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.ConfigureInputCrypto("masking-test-key", "")
	deployment := contract.Deployment{
		Workspace: "ws-a",
		App:       "app",
		Commit:    "commit-a",
		Actions: map[string]contract.Action{
			"run": {Action: "run"},
		},
	}
	run := state.NewRun("test", "run-mask", "app", "run", deployment, json.RawMessage(`{"token":"$var:token"}`))
	run.InputConfigResolved = true
	job := state.NewActionJob(run, run.Input)
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	processor := Processor{
		Store:           store,
		Runner:          maskingTestRunner{},
		WorkerID:        "worker-a",
		LeaseTTL:        time.Minute,
		RuntimeResolver: maskingTestResolver{},
	}
	processed, err := processor.ProcessOne(ctx)
	if err != nil || !processed {
		t.Fatalf("ProcessOne processed=%v err=%v", processed, err)
	}
	completed, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(completed.Result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "cross-boundary-secret") {
		t.Fatalf("stored result exposed secret: %s", encoded)
	}
	logs, found, err := store.GetLogs(ctx, "ws-a", completed.Result.JobID)
	if err != nil || !found {
		t.Fatalf("GetLogs found=%v err=%v", found, err)
	}
	if strings.Contains(logs, "cross-boundary-secret") {
		t.Fatalf("stored logs exposed secret: %q", logs)
	}
}

func TestTeeJobLogsWritesCapturedChunksToProcessLog(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)

	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	processor.TeeJobLogs = true

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	logs, exists, err := stateStore.GetLogs(context.Background(), "workspace-a", completed.Result.JobID)
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if !exists || !strings.Contains(logs, "worker stdout") || !strings.Contains(logs, "worker stderr") {
		t.Fatalf("stored logs = %q, exists = %v", logs, exists)
	}
	processLogs := output.String()
	if !strings.Contains(processLogs, "worker job log") ||
		!strings.Contains(processLogs, "worker stdout") ||
		!strings.Contains(processLogs, "worker stderr") {
		t.Fatalf("process logs = %q", processLogs)
	}
}

func TestProcessorCompletesQueuedRun(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.State != state.RunSucceeded {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunSucceeded)
	}
	if completed.Result == nil || completed.Result.JobID == "" {
		t.Fatalf("completed result missing job id: %#v", completed.Result)
	}
	if completed.Result.Stdout != "" || completed.Result.Stderr != "" {
		t.Fatalf("completed result should not expose logs: %#v", completed.Result)
	}
	logs, exists, err := stateStore.GetLogs(context.Background(), "workspace-a", completed.Result.JobID)
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if !exists || !strings.Contains(logs, "worker stdout") || !strings.Contains(logs, "worker stderr") {
		t.Fatalf("logs = %q, exists = %v", logs, exists)
	}
	var output struct {
		OK          bool   `json:"ok"`
		WorkerGroup string `json:"worker_group"`
		ProxyURL    string `json:"proxy_url"`
		Input       struct {
			Message string `json:"message"`
		} `json:"input"`
	}
	if err := json.Unmarshal(completed.Output, &output); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if !output.OK || output.Input.Message != "hello" || output.WorkerGroup != "test" ||
		output.ProxyURL != "http://job-"+completed.Result.JobID+"@proxy:18080" {
		t.Fatalf("output = %s", completed.Output)
	}
}

func TestProcessorAppliesInputConfigBeforeExecution(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	if _, err := stateStore.SetInputConfig(context.Background(), state.InputConfig{
		WorkspaceID: "workspace-a", AppKey: "echo", ActionKey: "echo",
		Config: json.RawMessage(`{"region":"kr"}`),
	}, "operator"); err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Input map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(completed.Output, &output); err != nil {
		t.Fatal(err)
	}
	if string(output.Input["region"]) != `"kr"` || string(output.Input["message"]) != `"hello"` {
		t.Fatalf("worker input = %s", completed.Output)
	}
}

func TestProcessorOwnsRuntimeBindings(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	processor.RuntimeBindings = RuntimeBindings{
		AuthSession: AuthSessionBinding{
			ServiceURL: "http://auth-session:8005",
			JWT:        "worker-token",
			Timeout:    12 * time.Second,
		},
	}
	if _, err := stateStore.SetInputConfig(context.Background(), state.InputConfig{
		WorkspaceID: "workspace-a", AppKey: "echo", ActionKey: "echo",
		Config: json.RawMessage(`{
			"_SCRAPING_RUNTIME":{"authSession":{"serviceUrl":"http://stale","jwt":"stale","timeoutMs":1}},
			"region":"kr"
		}`),
	}, "operator"); err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Input map[string]json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(completed.Output, &output); err != nil {
		t.Fatal(err)
	}
	if string(output.Input["region"]) != `"kr"` {
		t.Fatalf("business input setting missing: %s", completed.Output)
	}
	var runtimePayload struct {
		AuthSession struct {
			ServiceURL string `json:"serviceUrl"`
			JWT        string `json:"jwt"`
			TimeoutMs  int64  `json:"timeoutMs"`
		} `json:"authSession"`
	}
	if err := json.Unmarshal(output.Input["_SCRAPING_RUNTIME"], &runtimePayload); err != nil {
		t.Fatalf("runtime payload missing: %v: %s", err, completed.Output)
	}
	if runtimePayload.AuthSession.ServiceURL != "http://auth-session:8005" ||
		runtimePayload.AuthSession.JWT != strings.Repeat("*", len("worker-token")) ||
		runtimePayload.AuthSession.TimeoutMs != 12000 {
		t.Fatalf("runtime payload = %#v", runtimePayload.AuthSession)
	}
}

func TestProcessorRejectsLockedInputConfig(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	if _, err := stateStore.SetInputConfig(context.Background(), state.InputConfig{
		WorkspaceID: "workspace-a", AppKey: "echo", ActionKey: "echo",
		Config: json.RawMessage(`{"message":"server"}`), LockedKeys: []string{"message"},
	}, "operator"); err != nil {
		t.Fatal(err)
	}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("ProcessOne = %v, %v", processed, err)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != state.RunFailed || completed.Result == nil || !strings.Contains(string(completed.Result.Output), "InputConfigLocked") {
		t.Fatalf("completed = %#v", completed)
	}
}

func TestProcessorAppliesLogSizeCap(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	processor.LogCapBytes = 5

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.Result == nil {
		t.Fatalf("completed result missing")
	}
	logs, exists, err := stateStore.GetLogs(context.Background(), "workspace-a", completed.Result.JobID)
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if !exists || !strings.Contains(logs, "[log truncated: job exceeded log size cap]") {
		t.Fatalf("logs = %q, exists = %v", logs, exists)
	}
	if strings.Contains(logs, "worker stdout") && strings.Contains(logs, "worker stderr") {
		t.Fatalf("logs were not capped: %q", logs)
	}
}

func TestProcessorStoresFailedActionOutputAndLogsSeparately(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "fail")

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.State != state.RunFailed {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunFailed)
	}
	if completed.Result == nil {
		t.Fatalf("completed result is nil")
	}
	if completed.Result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", completed.Result.ExitCode)
	}
	if completed.Result.Stdout != "" || completed.Result.Stderr != "" {
		t.Fatalf("failed result should not expose logs: %#v", completed.Result)
	}
	var output struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(completed.Result.Output, &output); err != nil {
		t.Fatalf("failed output is not JSON: %v", err)
	}
	if output.Name != "TargetError" || output.Message != "target rejected" {
		t.Fatalf("failed output = %s", completed.Result.Output)
	}
	logs, exists, err := stateStore.GetLogs(context.Background(), "workspace-a", completed.Result.JobID)
	if err != nil {
		t.Fatalf("GetLogs returned error: %v", err)
	}
	if !exists || !strings.Contains(logs, "failure stdout") || !strings.Contains(logs, "failure stderr") {
		t.Fatalf("logs = %q, exists = %v", logs, exists)
	}
}

func TestProcessorPromotesActionFailurePayload(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "declared-fail")

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.State != state.RunFailed {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunFailed)
	}
	if completed.Result == nil {
		t.Fatalf("completed result is nil")
	}
	if completed.Result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", completed.Result.ExitCode)
	}
	if completed.Result.Error != "ERR_MLCOM_MSG10000" {
		t.Fatalf("error = %q, want ECODE", completed.Result.Error)
	}
	if !strings.Contains(string(completed.Result.Output), `"RESULT":"FAIL"`) {
		t.Fatalf("action output was not preserved: %s", completed.Result.Output)
	}
}

func TestProcessorStoresExecutionBundleFetchErrorResult(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "echo")
	runtimeRunner := processor.Runner.(*actionruntime.Runner)
	runtimeRunner.ArtifactStore = executionbundle.NewLocalStore(filepath.Join(t.TempDir(), "empty-artifact-store"))

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.State != state.RunFailed {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunFailed)
	}
	if completed.Result == nil {
		t.Fatalf("completed result is nil")
	}
	var output struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(completed.Result.Output, &output); err != nil {
		t.Fatalf("prepare output is not JSON: %v", err)
	}
	if output.Name != "BundleFetchError" || !strings.Contains(output.Message, "execution bundle") {
		t.Fatalf("execution bundle output = %s", completed.Result.Output)
	}
}

func TestProcessorCreatesHumanTask(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "human")

	processed, err := processor.ProcessOne(context.Background())
	if err != nil {
		t.Fatalf("ProcessOne returned error: %v", err)
	}
	if !processed {
		t.Fatalf("ProcessOne processed no job")
	}

	waiting, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if waiting.State != state.RunWaitingHuman {
		t.Fatalf("run state = %s, want %s", waiting.State, state.RunWaitingHuman)
	}
	task, err := stateStore.GetHumanTask(context.Background(), waiting.TaskID)
	if err != nil {
		t.Fatalf("GetHumanTask returned error: %v", err)
	}
	if task.Title != "Approve" {
		t.Fatalf("task title = %q", task.Title)
	}

	_, resumeJob, err := stateStore.ResumeHumanTask(context.Background(), task.ID, json.RawMessage(`{"approved":true}`))
	if err != nil {
		t.Fatalf("ResumeHumanTask returned error: %v", err)
	}
	if !strings.Contains(string(resumeJob.Payload.Input), `"$resume"`) {
		t.Fatalf("resume job input = %s", resumeJob.Payload.Input)
	}
}

func TestProcessorHeartbeatCancelsRunningAction(t *testing.T) {
	processor, stateStore, run := newProcessorTestHarness(t, "sleep")
	processor.LeaseTTL = 200 * time.Millisecond
	processor.HeartbeatInterval = 20 * time.Millisecond

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		processed, err := processor.ProcessOne(context.Background())
		if err != nil {
			done <- err
			return
		}
		if !processed {
			done <- fmt.Errorf("ProcessOne processed no job")
			return
		}
		done <- nil
	}()

	jobID := waitForRunningJob(t, stateStore, run.ID)
	cancelResult, err := stateStore.CancelJob(context.Background(), "workspace-a", jobID, "operator@example.test", "stop")
	if err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if !cancelResult.SoftCanceled {
		t.Fatalf("CancelJob result = %#v, want soft cancel", cancelResult)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessOne returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ProcessOne did not stop after cancel")
	}
	if elapsed := time.Since(start); elapsed >= 4*time.Second {
		t.Fatalf("ProcessOne waited for the helper sleep instead of canceling: %s", elapsed)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun returned error: %v", err)
	}
	if completed.State != state.RunCanceled {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunCanceled)
	}
	if completed.Result == nil || completed.Result.Error != "job canceled" {
		t.Fatalf("completed result = %#v", completed.Result)
	}
	if completed.Result.Interruption == nil || completed.Result.Interruption.Cause != contract.InterruptionRunCanceled {
		t.Fatalf("canceled interruption = %#v", completed.Result.Interruption)
	}
}

type drainTestRunner struct {
	started  chan struct{}
	release  chan struct{}
	canceled chan struct{}
}

func (r *drainTestRunner) Run(ctx context.Context, _ actionruntime.RunRequest) (contract.JobResult, error) {
	close(r.started)
	select {
	case <-r.release:
		return contract.JobResult{Output: json.RawMessage(`{"drained":true}`)}, nil
	case <-ctx.Done():
		close(r.canceled)
		return contract.JobResult{ExitCode: -1, Error: ctx.Err().Error()}, ctx.Err()
	}
}

func TestRunLoopDrainsActiveJobBeforeGoingOffline(t *testing.T) {
	processor, stateStore, run := newDrainTestProcessor(t, 500*time.Millisecond)
	runner := processor.Runner.(*drainTestRunner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- processor.RunLoop(ctx, 10*time.Millisecond) }()

	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start the queued job")
	}
	cancel()
	waitForWorkerStatus(t, stateStore, processor.WorkerID, state.WorkerStatusDraining)
	select {
	case <-runner.canceled:
		t.Fatal("active job was canceled before the drain timeout")
	case <-time.After(75 * time.Millisecond):
	}
	close(runner.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunLoop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not finish draining")
	}
	workers, err := stateStore.ListWorkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 0 {
		t.Fatalf("workers after drain = %#v, want offline (absent)", workers)
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != state.RunSucceeded {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunSucceeded)
	}
}

func TestRunLoopCancelsActiveJobAfterDrainTimeout(t *testing.T) {
	processor, stateStore, run := newDrainTestProcessor(t, 50*time.Millisecond)
	runner := processor.Runner.(*drainTestRunner)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- processor.RunLoop(ctx, 10*time.Millisecond) }()

	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not start the queued job")
	}
	cancel()
	waitForWorkerStatus(t, stateStore, processor.WorkerID, state.WorkerStatusDraining)
	select {
	case <-runner.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("active job was not canceled after the drain timeout")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunLoop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not go offline after forced drain")
	}
	completed, err := stateStore.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != state.RunFailed {
		t.Fatalf("run state = %s, want %s", completed.State, state.RunFailed)
	}
}

func newDrainTestProcessor(t *testing.T, drainTimeout time.Duration) (Processor, *state.LocalStore, state.Run) {
	t.Helper()
	stateStore := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	stateStore.ConfigureInputCrypto("test-secret-key", "")
	deployment := contract.Deployment{
		Workspace:  "workspace-a",
		App:        "opaque-sdk-app",
		Entrypoint: "main.ts",
		ScriptLang: contract.ScriptLangTypeScript,
		Actions:    map[string]contract.Action{"run": {Action: "run"}},
	}
	run := state.NewRun("windforce", state.NewID("run"), deployment.App, "run", deployment, json.RawMessage(`{"message":"hello"}`))
	if err := stateStore.CreateRunAndEnqueue(context.Background(), run, state.NewActionJob(run, nil)); err != nil {
		t.Fatal(err)
	}
	runner := &drainTestRunner{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		canceled: make(chan struct{}),
	}
	return Processor{
		Store:             stateStore,
		Runner:            runner,
		WorkerID:          "worker-drain",
		Group:             "test",
		EngineVersion:     "v0.9.2",
		BuildRevision:     "abcdef123456",
		LeaseTTL:          time.Second,
		HeartbeatInterval: 20 * time.Millisecond,
		DrainTimeout:      drainTimeout,
	}, stateStore, run
}

func waitForWorkerStatus(t *testing.T, store *state.LocalStore, workerID string, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		workers, err := store.ListWorkers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, worker := range workers {
			if worker.ID == workerID && worker.Status == status {
				if worker.EngineVersion != "v0.9.2" || worker.BuildRevision != "abcdef123456" {
					t.Fatalf("worker build identity = %#v", worker)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker %s did not reach status %s", workerID, status)
}

func newProcessorTestHarness(t *testing.T, helperMode string) (Processor, *state.LocalStore, state.Run) {
	t.Helper()
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "windforce.json"), []byte(`{"app":"echo","entrypoint":"main.ts","actions":{"echo":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "main.ts"), []byte("export async function main(input: unknown) { return input; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundleStore := bundle.NewLocalStore(filepath.Join(tempDir, "store"))
	if err := bundleStore.Materialize(context.Background(), "workspace-a", "source-a", "commit-a", sourceDir); err != nil {
		t.Fatal(err)
	}
	deployment := contract.Deployment{
		Workspace:   "workspace-a",
		GitSourceID: "source-a",
		App:         "echo",
		Commit:      "commit-a",
		Entrypoint:  "main.ts",
		ScriptLang:  "typescript",
		Actions: map[string]contract.Action{
			"echo": {
				Action:  "echo",
				Command: []string{os.Args[0], "-test.run=TestWorkerHelperProcess", "--", helperMode},
			},
		},
	}
	executionBundleStore := executionbundle.NewLocalStore(filepath.Join(tempDir, "artifacts"))
	runner := &actionruntime.Runner{
		Store:         bundleStore,
		ArtifactStore: executionBundleStore,
		CacheRoot:     filepath.Join(tempDir, "cache"),
	}
	deployment, err := runner.BuildExecutionBundle(context.Background(), deployment)
	if err != nil {
		t.Fatalf("BuildExecutionBundle returned error: %v", err)
	}
	runner.Store = nil
	run := state.NewRun("windforce", "run-"+helperMode, "echo", "echo", deployment, json.RawMessage(`{"message":"hello"}`))
	job := state.NewActionJob(run, nil)
	stateStore := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	stateStore.ConfigureInputCrypto("test-secret-key", "")
	if err := stateStore.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	return Processor{
		Store:           stateStore,
		Runner:          runner,
		WorkerID:        "worker-a",
		Group:           "test",
		EgressProxyAddr: "proxy:18080",
		LeaseTTL:        time.Minute,
	}, stateStore, run
}

func waitForRunningJob(t *testing.T, stateStore *state.LocalStore, runID string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := stateStore.Load(context.Background())
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		for _, job := range snapshot.Jobs {
			if job.RunID == runID && job.State == state.JobRunning {
				return job.ID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job for run %s did not reach running state", runID)
	return ""
}

func TestWorkerHelperProcess(t *testing.T) {
	mode := ""
	for index, arg := range os.Args {
		if mode == "" && arg == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
		}
	}
	if mode == "" {
		return
	}

	switch mode {
	case "echo":
		input, err := os.ReadFile(os.Getenv("WF_INPUT_JSON"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		output := []byte(`{"ok":true,"worker_group":` + strconv.Quote(os.Getenv("WF_WORKER_GROUP")) + `,"proxy_url":` + strconv.Quote(os.Getenv("WF_PROXY_URL")) + `,"input":` + string(input) + `}`)
		if err := os.WriteFile(os.Getenv("WF_RESULT_JSON"), output, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("worker stdout")
		fmt.Fprintln(os.Stderr, "worker stderr")
	case "human":
		output := []byte(`{"$windforce":{"type":"human_task","title":"Approve","fields":[{"name":"approved","type":"boolean","required":true}]}}`)
		if err := os.WriteFile(os.Getenv("WF_RESULT_JSON"), output, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "fail":
		output := []byte(`{"name":"TargetError","message":"target rejected"}`)
		if err := os.WriteFile(os.Getenv("WF_RESULT_JSON"), output, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("failure stdout")
		fmt.Fprintln(os.Stderr, "failure stderr")
		os.Exit(7)
	case "declared-fail":
		output := []byte(`{"RESULT":"FAIL","ECODE":"ERR_MLCOM_MSG10000","EMSG":"schema validation failed"}`)
		if err := os.WriteFile(os.Getenv("WF_RESULT_JSON"), output, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "sleep":
		time.Sleep(5 * time.Second)
		if err := os.WriteFile(os.Getenv("WF_RESULT_JSON"), []byte(`{"ok":true}`), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}
