package runtime

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionbundle"
)

type executionBundleContextKey struct{}

type observingExecutionBundleStore struct {
	executionbundle.Store
	started chan context.Context
	release chan struct{}
	fetches atomic.Int32
}

func (s *observingExecutionBundleStore) FetchTo(ctx context.Context, destinationDir string, digest string) (executionbundle.Descriptor, error) {
	s.fetches.Add(1)
	s.started <- ctx
	select {
	case <-s.release:
	case <-ctx.Done():
		return executionbundle.Descriptor{}, ctx.Err()
	}
	return s.Store.FetchTo(ctx, destinationDir, digest)
}

func TestOpenExecutionBundlePreservesContextAcrossSingleflightCallerCancellation(t *testing.T) {
	tempDir := t.TempDir()
	artifactDir := filepath.Join(tempDir, "artifact")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readyValue, err := sourceReadyValue(context.Background(), contract.ScriptLangGo, "python", "bun", "go")
	if err != nil {
		t.Fatalf("create runtime fingerprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, sourceReadyFile), []byte(readyValue), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	baseStore := executionbundle.NewLocalStore(filepath.Join(tempDir, "store"))
	descriptor, err := baseStore.Publish(context.Background(), artifactDir)
	if err != nil {
		t.Fatalf("publish execution bundle: %v", err)
	}
	store := &observingExecutionBundleStore{
		Store:   baseStore,
		started: make(chan context.Context, 1),
		release: make(chan struct{}),
	}
	runner := Runner{
		ArtifactStore: store,
		CacheRoot:     filepath.Join(tempDir, "cache"),
	}
	deployment := contract.Deployment{
		BundleDigest: descriptor.Digest,
		BundleURI:    descriptor.URI,
		ScriptLang:   contract.ScriptLangGo,
	}

	type fetchResult struct {
		dir string
		err error
	}
	firstCtx, cancelFirst := context.WithCancel(context.WithValue(context.Background(), executionBundleContextKey{}, "lease-a"))
	firstResult := make(chan fetchResult, 1)
	go func() {
		dir, fetchErr := runner.openExecutionBundle(firstCtx, deployment)
		firstResult <- fetchResult{dir: dir, err: fetchErr}
	}()

	var fetchCtx context.Context
	select {
	case fetchCtx = <-store.started:
	case <-time.After(5 * time.Second):
		t.Fatal("execution bundle fetch did not start")
	}
	if got := fetchCtx.Value(executionBundleContextKey{}); got != "lease-a" {
		t.Fatalf("fetch context value = %v, want lease-a", got)
	}

	secondCtx := context.WithValue(context.Background(), executionBundleContextKey{}, "lease-b")
	secondResult := make(chan fetchResult, 1)
	go func() {
		dir, fetchErr := runner.openExecutionBundle(secondCtx, deployment)
		secondResult <- fetchResult{dir: dir, err: fetchErr}
	}()
	select {
	case result := <-secondResult:
		t.Fatalf("shared fetch returned before release: dir=%q err=%v", result.dir, result.err)
	case <-time.After(20 * time.Millisecond):
	}

	cancelFirst()
	select {
	case result := <-firstResult:
		if name, ok := ErrorName(result.err); !ok || name != "BundleFetchCanceled" {
			t.Fatalf("first caller error = %v (name %q), want BundleFetchCanceled", result.err, name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled caller did not return")
	}

	close(store.release)
	select {
	case result := <-secondResult:
		if result.err != nil {
			t.Fatalf("shared fetch returned error: %v", result.err)
		}
		if result.dir == "" {
			t.Fatal("shared fetch returned an empty bundle directory")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shared fetch did not complete")
	}
	if got := store.fetches.Load(); got != 1 {
		t.Fatalf("artifact fetches = %d, want 1", got)
	}
}
