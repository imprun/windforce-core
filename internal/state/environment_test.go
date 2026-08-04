package state

import (
	"os"
	"strings"
	"testing"
)

func postgresTestDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("WINDFORCE_CORE_POSTGRES_TEST_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("WINDFORCE_LITE_POSTGRES_TEST_DSN"))
}

func TestPostgresTestDSNPrefersCoreAndFallsBackToLegacyAlias(t *testing.T) {
	t.Setenv("WINDFORCE_CORE_POSTGRES_TEST_DSN", "core-dsn")
	t.Setenv("WINDFORCE_LITE_POSTGRES_TEST_DSN", "legacy-dsn")
	if got := postgresTestDSN(); got != "core-dsn" {
		t.Fatalf("new test DSN = %q, want core-dsn", got)
	}

	t.Setenv("WINDFORCE_CORE_POSTGRES_TEST_DSN", "")
	if got := postgresTestDSN(); got != "legacy-dsn" {
		t.Fatalf("legacy test DSN fallback = %q, want legacy-dsn", got)
	}
}
