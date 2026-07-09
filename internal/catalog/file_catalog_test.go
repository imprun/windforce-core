package catalog

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/imprun/windforce-lite/internal/contract"
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
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(snapshot.History[0].ID) {
		t.Fatalf("history id = %q, want UUID app version id", snapshot.History[0].ID)
	}
}
