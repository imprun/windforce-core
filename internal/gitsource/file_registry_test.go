package gitsource

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRegistryPreservesRawSubpath(t *testing.T) {
	registry := NewFileRegistry(filepath.Join(t.TempDir(), "git-sources.json"))
	created, err := registry.Create(context.Background(), Source{
		Workspace: "ws-a",
		Name:      "source-a",
		RepoURL:   "https://example.test/repo.git",
		Branch:    "main",
		Subpath:   "/apps/echo",
	})
	if err != nil {
		t.Fatalf("Create error = %v", err)
	}
	if created.Subpath != "/apps/echo" {
		t.Fatalf("Subpath = %q, want raw value", created.Subpath)
	}
}

func TestFileRegistryPersistsValidatedAppKeyAndClearsItWhenSourceChanges(t *testing.T) {
	registry := NewFileRegistry(filepath.Join(t.TempDir(), "git-sources.json"))
	created, err := registry.Create(context.Background(), Source{
		Workspace: "ws-a",
		Name:      "source-a",
		RepoURL:   "https://example.test/repo.git",
		Branch:    "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	synced, err := registry.MarkSynced(context.Background(), "ws-a", created.ID, " echo ", "commit-a", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if synced.AppKey != "echo" {
		t.Fatalf("AppKey = %q, want echo", synced.AppKey)
	}

	loaded, err := registry.Get(context.Background(), "ws-a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AppKey != "echo" {
		t.Fatalf("persisted AppKey = %q, want echo", loaded.AppKey)
	}

	branch := "next"
	changed, err := registry.Patch(context.Background(), "ws-a", created.ID, Patch{Branch: &branch})
	if err != nil {
		t.Fatal(err)
	}
	if changed.AppKey != "" || changed.LastSyncedCommit != nil || changed.LastSyncedAt != nil {
		t.Fatalf("changed source retained validated identity: %#v", changed)
	}
}
