package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLocalStoreClients(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	exerciseClientStore(t, store, "local-clients")
}

func TestLocalStoreMigratesLegacyClientSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{"apiClients":{"default":{"client_legacy":{"id":"client_legacy","workspace_id":"default","name":"Legacy client","client_key":"external-legacy","created_by":"alice","updated_by":"alice","created_at":"2026-07-14T00:00:00Z","updated_at":"2026-07-14T00:00:00Z"}}},"apiClientAudits":{}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewLocalStore(path)
	clients, err := store.ListClients(context.Background(), "default")
	if err != nil || len(clients) != 1 || clients[0].TokenHash != "" || clients[0].EffectiveInvocationPolicy().Mode != TargetPolicyModeAll {
		t.Fatalf("clients = %#v, %v", clients, err)
	}
	audit, err := store.ListClientAudit(context.Background(), "default", "client_legacy")
	if err != nil || len(audit) != 1 || audit[0].Kind != "token_revoked_migration" {
		t.Fatalf("migration audit = %#v, %v", audit, err)
	}
	if _, err := store.UpdateClient(context.Background(), "default", "client_legacy", "Migrated client", "bob"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["clients"] == nil || snapshot["apiClients"] != nil || strings.Contains(string(data), `"client_key"`) || strings.Contains(string(data), `"external_key"`) || strings.Contains(string(data), "external-legacy") {
		t.Fatalf("legacy client fields remain after write: %s", data)
	}
}

func TestPostgresStoreClients(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	workspaceID := "test-clients-" + time.Now().UTC().Format("20060102150405.000000000")
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM client_registry_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM client_registry WHERE workspace_id=$1`, workspaceID)
	}()
	exerciseClientStore(t, store, workspaceID)
}

func TestPostgresMigrationPreservesLegacyClients(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	admin, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "client_migration_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405_000000000"), ".", "")
	if _, err := admin.pool.Exec(context.Background(), `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) }()
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	store, err := OpenPostgresStore(context.Background(), dsn+separator+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.pool.Exec(context.Background(), `
CREATE TABLE api_client (
    workspace_id TEXT NOT NULL, id TEXT NOT NULL, name TEXT NOT NULL, client_key TEXT NOT NULL,
    created_by TEXT NOT NULL, updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id), UNIQUE (workspace_id, client_key)
);
CREATE TABLE api_client_audit (
    id BIGSERIAL PRIMARY KEY, workspace_id TEXT NOT NULL, api_client_id TEXT NOT NULL,
    kind TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO api_client (workspace_id, id, name, client_key, created_by, updated_by)
VALUES ('default', 'client_legacy', 'Legacy client', 'external-legacy', 'alice', 'alice');
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	client, err := store.GetClient(context.Background(), "default", "client_legacy")
	if err != nil || client.TokenHash != "" || client.EffectiveInvocationPolicy().Mode != TargetPolicyModeAll {
		t.Fatalf("client = %#v, %v", client, err)
	}
	audit, err := store.ListClientAudit(context.Background(), "default", "client_legacy")
	if err != nil || len(audit) != 1 || audit[0].Kind != "token_revoked_migration" {
		t.Fatalf("migration audit = %#v, %v", audit, err)
	}
	issued := "wfk_post_migration"
	if _, err := store.RotateClientToken(context.Background(), "default", "client_legacy", HashClientToken(issued), "admin"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	client, err = store.GetClient(context.Background(), "default", "client_legacy")
	if err != nil || !ClientTokenMatches(client, issued) {
		t.Fatalf("issued token did not survive repeated migration: %#v, %v", client, err)
	}
}

func exerciseClientStore(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	created, err := store.CreateClient(ctx, workspaceID, "Client A", HashClientToken("client-key-a"), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.WorkspaceID != workspaceID || created.CreatedBy != "alice" {
		t.Fatalf("created = %#v", created)
	}
	if created.InvocationPolicy.Mode != TargetPolicyModeAll || len(created.InvocationPolicy.AllowedTargets) != 0 || created.InvocationPolicyRevision != 0 {
		t.Fatalf("created invocation policy = %#v revision=%d", created.InvocationPolicy, created.InvocationPolicyRevision)
	}
	restrictedPolicy := TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: []string{"billing/export", "orders", "orders"}}
	restricted, err := store.CreateClientWithInvocationPolicy(ctx, CreateClientRequest{
		WorkspaceID: workspaceID, Name: "Restricted Client", TokenHash: HashClientToken("client-key-restricted"),
		InvocationPolicy: &restrictedPolicy, Actor: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if restricted.InvocationPolicyRevision != 0 || !reflect.DeepEqual(restricted.InvocationPolicy.AllowedTargets, []string{"billing/export", "orders"}) {
		t.Fatalf("atomically created policy = %#v revision=%d", restricted.InvocationPolicy, restricted.InvocationPolicyRevision)
	}
	restrictedAudit, err := store.ListClientAudit(ctx, workspaceID, restricted.ID)
	if err != nil || len(restrictedAudit) != 1 || restrictedAudit[0].Kind != "created" ||
		!strings.Contains(restrictedAudit[0].Detail, `"mode":"restricted"`) || !strings.Contains(restrictedAudit[0].Detail, `"revision":0`) {
		t.Fatalf("atomic creation audit = %#v, %v", restrictedAudit, err)
	}
	if _, err := store.CreateClient(ctx, workspaceID, "Duplicate", HashClientToken("client-key-a"), "alice"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate error = %v, want conflict", err)
	}
	got, err := store.GetClient(ctx, workspaceID, created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get = %#v, %v", got, err)
	}
	byToken, err := store.GetClientByTokenHash(ctx, workspaceID, HashClientToken("client-key-a"))
	if err != nil || byToken.ID != created.ID {
		t.Fatalf("get by token = %#v, %v", byToken, err)
	}
	policyRequest := UpdateClientInvocationPolicyRequest{
		WorkspaceID: workspaceID, ClientID: created.ID,
		Policy:      TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: []string{"reports/export", "orders", "orders"}},
		OperationID: "op-policy-1", ExpectedRevision: 0, RequestFingerprint: "policy-fingerprint-1", Actor: "policy-admin",
	}
	policyUpdated, replayed, err := store.UpdateClientInvocationPolicy(ctx, policyRequest)
	if err != nil || replayed || policyUpdated.InvocationPolicyRevision != 1 ||
		!reflect.DeepEqual(policyUpdated.InvocationPolicy.AllowedTargets, []string{"orders", "reports/export"}) ||
		!ClientTokenMatches(policyUpdated, "client-key-a") {
		t.Fatalf("policy update = %#v replayed=%v err=%v", policyUpdated, replayed, err)
	}
	replayedClient, replayed, err := store.UpdateClientInvocationPolicy(ctx, policyRequest)
	if err != nil || !replayed || replayedClient.InvocationPolicyRevision != 1 {
		t.Fatalf("policy replay = %#v replayed=%v err=%v", replayedClient, replayed, err)
	}
	conflictingReplay := policyRequest
	conflictingReplay.RequestFingerprint = "different"
	if _, _, err := store.UpdateClientInvocationPolicy(ctx, conflictingReplay); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	stale := policyRequest
	stale.OperationID = "op-policy-stale"
	stale.RequestFingerprint = "stale"
	if _, _, err := store.UpdateClientInvocationPolicy(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale policy error = %v", err)
	}
	updated, err := store.UpdateClient(ctx, workspaceID, created.ID, "Client B", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.UpdatedBy != "bob" || !ClientTokenMatches(updated, "client-key-a") || updated.InvocationPolicyRevision != 1 {
		t.Fatalf("updated = %#v", updated)
	}
	rotated, err := store.RotateClientToken(ctx, workspaceID, created.ID, HashClientToken("client-key-b"), "bob")
	if err != nil || !ClientTokenMatches(rotated, "client-key-b") || ClientTokenMatches(rotated, "client-key-a") ||
		rotated.InvocationPolicyRevision != 1 || !reflect.DeepEqual(rotated.InvocationPolicy.AllowedTargets, []string{"orders", "reports/export"}) {
		t.Fatalf("rotated = %#v, %v", rotated, err)
	}
	clients, err := store.ListClients(ctx, workspaceID)
	if err != nil || len(clients) != 2 {
		t.Fatalf("clients = %#v, %v", clients, err)
	}
	audit, err := store.ListClientAudit(ctx, workspaceID, created.ID)
	if err != nil || len(audit) != 4 || audit[0].Kind != "token_rotated" {
		t.Fatalf("audit = %#v, %v", audit, err)
	}
	policyAuditCount := 0
	for _, record := range audit {
		if strings.Contains(record.Detail, "client-key-") {
			t.Fatalf("audit exposes client key: %#v", record)
		}
		if record.Kind == "invocation_policy_updated" {
			policyAuditCount++
			if !strings.Contains(record.Detail, `"revision":1`) || !strings.Contains(record.Detail, `"orders"`) {
				t.Fatalf("policy audit = %#v", record)
			}
		}
	}
	if policyAuditCount != 1 {
		t.Fatalf("policy audit count = %d", policyAuditCount)
	}
	if err := store.DeleteClient(ctx, workspaceID, created.ID, "carol"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete with active token error = %v, want conflict", err)
	}
	revoked, err := store.RevokeClientToken(ctx, workspaceID, created.ID, "carol")
	if err != nil || revoked.TokenHash != "" || revoked.InvocationPolicyRevision != 1 ||
		!reflect.DeepEqual(revoked.InvocationPolicy.AllowedTargets, []string{"orders", "reports/export"}) {
		t.Fatalf("revoked = %#v, %v", revoked, err)
	}
	if err := store.DeleteClient(ctx, workspaceID, created.ID, "carol"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetClient(ctx, workspaceID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted error = %v, want not found", err)
	}
}
