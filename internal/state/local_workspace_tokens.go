package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListWorkspaceTokens(ctx context.Context, workspaceID string) ([]WorkspaceToken, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	items := make([]WorkspaceToken, 0, len(snapshot.WorkspaceTokens[workspaceID]))
	for _, token := range snapshot.WorkspaceTokens[workspaceID] {
		items = append(items, token)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *LocalStore) GetWorkspaceToken(ctx context.Context, workspaceID string, id string) (WorkspaceToken, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkspaceToken{}, err
	}
	token, ok := snapshot.WorkspaceTokens[contract.NormalizeWorkspace(workspaceID)][strings.TrimSpace(id)]
	if !ok {
		return WorkspaceToken{}, ErrNotFound
	}
	return token, nil
}

func (s *LocalStore) GetWorkspaceTokenByTokenHash(ctx context.Context, workspaceID string, tokenHash string) (WorkspaceToken, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkspaceToken{}, err
	}
	for _, token := range snapshot.WorkspaceTokens[contract.NormalizeWorkspace(workspaceID)] {
		if tokenHash != "" && token.TokenHash == tokenHash && token.RevokedAt == nil {
			return token, nil
		}
	}
	return WorkspaceToken{}, ErrNotFound
}

func (s *LocalStore) CreateWorkspaceToken(ctx context.Context, workspaceID string, name string, tokenHash string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	name = strings.TrimSpace(name)
	if name == "" || tokenHash == "" {
		return WorkspaceToken{}, ErrInvalidState
	}
	var created WorkspaceToken
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		workspace, ok := snapshot.Workspaces[workspaceID]
		if !ok {
			return ErrNotFound
		}
		if workspace.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		if snapshot.WorkspaceTokens[workspaceID] == nil {
			snapshot.WorkspaceTokens[workspaceID] = map[string]WorkspaceToken{}
		}
		if workspaceTokenHashExists(snapshot.WorkspaceTokens[workspaceID], tokenHash, "") {
			return fmt.Errorf("%w: workspace token already exists", ErrConflict)
		}
		created = WorkspaceToken{
			ID: NewID("workspace_token"), WorkspaceID: workspaceID, Name: name, TokenHash: tokenHash,
			CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now,
		}
		snapshot.WorkspaceTokens[workspaceID][created.ID] = created
		appendLocalWorkspaceAudit(snapshot, workspaceID, "token_created", created.ID+": "+name, actor, now)
		return nil
	})
	return created, err
}

func (s *LocalStore) RotateWorkspaceToken(ctx context.Context, workspaceID string, id string, tokenHash string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if tokenHash == "" {
		return WorkspaceToken{}, ErrInvalidState
	}
	var updated WorkspaceToken
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		workspace, ok := snapshot.Workspaces[workspaceID]
		if !ok {
			return ErrNotFound
		}
		if workspace.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		current, ok := snapshot.WorkspaceTokens[workspaceID][id]
		if !ok {
			return ErrNotFound
		}
		if current.RevokedAt != nil {
			return ErrInvalidState
		}
		if workspaceTokenHashExists(snapshot.WorkspaceTokens[workspaceID], tokenHash, id) {
			return fmt.Errorf("%w: workspace token already exists", ErrConflict)
		}
		current.TokenHash = tokenHash
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.WorkspaceTokens[workspaceID][id] = current
		appendLocalWorkspaceAudit(snapshot, workspaceID, "token_rotated", id+": "+current.Name, actor, now)
		updated = current
		return nil
	})
	return updated, err
}

func (s *LocalStore) RevokeWorkspaceToken(ctx context.Context, workspaceID string, id string, actor string) (WorkspaceToken, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var updated WorkspaceToken
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, ok := snapshot.WorkspaceTokens[workspaceID][id]
		if !ok {
			return ErrNotFound
		}
		if current.RevokedAt != nil || current.TokenHash == "" {
			return ErrInvalidState
		}
		current.TokenHash = ""
		current.RevokedAt = &now
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.WorkspaceTokens[workspaceID][id] = current
		appendLocalWorkspaceAudit(snapshot, workspaceID, "token_revoked", id+": "+current.Name, actor, now)
		updated = current
		return nil
	})
	return updated, err
}

func workspaceTokenHashExists(tokens map[string]WorkspaceToken, tokenHash string, exceptID string) bool {
	if tokenHash == "" {
		return false
	}
	for id, token := range tokens {
		if id != exceptID && token.TokenHash == tokenHash {
			return true
		}
	}
	return false
}
