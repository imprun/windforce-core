package state

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

type routingPolicyCatalog interface {
	catalog.Store
	RollbackRelease(context.Context, catalog.ReleaseRollbackRequest) (catalog.ReleaseRollbackResult, error)
}

func TestLocalRoutingPolicySurvivesReleaseRollbackAndRestart(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store := NewLocalStore(path)
	verifyRoutingPolicyPersistence(t, store)

	restarted := NewLocalStore(path)
	assertRoutingPolicyApplied(t, restarted)
}

func TestPostgresRoutingPolicySurvivesReleaseAndRollback(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	verifyRoutingPolicyPersistence(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func TestLocalInitialRoutingPolicyAppliesToFirstRelease(t *testing.T) {
	verifyInitialRoutingPolicy(t, NewLocalStore(t.TempDir()+"/state.json"))
}

func TestPostgresInitialRoutingPolicyAppliesToFirstRelease(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	verifyInitialRoutingPolicy(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func TestPostgresConcurrentInitialPlacementPatchesPreserveIndependentFields(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	ctx := context.Background()
	tag := "browser"
	labels := []string{"gpu"}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, patch := range []catalog.RoutingPolicyPatch{
		{RouteTagSet: true, RouteTagOverride: &tag, Actor: "tag-operator"},
		{RequiredLabelsSet: true, RequiredLabelsOverride: &labels, Actor: "label-operator"},
	} {
		patch := patch
		go func() {
			ready.Done()
			<-start
			_, err := store.SetInitialAppRoutingPolicy(ctx, "workspace-a", "echo", patch)
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	policy, err := store.GetRoutingPolicy(ctx, "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.RouteTagOverride == nil || *policy.RouteTagOverride != tag ||
		policy.RequiredLabelsOverride == nil || !reflect.DeepEqual(*policy.RequiredLabelsOverride, labels) {
		t.Fatalf("concurrent policy = %#v", policy)
	}
}

func TestPostgresMigrationExtractsEmbeddedRoutingPolicy(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	ctx := context.Background()
	legacy := routingPolicyDeployment("legacy-commit", "manifest-app", "manifest-action", []string{"linux"}, []string{"browser"})
	appTag := "legacy-app-override"
	appLabels := []string{"gpu"}
	actionTag := "legacy-action-override"
	emptyLabels := []string{}
	legacy.TagOverride = &appTag
	legacy.RequiredLabelsOverride = &appLabels
	action := legacy.Actions["run"]
	action.TagOverride = &actionTag
	action.RequiredLabelsOverride = &emptyLabels
	legacy.Actions["run"] = action
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `
INSERT INTO control_active_release (workspace_id, app_key, deployment, updated_at)
VALUES ($1, $2, $3, $4)
`, "workspace-a", "echo", raw, time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var storedRaw []byte
	if err := store.pool.QueryRow(ctx, `
SELECT deployment FROM control_active_release WHERE workspace_id = $1 AND app_key = $2
`, "workspace-a", "echo").Scan(&storedRaw); err != nil {
		t.Fatal(err)
	}
	var stored contract.Deployment
	if err := json.Unmarshal(storedRaw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.TagOverride != nil || stored.RequiredLabelsOverride != nil ||
		stored.Actions["run"].TagOverride != nil || stored.Actions["run"].RequiredLabelsOverride != nil {
		t.Fatalf("active release still contains embedded placement overrides: %#v", stored)
	}

	policy, err := store.GetRoutingPolicy(ctx, "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.UpdatedBy != "system:migration" || policy.RouteTagOverride == nil || *policy.RouteTagOverride != appTag ||
		policy.RequiredLabelsOverride == nil || !reflect.DeepEqual(*policy.RequiredLabelsOverride, appLabels) {
		t.Fatalf("migrated app policy = %#v", policy)
	}
	actionPolicy := policy.Actions["run"]
	if actionPolicy.RouteTagOverride == nil || *actionPolicy.RouteTagOverride != actionTag ||
		actionPolicy.RequiredLabelsOverride == nil || len(*actionPolicy.RequiredLabelsOverride) != 0 {
		t.Fatalf("migrated action policy = %#v", actionPolicy)
	}

	applied, err := store.GetDeploymentForWorkspace(ctx, "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if got := contract.EffectiveRouteTagForAction(applied, applied.Actions["run"]); got != actionTag {
		t.Fatalf("effective migrated worker tag = %q, want %q", got, actionTag)
	}
	if labels := contract.EffectiveRequiredLabels(applied, applied.Actions["run"]); labels == nil || len(labels) != 0 {
		t.Fatalf("effective migrated labels = %#v, want explicit empty", labels)
	}
}

func TestPostgresRollbackDoesNotRestoreLegacyEmbeddedRoutingPolicy(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	ctx := context.Background()
	first, err := store.PublishRelease(ctx, routingPolicyDeployment(
		"commit-a", "manifest-app-a", "manifest-action-a", []string{"linux"}, []string{"browser"},
	), time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishRelease(ctx, routingPolicyDeployment(
		"commit-b", "manifest-app-b", "manifest-action-b", []string{"arm64"}, []string{"kr"},
	), time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	var recordRaw []byte
	if err := store.pool.QueryRow(ctx, `SELECT record FROM control_release_history WHERE id = $1`, first.ReleaseID).Scan(&recordRaw); err != nil {
		t.Fatal(err)
	}
	var legacy catalog.DeploymentHistory
	if err := json.Unmarshal(recordRaw, &legacy); err != nil {
		t.Fatal(err)
	}
	legacyTag := "legacy-embedded"
	legacyLabels := []string{"legacy"}
	legacy.Deployment.TagOverride = &legacyTag
	legacy.Deployment.RequiredLabelsOverride = &legacyLabels
	legacyAction := legacy.Deployment.Actions["run"]
	legacyAction.TagOverride = &legacyTag
	legacyAction.RequiredLabelsOverride = &legacyLabels
	legacy.Deployment.Actions["run"] = legacyAction
	recordRaw, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE control_release_history SET record = $2 WHERE id = $1`, first.ReleaseID, recordRaw); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RollbackRelease(ctx, catalog.ReleaseRollbackRequest{
		Workspace: "workspace-a", App: "echo", ReleaseID: first.ReleaseID,
		Actor: "operator@example.test", Reason: "legacy release regression",
		RolledBackAt: time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	var activeRaw []byte
	if err := store.pool.QueryRow(ctx, `
SELECT deployment FROM control_active_release WHERE workspace_id = $1 AND app_key = $2
`, "workspace-a", "echo").Scan(&activeRaw); err != nil {
		t.Fatal(err)
	}
	var active contract.Deployment
	if err := json.Unmarshal(activeRaw, &active); err != nil {
		t.Fatal(err)
	}
	if active.TagOverride != nil || active.RequiredLabelsOverride != nil ||
		active.Actions["run"].TagOverride != nil || active.Actions["run"].RequiredLabelsOverride != nil {
		t.Fatalf("rollback restored legacy placement into active release: %#v", active)
	}
}

func verifyInitialRoutingPolicy(t *testing.T, store routingPolicyCatalog) {
	t.Helper()
	ctx := context.Background()
	appTag := "operator-before-release"
	emptyLabels := []string{}
	policy, err := store.SetInitialAppRoutingPolicy(ctx, "workspace-a", "echo", catalog.RoutingPolicyPatch{
		RouteTagSet:            true,
		RouteTagOverride:       &appTag,
		RequiredLabelsSet:      true,
		RequiredLabelsOverride: &emptyLabels,
		Actor:                  "operator@example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.RouteTagOverride == nil || *policy.RouteTagOverride != appTag || policy.RequiredLabelsOverride == nil {
		t.Fatalf("initial policy = %#v", policy)
	}
	if _, err := store.PublishRelease(ctx, routingPolicyDeployment(
		"commit-first", "manifest-app", "manifest-action", []string{"linux"}, []string{"browser"},
	), time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	deployment, err := store.GetDeploymentForWorkspace(ctx, "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if got := contract.EffectiveRouteTagForApp(deployment); got != appTag {
		t.Fatalf("effective route tag = %q, want %q", got, appTag)
	}
	if labels := contract.EffectiveRequiredLabels(deployment, deployment.Actions["run"]); labels == nil || len(labels) != 0 {
		t.Fatalf("effective labels = %#v, want explicit empty", labels)
	}
}

func verifyRoutingPolicyPersistence(t *testing.T, store routingPolicyCatalog) {
	t.Helper()
	ctx := context.Background()
	first := routingPolicyDeployment("commit-a", "manifest-app-a", "manifest-action-a", []string{"linux"}, []string{"browser"})
	firstPublication, err := store.PublishRelease(ctx, first, time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	appTag := "operator-app"
	appLabels := []string{"gpu"}
	if _, err := store.SetAppRoutingPolicy(ctx, "workspace-a", "echo", catalog.RoutingPolicyPatch{
		RouteTagSet: true, RouteTagOverride: &appTag,
		RequiredLabelsSet: true, RequiredLabelsOverride: &appLabels,
		Actor: "operator@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	actionTag := "operator-action"
	emptyLabels := []string{}
	if _, err := store.SetActionRoutingPolicy(ctx, "workspace-a", "echo", "run", catalog.RoutingPolicyPatch{
		RouteTagSet: true, RouteTagOverride: &actionTag,
		RequiredLabelsSet: true, RequiredLabelsOverride: &emptyLabels,
		Actor: "operator@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	assertRoutingPolicyApplied(t, store)

	second := routingPolicyDeployment("commit-b", "manifest-app-b", "manifest-action-b", []string{"arm64"}, []string{"kr"})
	if _, err := store.PublishRelease(ctx, second, time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	assertRoutingPolicyApplied(t, store)

	if _, err := store.RollbackRelease(ctx, catalog.ReleaseRollbackRequest{
		Workspace: "workspace-a", App: "echo", ReleaseID: firstPublication.ReleaseID,
		Actor: "operator@example.test", Reason: "verify routing policy",
		RolledBackAt: time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	assertRoutingPolicyApplied(t, store)

	snapshot, err := store.LoadCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, history := range snapshot.History {
		if history.Deployment.TagOverride != nil || history.Deployment.RequiredLabelsOverride != nil {
			t.Fatalf("release history contains app routing override: %#v", history.Deployment)
		}
		for _, action := range history.Deployment.Actions {
			if action.TagOverride != nil || action.RequiredLabelsOverride != nil {
				t.Fatalf("release history contains action routing override: %#v", action)
			}
		}
	}
}

func assertRoutingPolicyApplied(t *testing.T, store interface {
	GetDeploymentForWorkspace(context.Context, string, string) (contract.Deployment, error)
}) {
	t.Helper()
	deployment, err := store.GetDeploymentForWorkspace(context.Background(), "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	action := deployment.Actions["run"]
	if got := contract.EffectiveRouteTagForAction(deployment, action); got != "operator-action" {
		t.Fatalf("effective route tag = %q, want operator-action", got)
	}
	if action.RequiredLabelsOverride == nil || len(*action.RequiredLabelsOverride) != 0 {
		t.Fatalf("action labels override = %#v, want explicit empty list", action.RequiredLabelsOverride)
	}
	if got := contract.EffectiveRequiredLabels(deployment, action); len(got) != 0 {
		t.Fatalf("effective required labels = %#v, want none", got)
	}
	run := NewRun("windforce", "routing-policy-test", deployment.App, "run", deployment, []byte(`{}`))
	job := NewActionJob(run, nil)
	if job.Payload.Tag != "operator-action" || job.Payload.RequiredLabels == nil || len(job.Payload.RequiredLabels) != 0 {
		t.Fatalf("pinned job routing = tag:%q labels:%#v", job.Payload.Tag, job.Payload.RequiredLabels)
	}
	policyStore, ok := store.(interface {
		GetRoutingPolicy(context.Context, string, string) (catalog.RoutingPolicy, error)
	})
	if !ok {
		t.Fatal("routing policy read is unsupported")
	}
	policy, err := policyStore.GetRoutingPolicy(context.Background(), "workspace-a", "echo")
	if err != nil {
		t.Fatal(err)
	}
	if policy.RouteTagOverride == nil || *policy.RouteTagOverride != "operator-app" ||
		policy.RequiredLabelsOverride == nil || !reflect.DeepEqual(*policy.RequiredLabelsOverride, []string{"gpu"}) {
		t.Fatalf("app routing policy = %#v", policy)
	}
}

func routingPolicyDeployment(commit string, appTag string, actionTag string, appLabels []string, actionLabels []string) contract.Deployment {
	return contract.Deployment{
		Workspace: "workspace-a", GitSourceID: "source-a", App: "echo", Commit: commit,
		Tag: appTag, RequiredLabels: appLabels,
		Actions: map[string]contract.Action{
			"run": {Action: "run", Tag: &actionTag, RunsOn: &actionLabels},
		},
	}
}
