package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-lite/internal/catalog"
	"github.com/imprun/windforce-lite/internal/contract"
	"github.com/imprun/windforce-lite/internal/gitsource"
	"github.com/imprun/windforce-lite/internal/state"
)

func TestImportReleaseCatalogMigratesFileStateIdempotently(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "catalog.json")
	sourcesPath := filepath.Join(tempDir, "git-sources.json")
	legacyCatalog := catalog.NewFileCatalog(catalogPath)
	deployment := contract.Deployment{
		Workspace:   "workspace-a",
		GitSourceID: "1",
		App:         "echo",
		Commit:      "commit-a",
		Entrypoint:  "main.py",
		ObjectURI:   "bundle://workspace-a/1/commit-a",
		Actions:     map[string]contract.Action{"run": {Action: "run"}},
	}
	if err := legacyCatalog.UpsertDeployment(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	releasedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	commit := "commit-a"
	sources := gitsource.NewFileRegistry(sourcesPath)
	if err := sources.Upsert(ctx, gitsource.Source{
		Workspace:        "workspace-a",
		ID:               "1",
		Name:             "echo",
		RepoURL:          "https://example.test/echo.git",
		LastSyncedCommit: &commit,
		LastSyncedAt:     &releasedAt,
	}); err != nil {
		t.Fatal(err)
	}

	target := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	if err := importReleaseCatalog(ctx, target, catalogPath, sources); err != nil {
		t.Fatal(err)
	}
	if err := importReleaseCatalog(ctx, target, catalogPath, sources); err != nil {
		t.Fatal(err)
	}
	snapshot, err := target.LoadCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Deployments) != 1 || len(snapshot.History) != 1 {
		t.Fatalf("import counts = deployments:%d history:%d", len(snapshot.Deployments), len(snapshot.History))
	}
	marker := snapshot.SourceMarkers[catalog.SourceReleaseKey("workspace-a", "1")]
	if marker.Commit != commit || !marker.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("imported marker = %#v", marker)
	}
}
