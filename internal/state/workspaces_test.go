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

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
	controlevent "github.com/imprun/windforce-core/internal/event"
	"github.com/imprun/windforce-core/internal/webhook"
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

func TestLocalDeleteWorkspacePurgesScopedData(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	workspaceID := "team-delete"
	if _, err := store.CreateWorkspace(ctx, workspaceID, "Team Delete", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		snapshot.Runs["run-delete"] = Run{ID: "run-delete"}
		snapshot.Jobs["job-delete"] = Job{ID: "job-delete", RunID: "run-delete", Payload: JobPayload{Workspace: workspaceID}}
		snapshot.HumanTasks["task-delete"] = HumanTask{ID: "task-delete", RunID: "run-delete"}
		snapshot.Events = append(snapshot.Events, RunEvent{ID: 1, RunID: "run-delete"})
		snapshot.JobLogs["job-delete"] = JobLog{JobID: "job-delete", WorkspaceID: workspaceID}
		snapshot.JobState[workspaceID] = map[string]json.RawMessage{"state": json.RawMessage(`{"ok":true}`)}
		snapshot.Variables[workspaceID] = map[string]Variable{"secret": {Path: "secret"}}
		snapshot.Resources[workspaceID] = map[string]Resource{"db": {Path: "db"}}
		snapshot.ResourceTypes[workspaceID] = map[string]ResourceType{"database@1": {Name: "database", Version: "1"}}
		snapshot.Clients[workspaceID] = map[string]Client{"client": {ID: "client", WorkspaceID: workspaceID}}
		snapshot.LegacyClients = map[string]map[string]Client{}
		snapshot.LegacyClientAudits = map[string][]ClientAudit{}
		snapshot.LegacyClients[workspaceID] = map[string]Client{"legacy-client": {ID: "legacy-client", WorkspaceID: workspaceID}}
		snapshot.LegacyClientAudits[workspaceID] = []ClientAudit{{WorkspaceID: workspaceID}}
		snapshot.InputConfigs[workspaceID] = map[string]InputConfig{"config": {WorkspaceID: workspaceID}}
		snapshot.SecretAccessAudits[workspaceID] = []SecretAccessAudit{{WorkspaceID: workspaceID}}
		snapshot.WebhookSubscriptions["subscription"] = WebhookSubscriptionRecord{ID: "subscription", WorkspaceID: workspaceID}
		snapshot.ControlPlaneEvents["evt_delete"] = controlevent.Envelope{ID: "evt_delete", Source: "/workspaces/" + workspaceID + "/control-plane"}
		snapshot.WebhookDeliveries["delivery"] = webhook.Delivery{ID: "delivery", WorkspaceID: workspaceID}
		snapshot.Triggers["trigger"] = TriggerRecord{TriggerDefinition: TriggerDefinition{ID: "trigger", WorkspaceID: workspaceID}}
		snapshot.TriggerDeliveries["trigger-delivery"] = TriggerDelivery{ID: "trigger-delivery", WorkspaceID: workspaceID}
		snapshot.HTTPRouteBindings["route"] = HTTPRouteBinding{ID: "route", WorkspaceID: workspaceID}
		deployment := contract.Deployment{Workspace: workspaceID, App: "demo"}
		deploymentKey := catalog.DeploymentKey(workspaceID, deployment.App)
		snapshot.ReleaseCatalog.Deployments[deploymentKey] = deployment
		snapshot.ReleaseCatalog.ActiveHistoryIDs[deploymentKey] = "history-delete"
		snapshot.ReleaseCatalog.Candidates["candidate"] = catalog.ReleaseCandidate{Deployment: deployment, SyncedAt: now}
		snapshot.ReleaseCatalog.History = append(snapshot.ReleaseCatalog.History, catalog.DeploymentHistory{ID: "history-delete", Workspace: workspaceID})
		snapshot.ReleaseCatalog.Audit = append(snapshot.ReleaseCatalog.Audit, catalog.AuditRecord{ID: "audit-delete", Workspace: workspaceID})
		snapshot.ReleaseCatalog.SourceMarkers["marker"] = catalog.SourceReleaseMarker{Workspace: workspaceID}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteWorkspace(ctx, workspaceID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWorkspace(ctx, workspaceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted workspace lookup error = %v", err)
	}
	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := snapshot.Workspaces[workspaceID]; exists || len(snapshot.Runs) != 0 || len(snapshot.Jobs) != 0 || len(snapshot.HumanTasks) != 0 || len(snapshot.Events) != 0 || len(snapshot.JobLogs) != 0 {
		t.Fatalf("execution data remained after deletion: %#v", snapshot)
	}
	if _, exists := snapshot.JobState[workspaceID]; exists || len(snapshot.LegacyClients) != 0 || len(snapshot.LegacyClientAudits) != 0 || len(snapshot.Triggers) != 0 || len(snapshot.WebhookSubscriptions) != 0 || len(snapshot.ControlPlaneEvents) != 0 || len(snapshot.ReleaseCatalog.Deployments) != 0 {
		t.Fatalf("workspace-scoped data remained after deletion: %#v", snapshot)
	}
	if _, err := store.GetWorkspace(ctx, contract.DefaultWorkspace); err != nil {
		t.Fatalf("default workspace was removed: %v", err)
	}
	if err := store.DeleteWorkspace(ctx, contract.DefaultWorkspace, "admin"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("delete default error = %v", err)
	}
	if err := store.DeleteWorkspace(ctx, "missing", "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing error = %v", err)
	}
}

func TestPostgresWorkspaceLifecycle(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
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
	if err := store.SetVariable(ctx, workspaceID, "", "delete-me", "value", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkspace(ctx, workspaceID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetWorkspace(ctx, workspaceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted workspace lookup error = %v", err)
	}
	for _, table := range []string{"workspace_registry", "workspace_key", "workspace_token", "workspace_audit", "variable"} {
		var count int
		if err := store.pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s=$1`, table, map[bool]string{true: "id", false: "workspace_id"}[table == "workspace_registry"]), workspaceID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d workspace rows", table, count)
		}
	}
}

func TestPostgresWorkspaceTokenMigrationPreservesLegacyCredential(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
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
