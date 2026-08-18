package state

import (
	"context"
	"sort"
)

const StateSchemaContractVersion = "core-state/v1"

// SchemaCompatibility is a read-only description of whether a state backend
// contains the minimum durable shape required by this Core build.
type SchemaCompatibility struct {
	Backend    string
	Contract   string
	Compatible bool
	Missing    []string
}

// SchemaCompatibilityStore is an optional diagnostic capability. It is kept
// outside Store so third-party Store implementations remain source-compatible.
type SchemaCompatibilityStore interface {
	CheckSchemaCompatibility(context.Context) (SchemaCompatibility, error)
}

func (s *LocalStore) CheckSchemaCompatibility(ctx context.Context) (SchemaCompatibility, error) {
	result := SchemaCompatibility{
		Backend:    "local",
		Contract:   StateSchemaContractVersion,
		Compatible: true,
		Missing:    []string{},
	}
	if _, err := s.Load(ctx); err != nil {
		result.Compatible = false
		result.Missing = []string{"readable_snapshot"}
	}
	return result, nil
}

func (s *PostgresStore) CheckSchemaCompatibility(ctx context.Context) (SchemaCompatibility, error) {
	expected := map[string][]string{
		"app_runtime_lifecycle":    {"workspace_id", "app_key", "state", "revision"},
		"client_registry":          {"workspace_id", "id", "invocation_policy_revision"},
		"control_active_release":   {"workspace_id", "app_key", "history_id"},
		"execution_limit_policy":   {"workspace_id", "app_key", "policy_id", "revision"},
		"jobs":                     {"id", "state", "attempt", "lease_identity"},
		"queue_snapshot_state":     {"store_epoch", "revision"},
		"runs":                     {"id", "state", "deployment", "execution_limits"},
		"runtime_config_operation": {"workspace_id", "job_id", "attempt", "operation_id", "request_fingerprint"},
		"runtime_resource":         {"workspace_id", "owner_scope", "app_key", "path", "revision"},
		"runtime_variable":         {"workspace_id", "owner_scope", "app_key", "path", "revision"},
		"worker_registry":          {"id", "worker_group", "execution_profiles", "engine_version", "build_revision"},
	}

	rows, err := s.pool.Query(ctx, `
SELECT table_name, column_name
FROM information_schema.columns
WHERE table_schema = current_schema()
`)
	if err != nil {
		return SchemaCompatibility{}, err
	}
	defer rows.Close()
	observed := map[string]map[string]struct{}{}
	for rows.Next() {
		var table string
		var column string
		if err := rows.Scan(&table, &column); err != nil {
			return SchemaCompatibility{}, err
		}
		if observed[table] == nil {
			observed[table] = map[string]struct{}{}
		}
		observed[table][column] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return SchemaCompatibility{}, err
	}

	missing := []string{}
	for table, columns := range expected {
		for _, column := range columns {
			if _, ok := observed[table][column]; !ok {
				missing = append(missing, table+"."+column)
			}
		}
	}
	sort.Strings(missing)
	return SchemaCompatibility{
		Backend:    "postgres",
		Contract:   StateSchemaContractVersion,
		Compatible: len(missing) == 0,
		Missing:    missing,
	}, nil
}
