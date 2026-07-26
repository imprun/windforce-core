package state

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestLocalStoreServicePrincipals(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	exerciseServicePrincipalStore(t, store, "service-local")
}

func TestPostgresStoreServicePrincipalsAndConcurrentRunIdempotency(t *testing.T) {
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
	workspaceID := "test-service-principals-" + time.Now().UTC().Format("20060102150405.000000000")
	defer func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM jobs WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM runs WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM service_principal_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM service_principal WHERE workspace_id=$1`, workspaceID)
	}()

	principal := exerciseServicePrincipalStore(t, store, workspaceID)
	run := NewRun("http", "", "echo", "run", contract.Deployment{
		Workspace:    workspaceID,
		App:          "echo",
		Commit:       "commit-a",
		BundleDigest: "sha256:bundle",
	}, []byte(`{"message":"hello"}`))
	run.PrincipalKind = "service"
	run.PrincipalID = principal.ID
	run.IdempotencyHash = "idempotency-hash"
	run.RequestFingerprint = "request-fingerprint"
	run.CreatedBy = "service:" + principal.ID
	run.PermissionedAs = "service:" + principal.ID
	job := NewActionJob(run, run.Input)

	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			defer wait.Done()
			errs <- store.CreateRunAndEnqueue(ctx, run, job)
		}()
	}
	wait.Wait()
	close(errs)
	var created, conflicts int
	for err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("CreateRunAndEnqueue returned %v", err)
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("created=%d conflicts=%d, want 1/1", created, conflicts)
	}
	persisted, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PrincipalKind != run.PrincipalKind || persisted.PrincipalID != run.PrincipalID ||
		persisted.IdempotencyHash != run.IdempotencyHash || persisted.RequestFingerprint != run.RequestFingerprint ||
		persisted.CreatedBy != run.CreatedBy || persisted.PermissionedAs != run.PermissionedAs {
		t.Fatalf("persisted Run admission fields = %#v", persisted)
	}
}

func exerciseServicePrincipalStore(t *testing.T, store Store, workspaceID string) ServicePrincipal {
	t.Helper()
	ctx := context.Background()
	tokenHash := HashClientToken("wfs_first")
	created, err := store.CreateServicePrincipal(ctx, ServicePrincipal{
		WorkspaceID:    workspaceID,
		Name:           "Order intake",
		Scopes:         []string{"runs:create", "runs:read:own"},
		AllowedTargets: []string{"orders/ingest"},
	}, tokenHash, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.TokenHash != tokenHash || created.CreatedBy != "alice" {
		t.Fatalf("created = %#v", created)
	}
	byToken, err := store.GetServicePrincipalByTokenHash(ctx, workspaceID, tokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if byToken.ID != created.ID {
		t.Fatalf("by token = %#v, created = %#v", byToken, created)
	}
	created.Name = "Order intake v2"
	created.Scopes = []string{"apps:read"}
	created.AllowedTargets = []string{"orders"}
	updated, err := store.UpdateServicePrincipal(ctx, created, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != created.Name || updated.UpdatedBy != "bob" ||
		len(updated.Scopes) != 1 || updated.Scopes[0] != "apps:read" {
		t.Fatalf("updated = %#v", updated)
	}
	rotatedHash := HashClientToken("wfs_second")
	rotated, err := store.RotateServicePrincipalToken(ctx, workspaceID, created.ID, rotatedHash, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.TokenHash != rotatedHash {
		t.Fatalf("rotated = %#v", rotated)
	}
	if _, err := store.GetServicePrincipalByTokenHash(ctx, workspaceID, tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token lookup error = %v, want ErrNotFound", err)
	}
	audit, err := store.ListServicePrincipalAudit(ctx, workspaceID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 3 || audit[0].Kind != "token_rotated" || audit[1].Kind != "updated" || audit[2].Kind != "created" {
		t.Fatalf("audit = %#v", audit)
	}
	revoked, err := store.RevokeServicePrincipalToken(ctx, workspaceID, created.ID, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.TokenHash != "" {
		t.Fatalf("revoked = %#v", revoked)
	}
	if err := store.DeleteServicePrincipal(ctx, workspaceID, created.ID, "carol"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetServicePrincipal(ctx, workspaceID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted principal lookup error = %v, want ErrNotFound", err)
	}
	return created
}
