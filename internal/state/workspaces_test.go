package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

func TestLocalWorkspaceTokenMigrationPreservesLegacyCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	legacyHash := HashWorkspaceToken("wfw_legacy-secret")
	legacy := Snapshot{
		Workspaces: map[string]Workspace{
			"legacy": {
				ID: "legacy", Name: "Legacy", Status: WorkspaceActive, TokenHash: legacyHash,
				CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now,
			},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewLocalStore(path)
	tokens, err := store.ListWorkspaceTokens(context.Background(), "legacy")
	if err != nil || len(tokens) != 1 || tokens[0].Name != "Legacy access token" {
		t.Fatalf("migrated tokens = %#v, %v", tokens, err)
	}
	if tokens[0].ID != "workspace_token_legacy" || tokens[0].TokenHash != legacyHash {
		t.Fatalf("migrated token = %#v", tokens[0])
	}
	if byHash, err := store.GetWorkspaceTokenByTokenHash(context.Background(), "legacy", legacyHash); err != nil || byHash.ID != tokens[0].ID {
		t.Fatalf("legacy token lookup = %#v, %v", byHash, err)
	}
	workspace, err := store.GetWorkspace(context.Background(), "legacy")
	if err != nil || workspace.TokenHash != "" {
		t.Fatalf("migrated workspace = %#v, %v", workspace, err)
	}
}

func TestLocalWorkspaceLifecycleAndNamedTokens(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.ConfigureInputCrypto("local-workspace-test-secret", "")

	defaultWorkspace, err := store.GetWorkspace(ctx, contract.DefaultWorkspace)
	if err != nil || defaultWorkspace.Status != WorkspaceActive {
		t.Fatalf("default workspace = %#v, %v", defaultWorkspace, err)
	}

	created, err := store.CreateWorkspace(ctx, "team-a", "Team A", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "Team A" {
		t.Fatalf("created workspace = %#v", created)
	}
	storedKey, version, err := store.GetWorkspaceKeyVersioned(ctx, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	if version != wfcrypto.WrappedDEKVersion {
		t.Fatalf("workspace key version = %d, want %d", version, wfcrypto.WrappedDEKVersion)
	}
	dek, err := wfcrypto.ResolveDEK(
		storedKey,
		version,
		[]string{wfcrypto.DeriveKEK("local-workspace-test-secret")},
	)
	if err != nil {
		t.Fatalf("resolve workspace DEK: %v", err)
	}
	if dek == "" || dek == wfcrypto.DeriveWorkspaceKey("local-workspace-test-secret", "team-a") {
		t.Fatal("workspace DEK was not generated randomly")
	}
	if _, err := store.CreateWorkspace(ctx, "team-a", "Duplicate", "admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate create error = %v", err)
	}
	token, err := store.CreateWorkspaceToken(ctx, "team-a", "CLI", HashWorkspaceToken("secret-a"), "admin")
	if err != nil || token.Name != "CLI" {
		t.Fatalf("created token = %#v, %v", token, err)
	}
	if byHash, err := store.GetWorkspaceTokenByTokenHash(ctx, "team-a", HashWorkspaceToken("secret-a")); err != nil || byHash.ID != token.ID {
		t.Fatalf("token lookup = %#v, %v", byHash, err)
	}

	updated, err := store.UpdateWorkspace(ctx, "team-a", "Platform Team", "operator")
	if err != nil || updated.Name != "Platform Team" {
		t.Fatalf("updated workspace = %#v, %v", updated, err)
	}
	rotated, err := store.RotateWorkspaceToken(ctx, "team-a", token.ID, HashWorkspaceToken("secret-b"), "operator")
	if err != nil || rotated.TokenHash != HashWorkspaceToken("secret-b") {
		t.Fatalf("rotated token = %#v, %v", rotated, err)
	}
	if _, err := store.GetWorkspaceTokenByTokenHash(ctx, "team-a", HashWorkspaceToken("secret-a")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token lookup error = %v", err)
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
	if _, err := store.RotateWorkspaceToken(ctx, "team-a", token.ID, HashWorkspaceToken("secret-c"), "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rotate archived error = %v, want invalid state", err)
	}

	audit, err := store.ListWorkspaceAudit(ctx, "team-a")
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"archived", "token_rotated", "updated", "token_created", "created"}
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
	store.ConfigureInputCrypto("postgres-workspace-test-secret", "")

	workspaceID := fmt.Sprintf("test-ws-%d", time.Now().UnixNano())
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_token WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_key WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_registry WHERE id=$1`, workspaceID)
	}()

	created, err := store.CreateWorkspace(ctx, workspaceID, "Postgres Workspace", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != workspaceID {
		t.Fatalf("created workspace = %#v", created)
	}
	storedKey, version, err := store.GetWorkspaceKeyVersioned(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if version != wfcrypto.WrappedDEKVersion {
		t.Fatalf("workspace key version = %d, want %d", version, wfcrypto.WrappedDEKVersion)
	}
	dek, err := wfcrypto.ResolveDEK(
		storedKey,
		version,
		[]string{wfcrypto.DeriveKEK("postgres-workspace-test-secret")},
	)
	if err != nil {
		t.Fatalf("resolve workspace DEK: %v", err)
	}
	if dek == "" || dek == wfcrypto.DeriveWorkspaceKey("postgres-workspace-test-secret", workspaceID) {
		t.Fatal("workspace DEK was not generated randomly")
	}
	token, err := store.CreateWorkspaceToken(ctx, workspaceID, "CLI", HashWorkspaceToken("secret-a"), "admin")
	if err != nil {
		t.Fatal(err)
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
	rotated, err := store.RotateWorkspaceToken(ctx, workspaceID, token.ID, HashWorkspaceToken("secret-b"), "operator")
	if err != nil || rotated.TokenHash != HashWorkspaceToken("secret-b") {
		t.Fatalf("rotated token = %#v, %v", rotated, err)
	}
	revoked, err := store.RevokeWorkspaceToken(ctx, workspaceID, token.ID, "operator")
	if err != nil || revoked.RevokedAt == nil || revoked.TokenHash != "" {
		t.Fatalf("revoked token = %#v, %v", revoked, err)
	}
	if _, err := store.GetWorkspaceTokenByTokenHash(ctx, workspaceID, HashWorkspaceToken("secret-b")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token lookup error = %v", err)
	}
	archived, err := store.ArchiveWorkspace(ctx, workspaceID, "operator")
	if err != nil || archived.Status != WorkspaceArchived {
		t.Fatalf("archived workspace = %#v, %v", archived, err)
	}
	if _, err := store.UpdateWorkspace(ctx, workspaceID, "Archived Workspace", "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("update archived error = %v, want invalid state", err)
	}
	if _, err := store.RotateWorkspaceToken(ctx, workspaceID, token.ID, HashWorkspaceToken("secret-c"), "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rotate archived error = %v, want invalid state", err)
	}
	audit, err := store.ListWorkspaceAudit(ctx, workspaceID)
	if err != nil || len(audit) != 6 {
		t.Fatalf("workspace audit = %#v, %v", audit, err)
	}
}

func TestPostgresWorkspaceTokenMigrationPreservesLegacyCredential(t *testing.T) {
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

	workspaceID := fmt.Sprintf("legacy-ws-%d", time.Now().UnixNano())
	legacyHash := HashWorkspaceToken("wfw_postgres-legacy")
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_token WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM workspace_registry WHERE id=$1`, workspaceID)
	}()
	if _, err := store.pool.Exec(ctx, `
INSERT INTO workspace_registry (id, display_name, status, token_hash, created_by, updated_by)
VALUES ($1, 'Legacy Workspace', 'active', $2, 'admin', 'admin')`, workspaceID, legacyHash); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	tokens, err := store.ListWorkspaceTokens(ctx, workspaceID)
	if err != nil || len(tokens) != 1 || tokens[0].Name != "Legacy access token" || tokens[0].TokenHash != legacyHash {
		t.Fatalf("migrated tokens = %#v, %v", tokens, err)
	}
	workspace, err := store.GetWorkspace(ctx, workspaceID)
	if err != nil || workspace.TokenHash != "" {
		t.Fatalf("migrated workspace = %#v, %v", workspace, err)
	}
	if byHash, err := store.GetWorkspaceTokenByTokenHash(ctx, workspaceID, legacyHash); err != nil || byHash.ID != tokens[0].ID {
		t.Fatalf("legacy token lookup = %#v, %v", byHash, err)
	}
}
