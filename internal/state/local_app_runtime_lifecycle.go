package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func appRuntimeLifecycleKey(workspaceID, appKey string) string {
	return contract.NormalizeWorkspace(workspaceID) + "\x00" + strings.TrimSpace(appKey)
}

func activeAppRuntimeLifecycle(workspaceID, appKey string) AppRuntimeLifecycle {
	return AppRuntimeLifecycle{WorkspaceID: contract.NormalizeWorkspace(workspaceID), AppKey: strings.TrimSpace(appKey), State: AppRuntimeActive}
}

func (s *LocalStore) ListAppRuntimeLifecycles(ctx context.Context, workspaceID string) ([]AppRuntimeLifecycle, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	result := make([]AppRuntimeLifecycle, 0)
	for _, lifecycle := range snapshot.AppRuntimeLifecycles {
		if lifecycle.WorkspaceID == workspaceID {
			result = append(result, lifecycle)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AppKey < result[j].AppKey })
	return result, nil
}

func (s *LocalStore) GetAppRuntimeLifecycle(ctx context.Context, workspaceID, appKey string) (AppRuntimeLifecycle, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return AppRuntimeLifecycle{}, err
	}
	if lifecycle, ok := snapshot.AppRuntimeLifecycles[appRuntimeLifecycleKey(workspaceID, appKey)]; ok {
		return lifecycle, nil
	}
	return activeAppRuntimeLifecycle(workspaceID, appKey), nil
}

func (s *LocalStore) SetAppRuntimeLifecycle(ctx context.Context, request SetAppRuntimeLifecycleRequest) (AppRuntimeLifecycle, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.AppKey = strings.TrimSpace(request.AppKey)
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.AppKey == "" || request.Actor == "" {
		return AppRuntimeLifecycle{}, fmt.Errorf("%w: App key and actor are required", ErrInvalidState)
	}
	if request.State != AppRuntimeActive && request.State != AppRuntimeTombstoned && request.State != AppRuntimeRevoked {
		return AppRuntimeLifecycle{}, fmt.Errorf("%w: invalid App runtime state", ErrInvalidState)
	}
	var result AppRuntimeLifecycle
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := appRuntimeLifecycleKey(request.WorkspaceID, request.AppKey)
		current, found := snapshot.AppRuntimeLifecycles[key]
		if !found {
			current = activeAppRuntimeLifecycle(request.WorkspaceID, request.AppKey)
		}
		if request.ExpectedRevision != nil && current.Revision != *request.ExpectedRevision {
			return runtimeConfigRevisionError(current.Revision)
		}
		result = AppRuntimeLifecycle{WorkspaceID: request.WorkspaceID, AppKey: request.AppKey, State: request.State,
			Reason: request.Reason, Actor: request.Actor, Revision: current.Revision + 1, UpdatedAt: now.UTC()}
		snapshot.AppRuntimeLifecycles[key] = result
		snapshot.AppRuntimeLifecycleAudits[key] = append(snapshot.AppRuntimeLifecycleAudits[key], AppRuntimeLifecycleAudit{
			WorkspaceID: result.WorkspaceID, AppKey: result.AppKey, State: result.State, Reason: result.Reason,
			Actor: result.Actor, Revision: result.Revision, CreatedAt: now.UTC(),
		})
		if request.State == AppRuntimeRevoked {
			for jobID, job := range snapshot.Jobs {
				if contract.NormalizeWorkspace(job.Payload.Workspace) == request.WorkspaceID && job.Payload.App == request.AppKey && job.State == JobRunning {
					actor, reason := request.Actor, request.Reason
					if reason == "" {
						reason = "App runtime access was emergency revoked"
					}
					job.CanceledBy, job.CanceledReason, job.UpdatedAt = &actor, &reason, now.UTC()
					snapshot.Jobs[jobID] = job
				}
			}
		}
		return nil
	})
	return result, err
}

func (s *LocalStore) PurgeAppRuntimeConfig(ctx context.Context, request PurgeAppRuntimeConfigRequest) error {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.AppKey, request.Actor, request.Reason = strings.TrimSpace(request.AppKey), strings.TrimSpace(request.Actor), strings.TrimSpace(request.Reason)
	if request.AppKey == "" || request.Actor == "" {
		return fmt.Errorf("%w: App key and actor are required", ErrInvalidState)
	}
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := appRuntimeLifecycleKey(request.WorkspaceID, request.AppKey)
		lifecycle, found := snapshot.AppRuntimeLifecycles[key]
		if !found || lifecycle.State == AppRuntimeActive {
			return fmt.Errorf("%w: App must be tombstoned or revoked before purge", ErrConflict)
		}
		validLease := false
		for _, job := range snapshot.Jobs {
			if contract.NormalizeWorkspace(job.Payload.Workspace) == request.WorkspaceID && job.Payload.App == request.AppKey &&
				job.State == JobRunning && job.LeaseExpiresAt != nil && job.LeaseExpiresAt.After(now) {
				validLease = true
				break
			}
		}
		if validLease && !request.Force {
			return fmt.Errorf("%w: valid App Job leases still exist", ErrConflict)
		}
		for mapKey, variable := range snapshot.Variables[request.WorkspaceID] {
			if variable.OwnerScope == contract.RuntimeConfigScopeApp && variable.AppKey == request.AppKey {
				delete(snapshot.Variables[request.WorkspaceID], mapKey)
			}
		}
		for mapKey, resource := range snapshot.Resources[request.WorkspaceID] {
			if resource.OwnerScope == contract.RuntimeConfigScopeApp && resource.AppKey == request.AppKey {
				delete(snapshot.Resources[request.WorkspaceID], mapKey)
			}
		}
		snapshot.AppRuntimeLifecycleAudits[key] = append(snapshot.AppRuntimeLifecycleAudits[key], AppRuntimeLifecycleAudit{
			WorkspaceID: request.WorkspaceID, AppKey: request.AppKey, State: lifecycle.State, Reason: request.Reason,
			Actor: request.Actor, Revision: lifecycle.Revision, Purged: true, Forced: request.Force, CreatedAt: now.UTC(),
		})
		return nil
	})
}

func (s *LocalStore) ListAppRuntimeLifecycleAudit(ctx context.Context, workspaceID, appKey string) ([]AppRuntimeLifecycleAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	return append([]AppRuntimeLifecycleAudit(nil), snapshot.AppRuntimeLifecycleAudits[appRuntimeLifecycleKey(workspaceID, appKey)]...), nil
}

func appRuntimeAllowsRunningAttempt(snapshot *Snapshot, job Job) bool {
	lifecycle, found := snapshot.AppRuntimeLifecycles[appRuntimeLifecycleKey(workerJobWorkspace(job), job.Payload.App)]
	if !found || lifecycle.State == AppRuntimeActive {
		return true
	}
	if lifecycle.State == AppRuntimeRevoked {
		return false
	}
	return job.StartedAt != nil && !job.StartedAt.After(lifecycle.UpdatedAt)
}

func appRuntimeAllowsNewAttempt(snapshot *Snapshot, job Job) bool {
	lifecycle, found := snapshot.AppRuntimeLifecycles[appRuntimeLifecycleKey(workerJobWorkspace(job), job.Payload.App)]
	return !found || lifecycle.State == AppRuntimeActive
}
