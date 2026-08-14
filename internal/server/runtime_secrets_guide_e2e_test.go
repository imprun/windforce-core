package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/remoteworker"
	actionruntime "github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/runtimeconfig"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/syncer"
	"github.com/imprun/windforce-core/internal/worker"
)

// TestTypeScriptRuntimeSecretsGuideE2E keeps the developer guide executable.
// It publishes examples/typescript-runtime-secrets, configures a client-scoped
// Secret Variable reference, and runs the Action through local and remote
// workers without exposing encryption keys to the remote worker.
func TestTypeScriptRuntimeSecretsGuideE2E(t *testing.T) {
	requireCommand(t, "bun")
	requireCommand(t, "git")

	const (
		workspaceID = "runtime-secrets-guide"
		appKey      = "runtime_secrets"
		actionKey   = "deliver"
		secretKey   = "runtime-secrets-guide-instance-key"
		secretValue = "guide-secret-value-42"
		workerToken = "runtime-secrets-guide-worker-token"
	)

	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")
	stateStore := state.NewLocalStore(statePath)
	stateStore.ConfigureInputCrypto(secretKey, "")
	if _, err := stateStore.CreateWorkspace(context.Background(), workspaceID, workspaceID, "guide-e2e"); err != nil {
		t.Fatal(err)
	}

	bundleStore := bundle.NewLocalStore(filepath.Join(tempDir, "source-store"))
	executionStore := executionbundle.NewLocalStore(filepath.Join(tempDir, "execution-store"))
	fileCatalog := catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json"))
	runtimeRunner := &actionruntime.Runner{
		Store:         bundleStore,
		ArtifactStore: executionStore,
		CacheRoot:     filepath.Join(tempDir, "local-cache"),
	}
	handler := New(Config{
		Store:            stateStore,
		Catalog:          fileCatalog,
		Syncer:           &syncer.Syncer{Store: bundleStore, CloneRoot: filepath.Join(tempDir, "clones")},
		ExecutionBundles: runtimeRunner,
		GitSources:       gitsource.NewFileRegistry(filepath.Join(tempDir, "git-sources.json")),
		WorkerToken:      workerToken,
		ArtifactStore:    executionStore,
		SecretKey:        secretKey,
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	repoDir := materializeRuntimeSecretsGuideRepo(t)
	sourceID := registerE2EGitSource(t, server.URL, workspaceID, repoDir, appKey)
	syncE2EGitSource(t, server.URL, workspaceID, sourceID)
	publishE2EGitSource(t, server.URL, workspaceID, sourceID)

	var issued struct {
		Client   clientView `json:"client"`
		APIToken string     `json:"api_token"`
	}
	runtimeSecretsGuideRequest(t, http.MethodPost, server.URL+"/api/w/"+workspaceID+"/clients", "", `{"name":"Runtime Secrets Guide"}`, http.StatusCreated, &issued)
	if issued.Client.ID == "" || issued.APIToken == "" {
		t.Fatalf("issued client = %#v", issued)
	}

	runtimeSecretsGuideRequest(t, http.MethodPost, server.URL+"/api/w/"+workspaceID+"/variables", "", `{"path":"secrets/partner-token","value":"`+secretValue+`","is_secret":true,"app_key":"`+appKey+`"}`, http.StatusOK, nil)
	inputConfigURL := server.URL + "/api/w/" + workspaceID + "/apps/" + appKey + "/input-configs"
	runtimeSecretsGuideRequest(t, http.MethodPut, inputConfigURL, "", `{"action_key":"`+actionKey+`","client_id":"`+issued.Client.ID+`","config":{"partnerToken":"plaintext-is-forbidden"},"locked_keys":["partnerToken"]}`, http.StatusBadRequest, nil)
	runtimeSecretsGuideRequest(t, http.MethodPut, inputConfigURL, "", `{"action_key":"`+actionKey+`","client_id":"`+issued.Client.ID+`","config":{"partnerToken":"$var:secrets/partner-token"},"locked_keys":["partnerToken"]}`, http.StatusOK, nil)

	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stateBytes, []byte(secretValue)) {
		t.Fatal("local state contains Secret Variable plaintext")
	}

	runtimeSecretsGuideRequest(t, http.MethodPost, server.URL+"/api/v1/workspaces/"+workspaceID+"/runs", issued.APIToken, `{"app":"`+appKey+`","action":"`+actionKey+`","input":{"orderId":"LOCKED","partnerToken":"spoofed"}}`, http.StatusBadRequest, nil)

	localRun := admitRuntimeSecretsGuideRun(t, server.URL, workspaceID, issued.APIToken, appKey, actionKey, "ORDER-LOCAL")
	assertRuntimeSecretsGuideJobEncrypted(t, statePath, localRun.RunID, secretValue)
	secretStore := secretbackend.NewDatabase(stateStore, secretKey, "")
	localProcessor := worker.Processor{
		Store:           stateStore,
		Runner:          runtimeRunner,
		RuntimeResolver: runtimeconfig.New(stateStore, secretStore),
		WorkerID:        "runtime-secrets-local",
		Group:           "guide-e2e",
		LeaseTTL:        time.Minute,
	}
	processed, err := localProcessor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("local Worker processed=%v err=%v", processed, err)
	}
	assertRuntimeSecretsGuideRun(t, stateStore, localRun.RunID, "ORDER-LOCAL", len(secretValue))

	remoteRun := admitRuntimeSecretsGuideRun(t, server.URL, workspaceID, issued.APIToken, appKey, actionKey, "ORDER-REMOTE")
	assertRuntimeSecretsGuideJobEncrypted(t, statePath, remoteRun.RunID, secretValue)
	remoteBackend := remoteworker.New(server.URL, workerToken)
	remoteRunner := &actionruntime.Runner{
		ArtifactStore: remoteworker.ArtifactStore{Client: remoteBackend},
		CacheRoot:     filepath.Join(tempDir, "remote-cache"),
		BaseURL:       server.URL,
		APIToken:      workerToken,
	}
	remoteProfiles, err := remoteRunner.ExecutionProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	remoteRecord := state.WorkerRecord{
		ID: "runtime-secrets-remote", Group: "guide-e2e", Slots: 1, Status: state.WorkerStatusActive,
		ExecutionProfiles: remoteProfiles,
	}
	if err := remoteBackend.RegisterWorker(context.Background(), remoteRecord); err != nil {
		t.Fatal(err)
	}
	defer remoteBackend.DeregisterWorker(context.Background(), remoteRecord.ID)
	remoteProcessor := worker.Processor{
		Store:    remoteBackend,
		Runner:   remoteRunner,
		WorkerID: remoteRecord.ID,
		Group:    remoteRecord.Group,
		LeaseTTL: time.Minute,
	}
	processed, err = remoteProcessor.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("remote Worker processed=%v err=%v", processed, err)
	}
	assertRuntimeSecretsGuideRun(t, stateStore, remoteRun.RunID, "ORDER-REMOTE", len(secretValue))
}

func materializeRuntimeSecretsGuideRepo(t *testing.T) string {
	t.Helper()
	sourceDir := filepath.Join("..", "..", "examples", "typescript-runtime-secrets")
	repoDir := filepath.Join(t.TempDir(), "runtime-secrets-example")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		source, err := os.Open(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		destination, err := os.Create(filepath.Join(repoDir, entry.Name()))
		if err != nil {
			source.Close()
			t.Fatal(err)
		}
		_, copyErr := io.Copy(destination, source)
		closeDestinationErr := destination.Close()
		closeSourceErr := source.Close()
		if copyErr != nil || closeDestinationErr != nil || closeSourceErr != nil {
			t.Fatalf("copy %s: copy=%v close destination=%v close source=%v", entry.Name(), copyErr, closeDestinationErr, closeSourceErr)
		}
	}
	runE2ECommand(t, repoDir, "git", "init")
	runE2ECommand(t, repoDir, "git", "checkout", "-b", "main")
	runE2ECommand(t, repoDir, "git", "config", "user.email", "windforce-guide@example.invalid")
	runE2ECommand(t, repoDir, "git", "config", "user.name", "Windforce Guide")
	runE2ECommand(t, repoDir, "git", "add", ".")
	runE2ECommand(t, repoDir, "git", "commit", "-m", "runtime secrets guide example")
	return repoDir
}

func runtimeSecretsGuideRequest(t *testing.T, method string, url string, token string, body string, wantStatus int, target any) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("X-Windforce-Actor", "runtime-secrets-guide")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, payload)
	}
	if target != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, target); err != nil {
			t.Fatalf("decode %s %s: %v: %s", method, url, err, payload)
		}
	}
}

func admitRuntimeSecretsGuideRun(t *testing.T, baseURL string, workspaceID string, token string, appKey string, actionKey string, orderID string) invocationRunView {
	t.Helper()
	var run invocationRunView
	runtimeSecretsGuideRequest(t, http.MethodPost, baseURL+"/api/v1/workspaces/"+workspaceID+"/runs", token, `{"app":"`+appKey+`","action":"`+actionKey+`","input":{"orderId":"`+orderID+`"}}`, http.StatusCreated, &run)
	return run
}

func assertRuntimeSecretsGuideJobEncrypted(t *testing.T, statePath string, runID string, secretValue string) {
	t.Helper()
	contents, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot state.Snapshot
	if err := json.Unmarshal(contents, &snapshot); err != nil {
		t.Fatal(err)
	}
	var job state.Job
	found := false
	for _, candidate := range snapshot.Jobs {
		if candidate.RunID == runID {
			job = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Job for Run %s not found", runID)
	}
	if !wfcrypto.IsEnc(job.Payload.Input) {
		t.Fatalf("Job input is not encrypted at rest: %s", job.Payload.Input)
	}
	if bytes.Contains(job.Payload.Input, []byte(secretValue)) {
		t.Fatal("encrypted Job input contains Secret Variable plaintext")
	}
}

func assertRuntimeSecretsGuideRun(t *testing.T, store *state.LocalStore, runID string, orderID string, secretLength int) {
	t.Helper()
	run, err := store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != state.RunSucceeded || run.Result == nil {
		t.Fatalf("Run %s = state %s result %#v", runID, run.State, run.Result)
	}
	var output struct {
		OrderID        string `json:"orderId"`
		SecretResolved bool   `json:"secretResolved"`
		SecretLength   int    `json:"secretLength"`
	}
	if err := json.Unmarshal(run.Result.Output, &output); err != nil {
		t.Fatalf("decode Run %s output: %v: %s", runID, err, run.Result.Output)
	}
	if output.OrderID != orderID || !output.SecretResolved || output.SecretLength != secretLength {
		t.Fatalf("Run %s output = %#v", runID, output)
	}
}
