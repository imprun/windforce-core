package bundle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalStorePruneUnreferenced(t *testing.T) {
	root := t.TempDir()
	store := NewLocalStore(root)
	old := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	cutoff := old.Add(24 * time.Hour)

	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "active"}, old)
	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "history"}, old)
	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "candidate"}, old)
	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "orphan"}, old)
	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "recent"}, cutoff)

	result, err := store.PruneUnreferenced(context.Background(), PruneOptions{
		Referenced: []Reference{
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "active"},
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "history"},
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "candidate"},
		},
		Before: cutoff,
	})
	if err != nil {
		t.Fatalf("PruneUnreferenced returned error: %v", err)
	}
	if result.Discovered != 5 || result.Referenced != 3 || result.Recent != 1 || result.Eligible != 1 || result.Removed != 1 || result.Invalid != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertBundleExists(t, store, "active", true)
	assertBundleExists(t, store, "history", true)
	assertBundleExists(t, store, "candidate", true)
	assertBundleExists(t, store, "recent", true)
	assertBundleExists(t, store, "orphan", false)

	again, err := store.PruneUnreferenced(context.Background(), PruneOptions{
		Referenced: []Reference{
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "active"},
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "history"},
			{Workspace: "ws-a", GitSourceID: "source-a", Commit: "candidate"},
		},
		Before: cutoff,
	})
	if err != nil {
		t.Fatalf("second PruneUnreferenced returned error: %v", err)
	}
	if again.Eligible != 0 || again.Removed != 0 {
		t.Fatalf("second prune was not idempotent: %+v", again)
	}
}

func TestLocalStorePruneUnreferencedDryRunAndInvalidMarker(t *testing.T) {
	root := t.TempDir()
	store := NewLocalStore(root)
	old := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	writeTestBundle(t, store, Reference{Workspace: "ws-a", GitSourceID: "source-a", Commit: "orphan"}, old)

	invalidDir := store.bundleDir("ws-a", "source-a", "invalid")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, markerFile), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneUnreferenced(context.Background(), PruneOptions{
		Before: old.Add(time.Hour),
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("PruneUnreferenced returned error: %v", err)
	}
	if result.Eligible != 1 || result.Removed != 0 || result.Invalid != 1 {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	assertBundleExists(t, store, "orphan", true)
	if _, err := os.Stat(invalidDir); err != nil {
		t.Fatalf("invalid bundle must be retained: %v", err)
	}
}

func writeTestBundle(t *testing.T, store *LocalStore, reference Reference, completedAt time.Time) {
	t.Helper()
	dir := store.bundleDir(reference.Workspace, reference.GitSourceID, reference.Commit)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(marker{
		CompletedAt: completedAt,
		Commit:      reference.Commit,
		FileCount:   1,
		GitSourceID: reference.GitSourceID,
		Workspace:   reference.Workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertBundleExists(t *testing.T, store *LocalStore, commit string, want bool) {
	t.Helper()
	exists, err := store.Exists(context.Background(), "ws-a", "source-a", commit)
	if err != nil {
		t.Fatal(err)
	}
	if exists != want {
		t.Fatalf("bundle %q exists = %v, want %v", commit, exists, want)
	}
}
