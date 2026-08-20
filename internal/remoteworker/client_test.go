package remoteworker

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/server"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/telemetry"
	"github.com/imprun/windforce-core/internal/worker"
)

func TestArtifactStoreRejectsTarDigestMismatchBeforePromotion(t *testing.T) {
	payload := []byte("export const main = () => 'tampered'\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-tar")
		writer := tar.NewWriter(w)
		_ = writer.WriteHeader(&tar.Header{Name: "main.ts", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg})
		_, _ = writer.Write(payload)
		_ = writer.Close()
	}))
	defer srv.Close()

	destination := filepath.Join(t.TempDir(), "bundle")
	wrongDigest := "sha256:" + strings.Repeat("0", 64)
	_, err := (ArtifactStore{Client: New(srv.URL, "")}).FetchTo(context.Background(), destination, wrongDigest)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("FetchTo error = %v, want digest mismatch", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched artifact was promoted: %v", statErr)
	}
}

// The client must satisfy the worker backend and token provider contracts.
var (
	_ worker.Backend                  = (*Client)(nil)
	_ worker.JobTokenProvider         = (*Client)(nil)
	_ worker.ExecutionContextProvider = (*Client)(nil)
)

func TestClientLifecycleAgainstRealServer(t *testing.T) {
	tempDir := t.TempDir()
	store := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	srv := httptest.NewServer(server.New(server.Config{
		Store:   store,
		Catalog: catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json")),

		AdminToken:     "admin-secret",
		JobTokenSecret: "job-secret",
	}))
	defer srv.Close()

	deployment := contract.Deployment{
		Workspace:      "ws-a",
		App:            "echo",
		Commit:         "commit-a",
		RequiredLabels: []string{"browser"},
		Actions:        map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
	}
	run := state.NewRun("windforce", "run-remote", "echo", "run", deployment, json.RawMessage(`{"message":"hi"}`))
	run.TraceContext, _ = telemetry.ParseCarrier("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "vendor=value", "http")
	job := state.NewActionJob(run, nil)
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}

	client := New(srv.URL, "admin-secret")
	ctx := context.Background()
	if err := client.RegisterWorker(ctx, state.WorkerRecord{
		ID: "w-remote", EngineVersion: "v0.9.2", BuildRevision: "abcdef123456", Labels: []string{"browser"}, Slots: 1,
	}); err != nil {
		t.Fatal(err)
	}
	workers, err := store.ListWorkers(ctx)
	if err != nil || len(workers) != 1 || workers[0].EngineVersion != "v0.9.2" || workers[0].BuildRevision != "abcdef123456" {
		t.Fatalf("remote worker build identity = %#v, err=%v", workers, err)
	}
	if err := client.HeartbeatWorker(ctx, "w-remote"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := client.ClaimJobForWorker(ctx, "w-remote", nil, nil, time.Minute); !errors.Is(err, state.ErrForbidden) {
		t.Fatalf("labelless remote claim err = %v, want ErrForbidden", err)
	}

	claimed, lease, err := client.ClaimJobForWorker(ctx, "w-remote", nil, []string{"browser"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Payload.App != "echo" || lease.JobID != claimed.ID {
		t.Fatalf("claimed = %#v", claimed.Payload)
	}
	if claimed.TraceContext != run.TraceContext {
		t.Fatalf("remote Worker Plane trace context = %#v, want %#v", claimed.TraceContext, run.TraceContext)
	}
	if token := client.JobTokenFor(claimed.ID); !strings.HasPrefix(token, "wfjob_") {
		t.Fatalf("job token = %q, want pre-minted wfjob_ token", token)
	}

	heartbeat, err := client.HeartbeatJob(ctx, lease, time.Minute)
	if err != nil || !heartbeat.StillOwned {
		t.Fatalf("job heartbeat = %#v err=%v", heartbeat, err)
	}
	if err := client.AppendLogs(ctx, claimed.ID, "ws-a", "line-1\n"); err != nil {
		t.Fatal(err)
	}
	if err := client.CompleteJobSucceeded(ctx, lease, contract.JobResult{App: "echo", Action: "run", Output: json.RawMessage(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetRun(ctx, "run-remote")
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != state.RunSucceeded {
		t.Fatalf("run state = %s", stored.State)
	}

	failureRun := state.NewRun("windforce", "run-remote-failure", "echo", "run", deployment, json.RawMessage(`{"message":"fail"}`))
	failureJob := state.NewActionJob(failureRun, nil)
	if err := store.CreateRunAndEnqueue(ctx, failureRun, failureJob); err != nil {
		t.Fatal(err)
	}
	failedClaim, failedLease, err := client.ClaimJobForWorker(ctx, "w-remote", nil, deployment.RequiredLabels, time.Minute)
	if err != nil || failedClaim.ID != failureJob.ID {
		t.Fatalf("claim failure job = %#v, err=%v", failedClaim, err)
	}
	failureOutput := json.RawMessage(`{"name":"RuntimeBindingError","message":"could not apply runtime bindings","phase":"capability_run_open","reason":"capacity_unavailable","retryable":true}`)
	if err := client.CompleteJobFailed(ctx, failedLease, contract.JobResult{
		JobID: failedClaim.ID, App: "echo", Action: "run", Output: failureOutput,
		ExitCode: -1, Error: "could not apply runtime bindings",
	}); err != nil {
		t.Fatal(err)
	}
	storedFailure, err := store.GetRun(ctx, failureRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	var failureMetadata struct {
		Phase     string `json:"phase"`
		Reason    string `json:"reason"`
		Retryable bool   `json:"retryable"`
	}
	if storedFailure.Result != nil {
		if err := json.Unmarshal(storedFailure.Result.Output, &failureMetadata); err != nil {
			t.Fatal(err)
		}
	}
	if storedFailure.State != state.RunFailed || storedFailure.Result == nil ||
		failureMetadata.Phase != "capability_run_open" || failureMetadata.Reason != "capacity_unavailable" ||
		!failureMetadata.Retryable || storedFailure.Result.ExitCode != -1 || storedFailure.Result.Error != "could not apply runtime bindings" {
		t.Fatalf("remote binding failure round-trip = %#v", storedFailure.Result)
	}
	if err := client.DeregisterWorker(ctx, "w-remote"); err != nil {
		t.Fatal(err)
	}

	// Typed store errors survive the wire: recovery logic (errors.Is) must
	// behave exactly as against a local store.
	staleLease := lease
	staleLease.WorkerID = "someone-else"
	if err := client.CompleteJobSucceeded(ctx, staleLease, contract.JobResult{}); !errors.Is(err, state.ErrInvalidLease) {
		t.Fatalf("stale-lease complete err = %v, want ErrInvalidLease", err)
	}
	if err := client.HeartbeatWorker(ctx, "w-never-registered"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("unknown worker heartbeat err = %v, want ErrNotFound", err)
	}
}

func TestArtifactStoreFetchesAndExtracts(t *testing.T) {
	tempDir := t.TempDir()
	artifacts := executionbundle.NewLocalStore(filepath.Join(tempDir, "artifacts"))
	sourceDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(filepath.Join(sourceDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "sub", "main.py"), []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	descriptor, err := artifacts.Publish(context.Background(), sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	baseHandler := server.New(server.Config{
		Store:   state.NewLocalStore(filepath.Join(tempDir, "state.json")),
		Catalog: catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json")),

		AdminToken:    "admin-secret",
		ArtifactStore: artifacts,
	})
	var artifactQuery map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		artifactQuery = map[string]string{
			"job_id": r.URL.Query().Get("job_id"), "workspace": r.URL.Query().Get("workspace"),
			"worker_id": r.URL.Query().Get("worker_id"),
		}
		baseHandler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	client := New(srv.URL, "admin-secret")
	dest := filepath.Join(tempDir, "fetched")
	ctx := client.WithExecutionContext(context.Background(), state.Job{
		ID: "job-artifact", Payload: state.JobPayload{Workspace: "ws-a"},
	}, state.Lease{WorkerID: "worker-artifact"})
	if _, err := (ArtifactStore{Client: client}).FetchTo(ctx, dest, descriptor.Digest); err != nil {
		t.Fatal(err)
	}
	if artifactQuery["job_id"] != "job-artifact" || artifactQuery["workspace"] != "ws-a" || artifactQuery["worker_id"] != "worker-artifact" {
		t.Fatalf("artifact query = %#v", artifactQuery)
	}
	payload, err := os.ReadFile(filepath.Join(dest, "sub", "main.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "print") {
		t.Fatalf("fetched content = %q", payload)
	}
}

func TestArtifactStoreRoundTripsSymlinks(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "src")
	if err := os.MkdirAll(filepath.Join(sourceDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "tool.py"), []byte("print('hi')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "tool.py"), filepath.Join(sourceDir, "bin", "tool")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	artifacts := executionbundle.NewLocalStore(filepath.Join(tempDir, "artifacts"))
	descriptor, err := artifacts.Publish(context.Background(), sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{
		Store:   state.NewLocalStore(filepath.Join(tempDir, "state.json")),
		Catalog: catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json")),

		AdminToken:    "admin-secret",
		ArtifactStore: artifacts,
	}))
	defer srv.Close()

	dest := filepath.Join(tempDir, "fetched")
	if _, err := (ArtifactStore{Client: New(srv.URL, "admin-secret")}).FetchTo(context.Background(), dest, descriptor.Digest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatalf("symlink missing after fetch: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("bin/tool is not a symlink after fetch (mode %v)", info.Mode())
	}
	payload, err := os.ReadFile(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "print") {
		t.Fatalf("symlink does not resolve to content: %q", payload)
	}
	// Files after the symlink in walk order must survive too (the old tar
	// writer truncated everything past the first symlink).
	if _, err := os.Stat(filepath.Join(dest, "tool.py")); err != nil {
		t.Fatalf("entry after symlink missing: %v", err)
	}
}
