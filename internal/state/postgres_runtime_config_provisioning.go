package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
)

func (s *PostgresStore) ApplyRuntimeConfigProvisioningBatch(ctx context.Context, batch RuntimeConfigProvisioningBatch) error {
	batch.WorkspaceID, batch.Actor = contract.NormalizeWorkspace(batch.WorkspaceID), strings.TrimSpace(batch.Actor)
	if batch.Actor == "" {
		return fmt.Errorf("%w: provisioning actor is required", ErrInvalidState)
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		seen := map[string]struct{}{}
		for _, item := range batch.Variables {
			appKey := strings.TrimSpace(item.AppKey)
			path, err := contract.NormalizeRuntimeConfigPath(item.Path)
			valueLimit := RuntimeConfigMaxValueBytes
			if item.IsSecret {
				valueLimit = RuntimeConfigMaxStoredSecretBytes
			}
			if err != nil || len(item.Value) > valueLimit {
				return fmt.Errorf("%w: invalid provisioned Variable %q", ErrInvalidState, item.Path)
			}
			key := "variable\x00" + appKey + "\x00" + path
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: duplicate provisioned Variable", ErrConflict)
			}
			seen[key] = struct{}{}
			var current Variable
			err = tx.QueryRow(ctx, `SELECT value,is_secret,description,revision FROM runtime_variable
WHERE workspace_id=$1 AND owner_scope=$2 AND app_key=$3 AND path=$4 FOR UPDATE`,
				batch.WorkspaceID, runtimeOwnerScope(appKey), appKey, path).Scan(&current.Value, &current.IsSecret, &current.Description, &current.Revision)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision,
				current.Value == item.Value && current.IsSecret == item.IsSecret && current.Description == item.Description)
			if err != nil || unchanged {
				if err != nil {
					return err
				}
				continue
			}
			if batch.DryRun {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO runtime_variable
(workspace_id,owner_scope,app_key,path,value,is_secret,description,revision,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(workspace_id,owner_scope,app_key,path)
DO UPDATE SET value=EXCLUDED.value,is_secret=EXCLUDED.is_secret,description=EXCLUDED.description,revision=EXCLUDED.revision,updated_at=now()`,
				batch.WorkspaceID, runtimeOwnerScope(appKey), appKey, path, item.Value, item.IsSecret, item.Description, revision); err != nil {
				return err
			}
			if err = insertProvisioningRuntimeAudit(ctx, tx, batch, appKey, path, "variable", variableStorage(item.IsSecret), revision); err != nil {
				return err
			}
		}
		for _, item := range batch.Resources {
			appKey := strings.TrimSpace(item.AppKey)
			path, err := contract.NormalizeRuntimeConfigPath(item.Path)
			if err != nil || len(item.Value) > RuntimeConfigMaxValueBytes || !json.Valid(item.Value) || appKey != "" && item.ResourceType == "" {
				return fmt.Errorf("%w: invalid provisioned Resource %q", ErrInvalidState, item.Path)
			}
			key := "resource\x00" + appKey + "\x00" + path
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: duplicate provisioned Resource", ErrConflict)
			}
			seen[key] = struct{}{}
			if item.ResourceType != "" {
				name, version, parseErr := resourceconfig.ParseTypeReference(item.ResourceType)
				if parseErr != nil {
					return fmt.Errorf("%w: %v", ErrInvalidState, parseErr)
				}
				var schema json.RawMessage
				if err = tx.QueryRow(ctx, `SELECT schema FROM resource_type WHERE workspace_id=$1 AND name=$2 AND version=$3`, batch.WorkspaceID, name, version).Scan(&schema); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return ErrNotFound
					}
					return err
				}
				if err = resourceconfig.ValidateValue(schema, item.Value); err != nil {
					return fmt.Errorf("%w: %v", ErrInvalidState, err)
				}
			}
			var current Resource
			err = tx.QueryRow(ctx, `SELECT value,resource_type,description,revision FROM runtime_resource
WHERE workspace_id=$1 AND owner_scope=$2 AND app_key=$3 AND path=$4 FOR UPDATE`,
				batch.WorkspaceID, runtimeOwnerScope(appKey), appKey, path).Scan(&current.Value, &current.ResourceType, &current.Description, &current.Revision)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision,
				jsonBytesEqual(current.Value, item.Value) && current.ResourceType == item.ResourceType && current.Description == item.Description)
			if err != nil || unchanged {
				if err != nil {
					return err
				}
				continue
			}
			if batch.DryRun {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO runtime_resource
(workspace_id,owner_scope,app_key,path,value,resource_type,description,revision,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(workspace_id,owner_scope,app_key,path)
DO UPDATE SET value=EXCLUDED.value,resource_type=EXCLUDED.resource_type,description=EXCLUDED.description,revision=EXCLUDED.revision,updated_at=now()`,
				batch.WorkspaceID, runtimeOwnerScope(appKey), appKey, path, item.Value, item.ResourceType, item.Description, revision); err != nil {
				return err
			}
			if err = insertProvisioningRuntimeAudit(ctx, tx, batch, appKey, path, "resource", "", revision); err != nil {
				return err
			}
		}
		for _, item := range batch.Lifecycles {
			appKey := strings.TrimSpace(item.AppKey)
			if appKey == "" || item.State != AppRuntimeActive && item.State != AppRuntimeTombstoned && item.State != AppRuntimeRevoked {
				return fmt.Errorf("%w: invalid App runtime lifecycle", ErrInvalidState)
			}
			var current AppRuntimeLifecycle
			err := tx.QueryRow(ctx, `SELECT state,reason,revision FROM app_runtime_lifecycle WHERE workspace_id=$1 AND app_key=$2 FOR UPDATE`,
				batch.WorkspaceID, appKey).Scan(&current.State, &current.Reason, &current.Revision)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision, current.State == item.State && current.Reason == item.Reason)
			if err != nil || unchanged {
				if err != nil {
					return err
				}
				continue
			}
			if batch.DryRun {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO app_runtime_lifecycle(workspace_id,app_key,state,reason,actor,revision,updated_at)
VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(workspace_id,app_key) DO UPDATE SET state=EXCLUDED.state,reason=EXCLUDED.reason,actor=EXCLUDED.actor,revision=EXCLUDED.revision,updated_at=now()`,
				batch.WorkspaceID, appKey, item.State, item.Reason, batch.Actor, revision); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO app_runtime_lifecycle_audit(id,workspace_id,app_key,state,reason,actor,revision,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,now())`, NewID("audit"), batch.WorkspaceID, appKey, item.State, item.Reason, batch.Actor, revision); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertProvisioningRuntimeAudit(ctx context.Context, tx pgx.Tx, batch RuntimeConfigProvisioningBatch, appKey, path, kind, storage string, revision int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO runtime_config_audit
(id,workspace_id,owner_scope,app_key,path,object_kind,storage,revision,operation_id,actor,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'provisioning',$9,now())`, NewID("audit"), batch.WorkspaceID,
		runtimeOwnerScope(appKey), appKey, path, kind, storage, revision, batch.Actor)
	return err
}

func jsonBytesEqual(left, right json.RawMessage) bool {
	var l, r any
	if json.Unmarshal(left, &l) != nil || json.Unmarshal(right, &r) != nil {
		return bytes.Equal(left, right)
	}
	lb, _ := json.Marshal(l)
	rb, _ := json.Marshal(r)
	return bytes.Equal(lb, rb)
}
