package state

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListServicePrincipals(ctx context.Context, workspaceID string) ([]ServicePrincipal, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	principals := make([]ServicePrincipal, 0, len(snapshot.ServicePrincipals[workspaceID]))
	for _, principal := range snapshot.ServicePrincipals[workspaceID] {
		principals = append(principals, cloneServicePrincipal(principal))
	}
	sort.Slice(principals, func(i, j int) bool {
		if principals[i].Name != principals[j].Name {
			return principals[i].Name < principals[j].Name
		}
		return principals[i].ID < principals[j].ID
	})
	return principals, nil
}

func (s *LocalStore) GetServicePrincipal(ctx context.Context, workspaceID string, id string) (ServicePrincipal, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return ServicePrincipal{}, err
	}
	principal, ok := snapshot.ServicePrincipals[contract.NormalizeWorkspace(workspaceID)][id]
	if !ok {
		return ServicePrincipal{}, ErrNotFound
	}
	return cloneServicePrincipal(principal), nil
}

func (s *LocalStore) GetServicePrincipalByTokenHash(ctx context.Context, workspaceID string, tokenHash string) (ServicePrincipal, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return ServicePrincipal{}, err
	}
	for _, principal := range snapshot.ServicePrincipals[contract.NormalizeWorkspace(workspaceID)] {
		if tokenHash != "" && principal.TokenHash == tokenHash {
			return cloneServicePrincipal(principal), nil
		}
	}
	return ServicePrincipal{}, ErrNotFound
}

func (s *LocalStore) CreateServicePrincipal(ctx context.Context, principal ServicePrincipal, tokenHash string, actor string) (ServicePrincipal, error) {
	principal.WorkspaceID = contract.NormalizeWorkspace(principal.WorkspaceID)
	var created ServicePrincipal
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if snapshot.ServicePrincipals[principal.WorkspaceID] == nil {
			snapshot.ServicePrincipals[principal.WorkspaceID] = map[string]ServicePrincipal{}
		}
		if serviceTokenHashExists(snapshot.ServicePrincipals[principal.WorkspaceID], tokenHash, "") {
			return fmt.Errorf("%w: service principal token already exists", ErrConflict)
		}
		created = cloneServicePrincipal(principal)
		created.ID = NewID("service")
		created.TokenHash = tokenHash
		created.CreatedBy = actor
		created.UpdatedBy = actor
		created.CreatedAt = now
		created.UpdatedAt = now
		snapshot.ServicePrincipals[principal.WorkspaceID][created.ID] = created
		appendLocalServicePrincipalAudit(snapshot, created.WorkspaceID, created.ID, "created", "", actor, now)
		return nil
	})
	return cloneServicePrincipal(created), err
}

func (s *LocalStore) UpdateServicePrincipal(ctx context.Context, principal ServicePrincipal, actor string) (ServicePrincipal, error) {
	principal.WorkspaceID = contract.NormalizeWorkspace(principal.WorkspaceID)
	var updated ServicePrincipal
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, ok := snapshot.ServicePrincipals[principal.WorkspaceID][principal.ID]
		if !ok {
			return ErrNotFound
		}
		detail := "authorization changed"
		if current.Name != principal.Name {
			detail = "name or authorization changed"
		}
		current.Name = principal.Name
		current.Scopes = append([]string(nil), principal.Scopes...)
		current.AllowedTargets = append([]string(nil), principal.AllowedTargets...)
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.ServicePrincipals[principal.WorkspaceID][principal.ID] = current
		appendLocalServicePrincipalAudit(snapshot, current.WorkspaceID, current.ID, "updated", detail, actor, now)
		updated = current
		return nil
	})
	return cloneServicePrincipal(updated), err
}

func (s *LocalStore) RotateServicePrincipalToken(ctx context.Context, workspaceID string, id string, tokenHash string, actor string) (ServicePrincipal, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if tokenHash == "" {
		return ServicePrincipal{}, ErrInvalidState
	}
	var updated ServicePrincipal
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, ok := snapshot.ServicePrincipals[workspaceID][id]
		if !ok {
			return ErrNotFound
		}
		if serviceTokenHashExists(snapshot.ServicePrincipals[workspaceID], tokenHash, id) {
			return fmt.Errorf("%w: service principal token already exists", ErrConflict)
		}
		current.TokenHash = tokenHash
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.ServicePrincipals[workspaceID][id] = current
		appendLocalServicePrincipalAudit(snapshot, workspaceID, id, "token_rotated", "", actor, now)
		updated = current
		return nil
	})
	return cloneServicePrincipal(updated), err
}

func (s *LocalStore) RevokeServicePrincipalToken(ctx context.Context, workspaceID string, id string, actor string) (ServicePrincipal, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated ServicePrincipal
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, ok := snapshot.ServicePrincipals[workspaceID][id]
		if !ok {
			return ErrNotFound
		}
		if current.TokenHash == "" {
			return ErrInvalidState
		}
		current.TokenHash = ""
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.ServicePrincipals[workspaceID][id] = current
		appendLocalServicePrincipalAudit(snapshot, workspaceID, id, "token_revoked", "", actor, now)
		updated = current
		return nil
	})
	return cloneServicePrincipal(updated), err
}

func (s *LocalStore) DeleteServicePrincipal(ctx context.Context, workspaceID string, id string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		principal, ok := snapshot.ServicePrincipals[workspaceID][id]
		if !ok {
			return ErrNotFound
		}
		if principal.TokenHash != "" {
			return fmt.Errorf("%w: revoke the active service principal token before deleting the principal", ErrConflict)
		}
		delete(snapshot.ServicePrincipals[workspaceID], id)
		appendLocalServicePrincipalAudit(snapshot, workspaceID, id, "deleted", "", actor, now)
		return nil
	})
}

func (s *LocalStore) ListServicePrincipalAudit(ctx context.Context, workspaceID string, id string) ([]ServicePrincipalAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := []ServicePrincipalAudit{}
	for _, record := range snapshot.ServicePrincipalAudits[contract.NormalizeWorkspace(workspaceID)] {
		if id == "" || record.ServicePrincipalID == id {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	return records, nil
}

func serviceTokenHashExists(principals map[string]ServicePrincipal, tokenHash string, exceptID string) bool {
	if tokenHash == "" {
		return false
	}
	for id, principal := range principals {
		if id != exceptID && principal.TokenHash == tokenHash {
			return true
		}
	}
	return false
}

func appendLocalServicePrincipalAudit(snapshot *Snapshot, workspaceID string, id string, kind string, detail string, actor string, now time.Time) {
	snapshot.ServicePrincipalAudits[workspaceID] = append(snapshot.ServicePrincipalAudits[workspaceID], ServicePrincipalAudit{
		ID: NewID("audit"), WorkspaceID: workspaceID, ServicePrincipalID: id,
		Kind: kind, Detail: detail, Actor: actor, CreatedAt: now,
	})
}

func cloneServicePrincipal(principal ServicePrincipal) ServicePrincipal {
	principal.Scopes = append([]string(nil), principal.Scopes...)
	principal.AllowedTargets = append([]string(nil), principal.AllowedTargets...)
	return principal
}
