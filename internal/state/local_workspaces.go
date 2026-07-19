package state

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	WorkspaceActive   = "active"
	WorkspaceArchived = "archived"
)

func HashWorkspaceToken(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func WorkspaceTokenMatches(workspace Workspace, value string) bool {
	if workspace.TokenHash == "" || strings.TrimSpace(value) == "" {
		return false
	}
	want, err := hex.DecodeString(workspace.TokenHash)
	if err != nil {
		return false
	}
	got, err := hex.DecodeString(HashWorkspaceToken(value))
	return err == nil && subtle.ConstantTimeCompare(want, got) == 1
}

func ensureLocalWorkspaces(snapshot *Snapshot) {
	ids := map[string]bool{contract.DefaultWorkspace: true}
	for workspaceID := range snapshot.JobState {
		ids[contract.NormalizeWorkspace(workspaceID)] = true
	}
	for workspaceID := range snapshot.Variables {
		ids[contract.NormalizeWorkspace(workspaceID)] = true
	}
	for workspaceID := range snapshot.Resources {
		ids[contract.NormalizeWorkspace(workspaceID)] = true
	}
	for workspaceID := range snapshot.Clients {
		ids[contract.NormalizeWorkspace(workspaceID)] = true
	}
	for workspaceID := range snapshot.InputConfigs {
		ids[contract.NormalizeWorkspace(workspaceID)] = true
	}
	for _, job := range snapshot.Jobs {
		ids[normalizedJobWorkspace("", job)] = true
	}
	for _, subscription := range snapshot.WebhookSubscriptions {
		ids[contract.NormalizeWorkspace(subscription.WorkspaceID)] = true
	}
	for _, deployment := range snapshot.ReleaseCatalog.Deployments {
		ids[contract.NormalizeWorkspace(deployment.SourceWorkspace())] = true
	}
	for _, history := range snapshot.ReleaseCatalog.History {
		ids[contract.NormalizeWorkspace(history.Workspace)] = true
	}
	for _, record := range snapshot.ReleaseCatalog.Audit {
		ids[contract.NormalizeWorkspace(record.Workspace)] = true
	}
	for _, marker := range snapshot.ReleaseCatalog.SourceMarkers {
		ids[contract.NormalizeWorkspace(marker.Workspace)] = true
	}
	now := time.Now().UTC()
	for workspaceID := range ids {
		if _, exists := snapshot.Workspaces[workspaceID]; exists {
			continue
		}
		name := workspaceID
		if workspaceID == contract.DefaultWorkspace {
			name = "Default"
		}
		snapshot.Workspaces[workspaceID] = Workspace{
			ID: workspaceID, Name: name, Status: WorkspaceActive,
			CreatedBy: "system", UpdatedBy: "system", CreatedAt: now, UpdatedAt: now,
		}
	}
}

func (s *LocalStore) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Workspace, 0, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		items = append(items, workspace)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Status != items[j].Status {
			return items[i].Status < items[j].Status
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *LocalStore) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return Workspace{}, err
	}
	workspace, ok := snapshot.Workspaces[contract.NormalizeWorkspace(workspaceID)]
	if !ok {
		return Workspace{}, ErrNotFound
	}
	return workspace, nil
}

func (s *LocalStore) CreateWorkspace(ctx context.Context, workspaceID string, name string, tokenHash string, actor string) (Workspace, error) {
	var created Workspace
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if _, exists := snapshot.Workspaces[workspaceID]; exists {
			return fmt.Errorf("%w: workspace already exists", ErrConflict)
		}
		created = Workspace{ID: workspaceID, Name: name, Status: WorkspaceActive, TokenHash: tokenHash, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		snapshot.Workspaces[workspaceID] = created
		appendLocalWorkspaceAudit(snapshot, workspaceID, "created", "", actor, now)
		return nil
	})
	return created, err
}

func (s *LocalStore) UpdateWorkspace(ctx context.Context, workspaceID string, name string, actor string) (Workspace, error) {
	var updated Workspace
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, exists := snapshot.Workspaces[workspaceID]
		if !exists {
			return ErrNotFound
		}
		if current.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		detail := "no value change"
		if current.Name != name {
			detail = "display name changed"
		}
		current.Name = name
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.Workspaces[workspaceID] = current
		appendLocalWorkspaceAudit(snapshot, workspaceID, "updated", detail, actor, now)
		updated = current
		return nil
	})
	return updated, err
}

func (s *LocalStore) ArchiveWorkspace(ctx context.Context, workspaceID string, actor string) (Workspace, error) {
	var archived Workspace
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, exists := snapshot.Workspaces[workspaceID]
		if !exists {
			return ErrNotFound
		}
		if workspaceID == contract.DefaultWorkspace {
			return fmt.Errorf("%w: default workspace cannot be archived", ErrInvalidState)
		}
		if current.Status == WorkspaceArchived {
			return fmt.Errorf("%w: workspace is already archived", ErrInvalidState)
		}
		current.Status = WorkspaceArchived
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.Workspaces[workspaceID] = current
		appendLocalWorkspaceAudit(snapshot, workspaceID, "archived", "", actor, now)
		archived = current
		return nil
	})
	return archived, err
}

func (s *LocalStore) RotateWorkspaceToken(ctx context.Context, workspaceID string, tokenHash string, actor string) (Workspace, error) {
	var updated Workspace
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, exists := snapshot.Workspaces[workspaceID]
		if !exists {
			return ErrNotFound
		}
		if current.Status == WorkspaceArchived {
			return ErrInvalidState
		}
		current.TokenHash = tokenHash
		current.UpdatedBy = actor
		current.UpdatedAt = now
		snapshot.Workspaces[workspaceID] = current
		appendLocalWorkspaceAudit(snapshot, workspaceID, "token_rotated", "", actor, now)
		updated = current
		return nil
	})
	return updated, err
}

func (s *LocalStore) ListWorkspaceAudit(ctx context.Context, workspaceID string) ([]WorkspaceAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	items := []WorkspaceAudit{}
	for _, record := range snapshot.WorkspaceAudits {
		if workspaceID == "" || record.WorkspaceID == workspaceID {
			items = append(items, record)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func appendLocalWorkspaceAudit(snapshot *Snapshot, workspaceID string, kind string, detail string, actor string, now time.Time) {
	snapshot.WorkspaceAudits = append(snapshot.WorkspaceAudits, WorkspaceAudit{
		ID: NewID("audit"), WorkspaceID: workspaceID, Kind: kind, Detail: detail, Actor: actor, CreatedAt: now,
	})
}
