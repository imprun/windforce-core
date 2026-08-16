package state

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListResourceTypes(ctx context.Context, workspaceID string) ([]ResourceType, error) {
	rows, err := s.pool.Query(ctx, `
SELECT name, version, schema, description
FROM resource_type
WHERE workspace_id=$1
ORDER BY name, version
`, contract.NormalizeWorkspace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ResourceType{}
	for rows.Next() {
		var item ResourceType
		if err := rows.Scan(&item.Name, &item.Version, &item.Schema, &item.Description); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) SetResourceType(ctx context.Context, workspaceID string, resourceType ResourceType) error {
	resourceType.Name = strings.TrimSpace(resourceType.Name)
	resourceType.Version = normalizeResourceTypeVersion(resourceType.Version)
	resourceType.Schema = canonicalJSONValue(resourceType.Schema, "{}")
	if resourceType.Name == "" || !json.Valid(resourceType.Schema) ||
		resourceconfig.ValidateSchema(resourceType.Schema) != nil {
		return ErrInvalidState
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO resource_type (workspace_id, name, version, schema, description)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, name, version)
DO UPDATE SET schema=EXCLUDED.schema, description=EXCLUDED.description
`, contract.NormalizeWorkspace(workspaceID), resourceType.Name, resourceType.Version, resourceType.Schema, resourceType.Description)
	return err
}

func (s *PostgresStore) GetResourceType(ctx context.Context, workspaceID string, name string, version string) (ResourceType, bool, error) {
	var item ResourceType
	err := s.pool.QueryRow(ctx, `
SELECT name, version, schema, description
FROM resource_type
WHERE workspace_id=$1 AND name=$2 AND version=$3
`, contract.NormalizeWorkspace(workspaceID), strings.TrimSpace(name), normalizeResourceTypeVersion(version)).
		Scan(&item.Name, &item.Version, &item.Schema, &item.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return ResourceType{}, false, nil
	}
	return item, err == nil, err
}

func (s *PostgresStore) DeleteResourceType(ctx context.Context, workspaceID string, name string, version string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	name = strings.TrimSpace(name)
	version = normalizeResourceTypeVersion(version)
	var used bool
	if err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM runtime_resource
    WHERE workspace_id=$1 AND (
        resource_type=$2 OR ($3='1' AND resource_type=$4)
    )
)
`, workspaceID, name+"@"+version, version, name).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrConflict
	}
	result, err := s.pool.Exec(ctx, `
DELETE FROM resource_type
WHERE workspace_id=$1 AND name=$2 AND version=$3
`, workspaceID, name, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
