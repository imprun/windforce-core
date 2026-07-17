package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestFileCatalogUpsertAndGet(t *testing.T) {
	catalog := NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	deployment := contract.Deployment{
		App:       "echo",
		Commit:    "commit-a",
		ObjectURI: "bundle://echo/commit-a",
		Actions: map[string]contract.Action{
			"echo": {Action: "echo"},
		},
	}
	if err := catalog.UpsertDeployment(context.Background(), deployment); err != nil {
		t.Fatalf("UpsertDeployment returned error: %v", err)
	}

	got, err := catalog.GetDeployment(context.Background(), "echo")
	if err != nil {
		t.Fatalf("GetDeployment returned error: %v", err)
	}
	if got.Commit != "commit-a" {
		t.Fatalf("commit = %q", got.Commit)
	}
	if got.Tag != "default" || got.TimeoutS != 300 || got.ScriptLang != "typescript" {
		t.Fatalf("defaults = tag:%q timeout:%d scriptLang:%q", got.Tag, got.TimeoutS, got.ScriptLang)
	}
	if got.UpdatedAt == nil {
		t.Fatalf("deployment updatedAt was not set")
	}
	if got.Actions["echo"].UpdatedAt == nil {
		t.Fatalf("action updatedAt was not set")
	}
	snapshot, err := catalog.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(snapshot.History) != 1 {
		t.Fatalf("history count = %d, want 1", len(snapshot.History))
	}
	if snapshot.History[0].Commit != "commit-a" || snapshot.History[0].Status != "deployed" {
		t.Fatalf("history item = %#v", snapshot.History[0])
	}
	if snapshot.History[0].Deployment.Tag != "default" ||
		snapshot.History[0].Deployment.TimeoutS != 300 ||
		snapshot.History[0].Deployment.ScriptLang != "typescript" {
		t.Fatalf("history deployment defaults = %#v", snapshot.History[0].Deployment)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(snapshot.History[0].ID) {
		t.Fatalf("history id = %q, want UUID app version id", snapshot.History[0].ID)
	}
}

func TestFileCatalogReleaseCandidatesAreImmutable(t *testing.T) {
	store := NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	ctx := context.Background()
	firstSyncedAt := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	first := contract.Deployment{
		Workspace:   "ws-a",
		GitSourceID: "source-a",
		App:         "echo",
		Commit:      "commit-a",
		Entrypoint:  "main.py",
		Actions:     map[string]contract.Action{"run": {Action: "run"}},
	}
	saved, err := store.SaveReleaseCandidate(ctx, first, firstSyncedAt)
	if err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.Entrypoint = "changed.py"
	unchanged, err := store.SaveReleaseCandidate(ctx, changed, firstSyncedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Deployment.Entrypoint != "main.py" || !unchanged.SyncedAt.Equal(saved.SyncedAt) {
		t.Fatalf("candidate was mutated: %#v", unchanged)
	}

	second := first
	second.Commit = "commit-b"
	second.Entrypoint = "next.py"
	secondSyncedAt := firstSyncedAt.Add(2 * time.Minute)
	if _, err := store.SaveReleaseCandidate(ctx, second, secondSyncedAt); err != nil {
		t.Fatal(err)
	}
	latest, err := store.GetLatestReleaseCandidate(ctx, "ws-a", "source-a")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Deployment.Commit != "commit-b" || latest.Deployment.Entrypoint != "next.py" || !latest.SyncedAt.Equal(secondSyncedAt) {
		t.Fatalf("latest candidate = %#v", latest)
	}
}

func TestFileCatalogRollbackSwitchesActiveReleaseWithoutChangingHistoryOrCandidate(t *testing.T) {
	store := NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	ctx := context.Background()
	first := contract.Deployment{
		Workspace: "ws-a", GitSourceID: "source-a", App: "echo", Commit: "commit-a",
		BundleDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Actions:      map[string]contract.Action{"run": {Action: "run"}},
	}
	second := first
	second.Commit = "commit-b"
	second.BundleDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := store.PublishRelease(ctx, first, time.Date(2026, 7, 18, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveReleaseCandidate(ctx, second, time.Date(2026, 7, 18, 2, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishRelease(ctx, second, time.Date(2026, 7, 18, 2, 2, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.RollbackRelease(ctx, ReleaseRollbackRequest{
		Workspace: "ws-a", App: "echo", ReleaseID: before.History[0].ID,
		Actor: "operator", Reason: "restore stable release",
		RolledBackAt: time.Date(2026, 7, 18, 2, 3, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := DeploymentKey("ws-a", "echo")
	if after.Deployments[key].Commit != "commit-a" || after.ActiveHistoryIDs[key] != before.History[0].ID {
		t.Fatalf("active release = deployment %#v id %q", after.Deployments[key], after.ActiveHistoryIDs[key])
	}
	if len(after.History) != 2 || len(after.Candidates) != 1 {
		t.Fatalf("rollback changed immutable records: history=%d candidates=%d", len(after.History), len(after.Candidates))
	}
	if result.PreviousReleaseID != before.History[1].ID || result.Audit.Kind != "release_rolled_back" {
		t.Fatalf("rollback result = %#v", result)
	}
	if _, err := store.RollbackRelease(ctx, ReleaseRollbackRequest{
		Workspace: "ws-a", App: "echo", ReleaseID: before.History[0].ID,
		Actor: "operator", Reason: "duplicate",
	}); !errors.Is(err, ErrReleaseAlreadyActive) {
		t.Fatalf("already-active rollback error = %v", err)
	}
}

func TestFileCatalogScopesDeploymentsByWorkspace(t *testing.T) {
	catalog := NewFileCatalog(filepath.Join(t.TempDir(), "catalog.json"))
	for _, deployment := range []contract.Deployment{
		{Workspace: "ws-a", App: "echo", Commit: "commit-a", Entrypoint: "main.ts", Actions: map[string]contract.Action{"echo": {Action: "echo"}}},
		{Workspace: "ws-b", App: "echo", Commit: "commit-b", Entrypoint: "main.ts", Actions: map[string]contract.Action{"echo": {Action: "echo"}}},
	} {
		if err := catalog.UpsertDeployment(context.Background(), deployment); err != nil {
			t.Fatalf("UpsertDeployment returned error: %v", err)
		}
	}

	gotA, err := catalog.GetDeploymentForWorkspace(context.Background(), "ws-a", "echo")
	if err != nil {
		t.Fatalf("GetDeploymentForWorkspace(ws-a) returned error: %v", err)
	}
	gotB, err := catalog.GetDeploymentForWorkspace(context.Background(), "ws-b", "echo")
	if err != nil {
		t.Fatalf("GetDeploymentForWorkspace(ws-b) returned error: %v", err)
	}
	if gotA.Commit != "commit-a" || gotB.Commit != "commit-b" {
		t.Fatalf("workspace deployments crossed: ws-a=%q ws-b=%q", gotA.Commit, gotB.Commit)
	}
	if _, err := catalog.GetDeployment(context.Background(), "echo"); err != ErrDeploymentNotFound {
		t.Fatalf("legacy default lookup error = %v, want ErrDeploymentNotFound", err)
	}
}

func TestFileCatalogMigratesLegacyAppKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	legacy := Snapshot{
		Deployments: map[string]contract.Deployment{
			"echo": {Workspace: "ws-a", App: "echo", Commit: "commit-a", Entrypoint: "main.ts", Actions: map[string]contract.Action{"echo": {Action: "echo"}}},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	catalog := NewFileCatalog(path)
	got, err := catalog.GetDeploymentForWorkspace(context.Background(), "ws-a", "echo")
	if err != nil {
		t.Fatalf("GetDeploymentForWorkspace returned error: %v", err)
	}
	if got.Commit != "commit-a" {
		t.Fatalf("commit = %q, want commit-a", got.Commit)
	}
	if got.Tag != "default" || got.TimeoutS != 300 || got.ScriptLang != "typescript" {
		t.Fatalf("legacy defaults = tag:%q timeout:%d scriptLang:%q", got.Tag, got.TimeoutS, got.ScriptLang)
	}
	snapshot, err := catalog.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, ok := snapshot.Deployments["ws-a/echo"]; !ok {
		t.Fatalf("normalized deployment key missing: %#v", snapshot.Deployments)
	}
}
