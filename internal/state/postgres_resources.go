package state

import (
	"context"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *PostgresStore) ListResources(ctx context.Context, workspaceID string) ([]Resource, error) {
	rows, err := s.pool.Query(ctx, `
SELECT path, value, resource_type, description
FROM resource
WHERE workspace_id=$1
ORDER BY path
`, contract.NormalizeWorkspace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Resource{}
	for rows.Next() {
		var item Resource
		if err := rows.Scan(&item.Path, &item.Value, &item.ResourceType, &item.Description); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) DeleteResource(ctx context.Context, workspaceID string, path string) error {
	result, err := s.pool.Exec(ctx, `
DELETE FROM resource
WHERE workspace_id=$1 AND path=$2
`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
