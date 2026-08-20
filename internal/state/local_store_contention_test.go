package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/imprun/windforce-core/internal/contract"
	controlevent "github.com/imprun/windforce-core/internal/event"
	"github.com/imprun/windforce-core/internal/webhook"
)

func TestLocalStoreEmptyWorkerClaimDoesNotWaitForWriteLock(t *testing.T) {
	store := seededEmptyLocalStore(t)
	if err := store.RegisterWorker(context.Background(), WorkerRecord{ID: "worker-empty"}); err != nil {
		t.Fatal(err)
	}
	before := localSnapshotAndMarkedMtime(t, store)
	owner := lockLocalStoreFile(t, store)
	defer func() { _ = owner.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := store.ClaimJobForWorkerScope(ctx, "worker-empty", nil, nil, nil, time.Minute)
	if !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("empty worker claim error = %v, want %v", err, ErrNoQueuedJob)
	}
	assertLocalStateUnchanged(t, store, before)
}

func TestLocalStoreEmptyWebhookClaimDoesNotWaitForWriteLock(t *testing.T) {
	store := seededEmptyLocalStore(t)
	before := localSnapshotAndMarkedMtime(t, store)
	owner := lockLocalStoreFile(t, store)
	defer func() { _ = owner.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := store.ClaimDelivery(ctx, "dispatcher-empty", time.Minute)
	if !errors.Is(err, webhook.ErrNoPendingDelivery) {
		t.Fatalf("empty webhook claim error = %v, want %v", err, webhook.ErrNoPendingDelivery)
	}
	assertLocalStateUnchanged(t, store, before)
}

func TestLocalStoreConcurrentEnqueueRemainsClaimable(t *testing.T) {
	for iteration := 0; iteration < 10; iteration++ {
		t.Run(NewID("iteration"), func(t *testing.T) {
			store := seededEmptyLocalStore(t)
			deployment := contract.Deployment{
				Workspace: "default", App: "concurrent-app", Commit: "concurrent-commit",
				Actions: map[string]contract.Action{"run": {Action: "run", Command: []string{"helper"}}},
			}
			run := NewRun("api", NewID("run"), deployment.App, "run", deployment, json.RawMessage(`{}`))
			job := NewActionJob(run, json.RawMessage(`{}`))
			start := make(chan struct{})
			var wait sync.WaitGroup
			wait.Add(1)
			var enqueueErr error
			go func() {
				defer wait.Done()
				<-start
				enqueueErr = store.CreateRunAndEnqueue(context.Background(), run, job)
			}()
			close(start)
			claimed, _, claimErr := store.ClaimJob(context.Background(), "worker-concurrent", time.Minute)
			wait.Wait()
			if enqueueErr != nil {
				t.Fatal(enqueueErr)
			}
			if errors.Is(claimErr, ErrNoQueuedJob) {
				claimed, _, claimErr = store.ClaimJob(context.Background(), "worker-concurrent", time.Minute)
			}
			if claimErr != nil {
				t.Fatal(claimErr)
			}
			if claimed.ID != job.ID {
				t.Fatalf("claimed Job = %q, want enqueued Job", claimed.ID)
			}
		})
	}
}

func TestLocalStoreConcurrentWebhookPublishRemainsClaimable(t *testing.T) {
	for iteration := 0; iteration < 10; iteration++ {
		t.Run(NewID("iteration"), func(t *testing.T) {
			store := seededEmptyLocalStore(t)
			store.ConfigureInputCrypto("local-test-secret-key", "")
			if _, err := store.CreateSubscription(context.Background(), webhook.Subscription{
				WorkspaceID: "workspace-a", Name: "Concurrent releases", Endpoint: "https://hooks.example.test/releases",
				SigningSecret: "signing-secret-0123456789", EventTypes: []string{controlevent.ReleasePublishedType},
				Enabled: true, CreatedBy: "operator:test",
			}); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wait sync.WaitGroup
			wait.Add(1)
			var publishErr error
			go func() {
				defer wait.Done()
				<-start
				_, publishErr = store.PublishRelease(context.Background(), releaseCatalogDeployment("workspace-a", "source-a", "echo", "commit-a"), time.Now().UTC())
			}()
			close(start)
			claimed, claimErr := store.ClaimDelivery(context.Background(), "dispatcher-concurrent", time.Minute)
			wait.Wait()
			if publishErr != nil {
				t.Fatal(publishErr)
			}
			if errors.Is(claimErr, webhook.ErrNoPendingDelivery) {
				claimed, claimErr = store.ClaimDelivery(context.Background(), "dispatcher-concurrent", time.Minute)
			}
			if claimErr != nil {
				t.Fatal(claimErr)
			}
			if claimed.Delivery.State != webhook.DeliveryDelivering || claimed.Delivery.Attempt != 1 {
				t.Fatalf("claimed delivery state=%q attempt=%d, want delivering attempt 1", claimed.Delivery.State, claimed.Delivery.Attempt)
			}
		})
	}
}

type localStateMarker struct {
	revision int64
	mtime    time.Time
}

func seededEmptyLocalStore(t *testing.T) *LocalStore {
	t.Helper()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.write(newSnapshot()); err != nil {
		t.Fatal(err)
	}
	return store
}

func localSnapshotAndMarkedMtime(t *testing.T, store *LocalStore) localStateMarker {
	t.Helper()
	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	marker := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(store.Path, marker, marker); err != nil {
		t.Fatal(err)
	}
	return localStateMarker{revision: snapshot.SnapshotRevision, mtime: marker}
}

func assertLocalStateUnchanged(t *testing.T, store *LocalStore, before localStateMarker) {
	t.Helper()
	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SnapshotRevision != before.revision {
		t.Fatalf("snapshot revision = %d, want unchanged %d", snapshot.SnapshotRevision, before.revision)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(before.mtime) {
		t.Fatalf("state mtime = %s, want unchanged %s", info.ModTime(), before.mtime)
	}
}

func lockLocalStoreFile(t *testing.T, store *LocalStore) *flock.Flock {
	t.Helper()
	owner := flock.New(store.Path + ".lock")
	if err := owner.Lock(); err != nil {
		t.Fatal(err)
	}
	return owner
}
