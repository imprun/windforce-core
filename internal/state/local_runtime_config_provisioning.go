package state

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
)

func (s *LocalStore) ApplyRuntimeConfigProvisioningBatch(ctx context.Context, batch RuntimeConfigProvisioningBatch) error {
	batch.WorkspaceID, batch.Actor = contract.NormalizeWorkspace(batch.WorkspaceID), strings.TrimSpace(batch.Actor)
	if batch.Actor == "" {
		return fmt.Errorf("%w: provisioning actor is required", ErrInvalidState)
	}
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
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
			key := runtimeConfigObjectKey(runtimeOwnerScope(appKey), appKey, path)
			if _, duplicate := seen["variable\x00"+key]; duplicate {
				return fmt.Errorf("%w: duplicate provisioned Variable", ErrConflict)
			}
			seen["variable\x00"+key] = struct{}{}
			current := snapshot.Variables[batch.WorkspaceID][key]
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision,
				current.Value == item.Value && current.IsSecret == item.IsSecret && current.Description == item.Description)
			if err != nil {
				return err
			}
			if unchanged {
				continue
			}
			if batch.DryRun {
				continue
			}
			if snapshot.Variables[batch.WorkspaceID] == nil {
				snapshot.Variables[batch.WorkspaceID] = map[string]Variable{}
			}
			snapshot.Variables[batch.WorkspaceID][key] = Variable{OwnerScope: runtimeOwnerScope(appKey), AppKey: appKey,
				Path: path, Value: item.Value, IsSecret: item.IsSecret, Description: item.Description, Revision: revision, UpdatedAt: now.UTC()}
			appendProvisioningRuntimeAudit(snapshot, now, batch, appKey, path, "variable", variableStorage(item.IsSecret), revision)
		}
		for _, item := range batch.Resources {
			appKey := strings.TrimSpace(item.AppKey)
			path, err := contract.NormalizeRuntimeConfigPath(item.Path)
			if err != nil || len(item.Value) > RuntimeConfigMaxValueBytes || !json.Valid(item.Value) {
				return fmt.Errorf("%w: invalid provisioned Resource %q", ErrInvalidState, item.Path)
			}
			if appKey != "" && strings.TrimSpace(item.ResourceType) == "" {
				return fmt.Errorf("%w: App-owned Resource requires a type", ErrInvalidState)
			}
			if item.ResourceType != "" {
				name, version, parseErr := resourceconfig.ParseTypeReference(item.ResourceType)
				if parseErr != nil {
					return fmt.Errorf("%w: %v", ErrInvalidState, parseErr)
				}
				registered := snapshot.ResourceTypes[batch.WorkspaceID][resourceTypeKey(name, version)]
				if registered.Name == "" {
					return ErrNotFound
				}
				if validateErr := resourceconfig.ValidateValue(registered.Schema, item.Value); validateErr != nil {
					return fmt.Errorf("%w: %v", ErrInvalidState, validateErr)
				}
			}
			key := runtimeConfigObjectKey(runtimeOwnerScope(appKey), appKey, path)
			if _, duplicate := seen["resource\x00"+key]; duplicate {
				return fmt.Errorf("%w: duplicate provisioned Resource", ErrConflict)
			}
			seen["resource\x00"+key] = struct{}{}
			current := snapshot.Resources[batch.WorkspaceID][key]
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision,
				jsonBytesEqual(current.Value, item.Value) && current.ResourceType == item.ResourceType && current.Description == item.Description)
			if err != nil {
				return err
			}
			if unchanged {
				continue
			}
			if batch.DryRun {
				continue
			}
			if snapshot.Resources[batch.WorkspaceID] == nil {
				snapshot.Resources[batch.WorkspaceID] = map[string]Resource{}
			}
			snapshot.Resources[batch.WorkspaceID][key] = Resource{OwnerScope: runtimeOwnerScope(appKey), AppKey: appKey,
				Path: path, Value: cloneRaw(item.Value), ResourceType: item.ResourceType, Description: item.Description, Revision: revision, UpdatedAt: now.UTC()}
			appendProvisioningRuntimeAudit(snapshot, now, batch, appKey, path, "resource", "", revision)
		}
		for _, item := range batch.Lifecycles {
			appKey := strings.TrimSpace(item.AppKey)
			if appKey == "" || item.State != AppRuntimeActive && item.State != AppRuntimeTombstoned && item.State != AppRuntimeRevoked {
				return fmt.Errorf("%w: invalid App runtime lifecycle", ErrInvalidState)
			}
			key := appRuntimeLifecycleKey(batch.WorkspaceID, appKey)
			current, found := snapshot.AppRuntimeLifecycles[key]
			if !found {
				current = activeAppRuntimeLifecycle(batch.WorkspaceID, appKey)
			}
			revision, unchanged, err := provisionedRevision(current.Revision, item.Revision, current.State == item.State && current.Reason == item.Reason)
			if err != nil {
				return err
			}
			if unchanged {
				continue
			}
			if batch.DryRun {
				continue
			}
			lifecycle := AppRuntimeLifecycle{WorkspaceID: batch.WorkspaceID, AppKey: appKey, State: item.State,
				Reason: item.Reason, Actor: batch.Actor, Revision: revision, UpdatedAt: now.UTC()}
			snapshot.AppRuntimeLifecycles[key] = lifecycle
			snapshot.AppRuntimeLifecycleAudits[key] = append(snapshot.AppRuntimeLifecycleAudits[key], AppRuntimeLifecycleAudit{
				WorkspaceID: batch.WorkspaceID, AppKey: appKey, State: item.State, Reason: item.Reason,
				Actor: batch.Actor, Revision: revision, CreatedAt: now.UTC(),
			})
		}
		return nil
	})
}

func runtimeOwnerScope(appKey string) contract.RuntimeConfigScope {
	if appKey == "" {
		return contract.RuntimeConfigScopeWorkspace
	}
	return contract.RuntimeConfigScopeApp
}

func variableStorage(secret bool) string {
	if secret {
		return string(contract.RuntimeVariableStorageSecret)
	}
	return string(contract.RuntimeVariableStoragePlain)
}

func provisionedRevision(current, desired int64, same bool) (int64, bool, error) {
	if desired < 0 {
		return 0, false, fmt.Errorf("%w: revision must not be negative", ErrInvalidState)
	}
	if desired > 0 {
		if current > 0 && (current != desired || !same) {
			return 0, false, runtimeConfigRevisionError(current)
		}
		return desired, current == desired && same, nil
	}
	if current > 0 && same {
		return current, true, nil
	}
	return max(current+1, 1), false, nil
}

func appendProvisioningRuntimeAudit(snapshot *Snapshot, now time.Time, batch RuntimeConfigProvisioningBatch, appKey, path, kind, storage string, revision int64) {
	snapshot.RuntimeConfigAudits[batch.WorkspaceID] = append(snapshot.RuntimeConfigAudits[batch.WorkspaceID], RuntimeConfigAudit{
		ID: NewID("audit"), WorkspaceID: batch.WorkspaceID, OwnerScope: runtimeOwnerScope(appKey), AppKey: appKey,
		Path: path, ObjectKind: kind, Storage: storage, Revision: revision, OperationID: "provisioning",
		Actor: batch.Actor, CreatedAt: now.UTC(),
	})
}
