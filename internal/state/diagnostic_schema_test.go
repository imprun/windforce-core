package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSchemaCompatibilityIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	store := NewLocalStore(path)
	compatibility, err := store.CheckSchemaCompatibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !compatibility.Compatible || compatibility.Backend != "local" || compatibility.Contract != StateSchemaContractVersion {
		t.Fatalf("compatibility = %#v", compatibility)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read-only check created state file, stat err=%v", err)
	}
}

func TestLocalSchemaCompatibilityRejectsMalformedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	compatibility, err := NewLocalStore(path).CheckSchemaCompatibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if compatibility.Compatible || len(compatibility.Missing) != 1 || compatibility.Missing[0] != "readable_snapshot" {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestPostgresSchemaCompatibilityDetectsMissingColumn(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("PostgreSQL test DSN is not configured")
	}
	ctx := context.Background()
	store := openIsolatedPostgresCatalogStore(t, dsn)
	compatibility, err := store.CheckSchemaCompatibility(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !compatibility.Compatible {
		t.Fatalf("migrated compatibility = %#v", compatibility)
	}
	if _, err := store.pool.Exec(ctx, `ALTER TABLE jobs DROP COLUMN lease_identity`); err != nil {
		t.Fatal(err)
	}
	compatibility, err = store.CheckSchemaCompatibility(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if compatibility.Compatible || !containsString(compatibility.Missing, "jobs.lease_identity") {
		t.Fatalf("compatibility after drift = %#v", compatibility)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
