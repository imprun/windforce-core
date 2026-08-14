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
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
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

func (s *LocalStore) CreateWorkspace(ctx context.Context, workspaceID string, name string, actor string) (Workspace, error) {
	var workspaceKey WorkspaceKey
	if strings.TrimSpace(s.SecretKey) != "" {
		wrapped, version, err := wfcrypto.NewWrappedDEK(s.SecretKey)
		if err != nil {
			return Workspace{}, fmt.Errorf("create workspace data-encryption key: %w", err)
		}
		workspaceKey = WorkspaceKey{Key: wrapped, KEKVersion: version}
	}
	var created Workspace
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if _, exists := snapshot.Workspaces[workspaceID]; exists {
			return fmt.Errorf("%w: workspace already exists", ErrConflict)
		}
		created = Workspace{ID: workspaceID, Name: name, Status: WorkspaceActive, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
		snapshot.Workspaces[workspaceID] = created
		if workspaceKey.Key != "" {
			snapshot.WorkspaceKeys[workspaceID] = workspaceKey
		}
		appendLocalWorkspaceAudit(snapshot, workspaceID, "created", "", actor, now)
		return nil
	})
	return created, err
}

func (s *LocalStore) GetWorkspaceKeyVersioned(ctx context.Context, workspaceID string) (string, int32, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return "", 0, err
	}
	key, ok := snapshot.WorkspaceKeys[contract.NormalizeWorkspace(workspaceID)]
	if !ok {
		return "", 0, nil
	}
	return key.Key, key.KEKVersion, nil
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

func (s *LocalStore) DeleteWorkspace(ctx context.Context, workspaceID string, actor string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if workspaceID == contract.DefaultWorkspace {
		return fmt.Errorf("%w: default workspace cannot be deleted", ErrInvalidState)
	}
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		if _, exists := snapshot.Workspaces[workspaceID]; !exists {
			return ErrNotFound
		}
		purgeLocalWorkspace(snapshot, workspaceID)
		return nil
	})
}

func purgeLocalWorkspace(snapshot *Snapshot, workspaceID string) {
	runIDs := map[string]struct{}{}
	jobIDs := map[string]struct{}{}
	for id, run := range snapshot.Runs {
		if contract.NormalizeWorkspace(run.Deployment.SourceWorkspace()) == workspaceID {
			runIDs[id] = struct{}{}
		}
	}
	for id, job := range snapshot.Jobs {
		if normalizedJobWorkspace("", job) != workspaceID {
			continue
		}
		runIDs[job.RunID] = struct{}{}
		jobIDs[id] = struct{}{}
		delete(snapshot.Jobs, id)
		delete(snapshot.WorkerLeaseIdentities, id)
	}
	for id := range runIDs {
		delete(snapshot.Runs, id)
	}
	for id, task := range snapshot.HumanTasks {
		if _, remove := runIDs[task.RunID]; remove {
			delete(snapshot.HumanTasks, id)
		}
	}
	filteredEvents := snapshot.Events[:0]
	for _, event := range snapshot.Events {
		if _, remove := runIDs[event.RunID]; !remove {
			filteredEvents = append(filteredEvents, event)
		}
	}
	snapshot.Events = filteredEvents
	for id, record := range snapshot.JobLogs {
		_, removeByJob := jobIDs[id]
		if removeByJob || contract.NormalizeWorkspace(record.WorkspaceID) == workspaceID {
			delete(snapshot.JobLogs, id)
		}
	}

	delete(snapshot.JobState, workspaceID)
	delete(snapshot.Variables, workspaceID)
	delete(snapshot.Resources, workspaceID)
	delete(snapshot.ResourceTypes, workspaceID)
	delete(snapshot.Clients, workspaceID)
	delete(snapshot.ClientAudits, workspaceID)
	delete(snapshot.LegacyClients, workspaceID)
	delete(snapshot.LegacyClientAudits, workspaceID)
	delete(snapshot.ServicePrincipals, workspaceID)
	delete(snapshot.ServicePrincipalAudits, workspaceID)
	delete(snapshot.InputConfigs, workspaceID)
	delete(snapshot.InputConfigAudits, workspaceID)
	delete(snapshot.SecretAccessAudits, workspaceID)
	delete(snapshot.WebhookAudits, workspaceID)
	delete(snapshot.WorkspaceKeys, workspaceID)
	delete(snapshot.WorkspaceTokens, workspaceID)
	delete(snapshot.Workspaces, workspaceID)

	for key, subscription := range snapshot.WebhookSubscriptions {
		if contract.NormalizeWorkspace(subscription.WorkspaceID) == workspaceID {
			delete(snapshot.WebhookSubscriptions, key)
		}
	}
	for key, delivery := range snapshot.WebhookDeliveries {
		if contract.NormalizeWorkspace(delivery.WorkspaceID) == workspaceID {
			delete(snapshot.WebhookDeliveries, key)
		}
	}
	workspaceEventSource := "/workspaces/" + workspaceID + "/control-plane"
	for key, event := range snapshot.ControlPlaneEvents {
		if event.Source == workspaceEventSource {
			delete(snapshot.ControlPlaneEvents, key)
		}
	}
	for key, trigger := range snapshot.Triggers {
		if contract.NormalizeWorkspace(trigger.WorkspaceID) == workspaceID {
			delete(snapshot.Triggers, key)
		}
	}
	for key, records := range snapshot.TriggerAudits {
		filtered := records[:0]
		for _, record := range records {
			if contract.NormalizeWorkspace(record.WorkspaceID) != workspaceID {
				filtered = append(filtered, record)
			}
		}
		if len(filtered) == 0 {
			delete(snapshot.TriggerAudits, key)
		} else {
			snapshot.TriggerAudits[key] = filtered
		}
	}
	for key, delivery := range snapshot.TriggerDeliveries {
		if contract.NormalizeWorkspace(delivery.WorkspaceID) == workspaceID {
			delete(snapshot.TriggerDeliveries, key)
		}
	}
	for key, binding := range snapshot.HTTPRouteBindings {
		if contract.NormalizeWorkspace(binding.WorkspaceID) == workspaceID {
			delete(snapshot.HTTPRouteBindings, key)
		}
	}
	for key, records := range snapshot.HTTPRouteBindingAudits {
		filtered := records[:0]
		for _, record := range records {
			if contract.NormalizeWorkspace(record.WorkspaceID) != workspaceID {
				filtered = append(filtered, record)
			}
		}
		if len(filtered) == 0 {
			delete(snapshot.HTTPRouteBindingAudits, key)
		} else {
			snapshot.HTTPRouteBindingAudits[key] = filtered
		}
	}

	for key, deployment := range snapshot.ReleaseCatalog.Deployments {
		if contract.NormalizeWorkspace(deployment.SourceWorkspace()) == workspaceID {
			delete(snapshot.ReleaseCatalog.Deployments, key)
			delete(snapshot.ReleaseCatalog.ActiveHistoryIDs, key)
		}
	}
	for key := range snapshot.ReleaseCatalog.RoutingPolicies {
		if strings.HasPrefix(key, workspaceID+"/") {
			delete(snapshot.ReleaseCatalog.RoutingPolicies, key)
		}
	}
	for key := range snapshot.ReleaseCatalog.ActiveHistoryIDs {
		if strings.HasPrefix(key, workspaceID+"/") {
			delete(snapshot.ReleaseCatalog.ActiveHistoryIDs, key)
		}
	}
	for key, candidate := range snapshot.ReleaseCatalog.Candidates {
		if contract.NormalizeWorkspace(candidate.Deployment.SourceWorkspace()) == workspaceID {
			delete(snapshot.ReleaseCatalog.Candidates, key)
		}
	}
	filteredHistory := snapshot.ReleaseCatalog.History[:0]
	for _, record := range snapshot.ReleaseCatalog.History {
		if contract.NormalizeWorkspace(record.Workspace) != workspaceID {
			filteredHistory = append(filteredHistory, record)
		}
	}
	snapshot.ReleaseCatalog.History = filteredHistory
	filteredCatalogAudit := snapshot.ReleaseCatalog.Audit[:0]
	for _, record := range snapshot.ReleaseCatalog.Audit {
		if contract.NormalizeWorkspace(record.Workspace) != workspaceID {
			filteredCatalogAudit = append(filteredCatalogAudit, record)
		}
	}
	snapshot.ReleaseCatalog.Audit = filteredCatalogAudit
	for key, marker := range snapshot.ReleaseCatalog.SourceMarkers {
		if contract.NormalizeWorkspace(marker.Workspace) == workspaceID {
			delete(snapshot.ReleaseCatalog.SourceMarkers, key)
		}
	}
	filteredWorkspaceAudit := snapshot.WorkspaceAudits[:0]
	for _, record := range snapshot.WorkspaceAudits {
		if contract.NormalizeWorkspace(record.WorkspaceID) != workspaceID {
			filteredWorkspaceAudit = append(filteredWorkspaceAudit, record)
		}
	}
	snapshot.WorkspaceAudits = filteredWorkspaceAudit
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
