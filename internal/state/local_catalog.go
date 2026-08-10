package state

import (
	"context"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

var _ catalog.Store = (*LocalStore)(nil)

func (s *LocalStore) PublishRelease(ctx context.Context, deployment contract.Deployment, releasedAt time.Time) (catalog.ReleasePublication, error) {
	var published contract.Deployment
	var releaseID string
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if releasedAt.IsZero() {
			releasedAt = now
		}
		var history catalog.DeploymentHistory
		var audit catalog.AuditRecord
		published, history, audit = catalog.PreparePublication(deployment, releasedAt)
		releaseID = history.ID
		releaseCatalog := &snapshot.ReleaseCatalog
		catalog.NormalizeSnapshot(releaseCatalog)
		previous := latestReleaseHistory(*releaseCatalog, published.SourceWorkspace(), published.App)
		deploymentKey := catalog.DeploymentKey(published.SourceWorkspace(), published.App)
		releaseCatalog.Deployments[deploymentKey] = published
		releaseCatalog.ActiveHistoryIDs[deploymentKey] = history.ID
		releaseCatalog.History = append(releaseCatalog.History, history)
		releaseCatalog.Audit = append(releaseCatalog.Audit, audit)
		marker := catalog.SourceReleaseMarker{
			Workspace:   published.SourceWorkspace(),
			GitSourceID: published.SourceGitSourceID(),
			Commit:      published.Commit,
			ReleasedAt:  history.CreatedAt,
		}
		releaseCatalog.SourceMarkers[catalog.SourceReleaseKey(marker.Workspace, marker.GitSourceID)] = marker
		releaseEvent, err := prepareReleaseEvent(history, previous)
		if err != nil {
			return err
		}
		snapshot.ControlPlaneEvents[releaseEvent.ID] = releaseEvent
		for _, subscription := range matchingSubscriptions(snapshot.WebhookSubscriptions, published.SourceWorkspace(), releaseEvent.Type, published.App) {
			delivery := newWebhookDelivery(releaseEvent, published.SourceWorkspace(), subscription.ID, now)
			snapshot.WebhookDeliveries[delivery.ID] = delivery
		}
		return nil
	})
	return catalog.ReleasePublication{Deployment: published, ReleaseID: releaseID}, err
}

func (s *LocalStore) RollbackRelease(ctx context.Context, request catalog.ReleaseRollbackRequest) (catalog.ReleaseRollbackResult, error) {
	var result catalog.ReleaseRollbackResult
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if request.RolledBackAt.IsZero() {
			request.RolledBackAt = now
		}
		var err error
		result, err = catalog.ApplyReleaseRollback(&snapshot.ReleaseCatalog, request)
		if err != nil {
			return err
		}
		rollbackEvent, err := prepareReleaseRollbackEvent(result)
		if err != nil {
			return err
		}
		snapshot.ControlPlaneEvents[rollbackEvent.ID] = rollbackEvent
		for _, subscription := range matchingSubscriptions(snapshot.WebhookSubscriptions, result.Target.Workspace, rollbackEvent.Type, result.Target.App) {
			delivery := newWebhookDelivery(rollbackEvent, result.Target.Workspace, subscription.ID, now)
			snapshot.WebhookDeliveries[delivery.ID] = delivery
		}
		return nil
	})
	return result, err
}

func (s *LocalStore) GetDeployment(ctx context.Context, app string) (contract.Deployment, error) {
	return s.GetDeploymentForWorkspace(ctx, contract.DefaultWorkspace, app)
}

func (s *LocalStore) GetDeploymentForWorkspace(ctx context.Context, workspace string, app string) (contract.Deployment, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return contract.Deployment{}, err
	}
	deployment, ok := snapshot.ReleaseCatalog.Deployments[catalog.DeploymentKey(workspace, app)]
	if !ok {
		return contract.Deployment{}, catalog.ErrDeploymentNotFound
	}
	policy := snapshot.ReleaseCatalog.RoutingPolicies[catalog.RoutingPolicyKey(workspace, app)]
	return catalog.ApplyRoutingPolicy(deployment, policy), nil
}

func (s *LocalStore) LoadCatalog(ctx context.Context) (catalog.Snapshot, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return catalog.Snapshot{}, err
	}
	return catalog.SnapshotWithAppliedRoutingPolicies(snapshot.ReleaseCatalog), nil
}

func (s *LocalStore) AppendAudit(ctx context.Context, record catalog.AuditRecord) error {
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		record = catalog.PrepareAuditRecord(record, now)
		snapshot.ReleaseCatalog.Audit = append(snapshot.ReleaseCatalog.Audit, record)
		return nil
	})
}

func (s *LocalStore) AuditTrail(ctx context.Context, workspace string, gitSourceID string) ([]catalog.AuditRecord, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]catalog.AuditRecord, 0)
	for _, record := range snapshot.ReleaseCatalog.Audit {
		if record.Workspace == workspace && record.GitSourceID == gitSourceID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *LocalStore) SetAppTagOverride(ctx context.Context, workspace string, app string, tagOverride *string) (contract.Deployment, error) {
	return s.SetAppRoutingPolicy(ctx, workspace, app, catalog.RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: tagOverride})
}

func (s *LocalStore) GetRoutingPolicy(ctx context.Context, workspace string, app string) (catalog.RoutingPolicy, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return catalog.RoutingPolicy{}, err
	}
	key := catalog.DeploymentKey(workspace, app)
	policy, ok := snapshot.ReleaseCatalog.RoutingPolicies[key]
	if !ok {
		policy = catalog.NewRoutingPolicy(workspace, app)
	}
	return catalog.NormalizeRoutingPolicy(policy), nil
}

func (s *LocalStore) SetInitialAppRoutingPolicy(ctx context.Context, workspace string, app string, patch catalog.RoutingPolicyPatch) (catalog.RoutingPolicy, error) {
	var updated catalog.RoutingPolicy
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := catalog.DeploymentKey(workspace, app)
		policy := snapshot.ReleaseCatalog.RoutingPolicies[key]
		if policy.App == "" {
			policy = catalog.NewRoutingPolicy(workspace, app)
		}
		updated = catalog.ApplyRoutingPolicyPatch(policy, "", patch, now)
		snapshot.ReleaseCatalog.RoutingPolicies[key] = updated
		return nil
	})
	return updated, err
}

func (s *LocalStore) SetAppRoutingPolicy(ctx context.Context, workspace string, app string, patch catalog.RoutingPolicyPatch) (contract.Deployment, error) {
	var updated contract.Deployment
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := catalog.DeploymentKey(workspace, app)
		deployment, ok := snapshot.ReleaseCatalog.Deployments[key]
		if !ok {
			return catalog.ErrDeploymentNotFound
		}
		policy := snapshot.ReleaseCatalog.RoutingPolicies[key]
		if policy.App == "" {
			policy = catalog.NewRoutingPolicy(workspace, app)
		}
		policy = catalog.ApplyRoutingPolicyPatch(policy, "", patch, now)
		snapshot.ReleaseCatalog.RoutingPolicies[key] = policy
		updated = catalog.ApplyRoutingPolicy(deployment, policy)
		return nil
	})
	return updated, err
}

func (s *LocalStore) SetActionTagOverride(ctx context.Context, workspace string, app string, actionKey string, tagOverride *string) (contract.Action, error) {
	return s.SetActionRoutingPolicy(ctx, workspace, app, actionKey, catalog.RoutingPolicyPatch{RouteTagSet: true, RouteTagOverride: tagOverride})
}

func (s *LocalStore) SetActionRoutingPolicy(ctx context.Context, workspace string, app string, actionKey string, patch catalog.RoutingPolicyPatch) (contract.Action, error) {
	var updated contract.Action
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := catalog.DeploymentKey(workspace, app)
		deployment, ok := snapshot.ReleaseCatalog.Deployments[key]
		if !ok {
			return catalog.ErrDeploymentNotFound
		}
		if _, ok := deployment.Actions[actionKey]; !ok {
			return catalog.ErrActionNotFound
		}
		policy := snapshot.ReleaseCatalog.RoutingPolicies[key]
		if policy.App == "" {
			policy = catalog.NewRoutingPolicy(workspace, app)
		}
		policy = catalog.ApplyRoutingPolicyPatch(policy, actionKey, patch, now)
		snapshot.ReleaseCatalog.RoutingPolicies[key] = policy
		updated = catalog.ApplyRoutingPolicy(deployment, policy).Actions[actionKey]
		return nil
	})
	return updated, err
}

func (s *LocalStore) ListSourceReleaseMarkers(ctx context.Context) (map[string]catalog.SourceReleaseMarker, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	markers := make(map[string]catalog.SourceReleaseMarker, len(snapshot.ReleaseCatalog.SourceMarkers))
	for key, marker := range snapshot.ReleaseCatalog.SourceMarkers {
		markers[key] = marker
	}
	return markers, nil
}

func (s *LocalStore) ImportCatalog(ctx context.Context, imported catalog.Snapshot) error {
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		catalog.MergeSnapshot(&snapshot.ReleaseCatalog, imported)
		return nil
	})
}

func cloneCatalogString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func catalogTimePtr(value time.Time) *time.Time {
	return &value
}
