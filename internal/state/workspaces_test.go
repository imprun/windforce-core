package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalWorkspaceLifecycleAndTokenHashing(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))

	defaultWorkspace, err := store.GetWorkspace(ctx, contract.DefaultWorkspace)
	if err != nil || defaultWorkspace.Status != WorkspaceActive {
		t.Fatalf("default workspace = %#v, %v", defaultWorkspace, err)
	}

	created, err := store.CreateWorkspace(ctx, "team-a", "Team A", HashWorkspaceToken("secret-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Team A" || !WorkspaceTokenMatches(created, "secret-a") || WorkspaceTokenMatches(created, "secret-b") {
		t.Fatalf("created workspace = %#v", created)
	}
	if _, err := store.CreateWorkspace(ctx, "team-a", "Duplicate", "", "admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create error = %v", err)
	}

	updated, err := store.UpdateWorkspace(ctx, "team-a", "Platform Team", "operator")
	if err != nil || updated.Name != "Platform Team" {
		t.Fatalf("updated workspace = %#v, %v", updated, err)
	}
	rotated, err := store.RotateWorkspaceToken(ctx, "team-a", HashWorkspaceToken("secret-b"), "operator")
	if err != nil || !WorkspaceTokenMatches(rotated, "secret-b") || WorkspaceTokenMatches(rotated, "secret-a") {
		t.Fatalf("rotated workspace = %#v, %v", rotated, err)
	}
	archived, err := store.ArchiveWorkspace(ctx, "team-a", "operator")
	if err != nil || archived.Status != WorkspaceArchived {
		t.Fatalf("archived workspace = %#v, %v", archived, err)
	}
	if _, err := store.ArchiveWorkspace(ctx, contract.DefaultWorkspace, "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("archive default error = %v", err)
	}
	if _, err := store.UpdateWorkspace(ctx, "team-a", "Archived Team", "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update archived error = %v, want invalid state", err)
	}
	if _, err := store.RotateWorkspaceToken(ctx, "team-a", HashWorkspaceToken("secret-c"), "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rotate archived error = %v, want invalid state", err)
	}

	audit, err := store.ListWorkspaceAudit(ctx, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"archived", "token_rotated", "updated", "created"}
	if len(audit) != len(wantKinds) {
		t.Fatalf("audit = %#v", audit)
	}
	for index, kind := range wantKinds {
		if audit[index].Kind != kind {
			t.Fatalf("audit[%d].kind = %q, want %q", index, audit[index].Kind, kind)
		}
	}
}

func TestPostgresWorkspaceLifecycle(t *testing.T) {
	dsn := os.Getenv("WINDFORCE_LITE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("WINDFORCE_LITE_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	workspaceID := fmt.Sprintf("test-ws-%d", time.Now().UnixNano())
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_registry WHERE id=$1`, workspaceID)
	}()

	created, err := store.CreateWorkspace(ctx, workspaceID, "Postgres Workspace", HashWorkspaceToken("secret-a"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != workspaceID || !WorkspaceTokenMatches(created, "secret-a") {
		t.Fatalf("created workspace = %#v", created)
	}
	listed, err := store.ListWorkspaces(ctx)
	found := false
	for _, workspace := range listed {
		found = found || workspace.ID == workspaceID
	}
	if err != nil || !found {
		t.Fatalf("listed workspaces = %#v, %v", listed, err)
	}
	updated, err := store.UpdateWorkspace(ctx, workspaceID, "Updated Workspace", "operator")
	if err != nil || updated.Name != "Updated Workspace" {
		t.Fatalf("updated workspace = %#v, %v", updated, err)
	}
	rotated, err := store.RotateWorkspaceToken(ctx, workspaceID, HashWorkspaceToken("secret-b"), "operator")
	if err != nil || !WorkspaceTokenMatches(rotated, "secret-b") || WorkspaceTokenMatches(rotated, "secret-a") {
		t.Fatalf("rotated workspace = %#v, %v", rotated, err)
	}
	archived, err := store.ArchiveWorkspace(ctx, workspaceID, "operator")
	if err != nil || archived.Status != WorkspaceArchived {
		t.Fatalf("archived workspace = %#v, %v", archived, err)
	}
	if _, err := store.UpdateWorkspace(ctx, workspaceID, "Archived Workspace", "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update archived error = %v, want invalid state", err)
	}
	if _, err := store.RotateWorkspaceToken(ctx, workspaceID, HashWorkspaceToken("secret-c"), "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rotate archived error = %v, want invalid state", err)
	}
	audit, err := store.ListWorkspaceAudit(ctx, workspaceID)
	if err != nil || len(audit) != 4 {
		t.Fatalf("workspace audit = %#v, %v", audit, err)
	}
}
