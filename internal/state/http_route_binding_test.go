package state

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestLocalHTTPRouteBindingStoreContract(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-http-route-binding-key"
	exerciseHTTPRouteBindingStore(t, store, "routes-local")
}

func TestPostgresHTTPRouteBindingStoreContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	store.ConfigureInputCrypto("postgres-http-route-binding-key", "")
	exerciseHTTPRouteBindingStore(t, store, "routes-postgres")
}

func exerciseHTTPRouteBindingStore(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	trigger, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: workspaceID,
		Name:        "incoming",
		Kind:        "webhook",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{}`),
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: workspaceID,
		Name:        "nightly",
		Kind:        "schedule",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{"cron":"0 0 * * *"}`),
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateHTTPRouteBinding(ctx, HTTPRouteBinding{
		WorkspaceID: workspaceID,
		TriggerID:   schedule.ID,
		Path:        "/nightly",
	}, "creator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("schedule route error = %v", err)
	}

	created, err := store.CreateHTTPRouteBinding(ctx, HTTPRouteBinding{
		WorkspaceID: workspaceID,
		TriggerID:   trigger.ID,
		Hostname:    "Hooks.Example.COM",
		Path:        "/gale/events",
		Visibility:  "public",
		Provider:    "auto",
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	if created.State != HTTPRouteBindingPending ||
		created.Generation != 1 ||
		created.ObservedGeneration != 0 ||
		created.Hostname != "hooks.example.com" {
		t.Fatalf("created binding = %#v", created)
	}
	if _, err := store.CreateHTTPRouteBinding(ctx, HTTPRouteBinding{
		WorkspaceID: workspaceID,
		TriggerID:   trigger.ID,
		Hostname:    "hooks.example.com",
		Path:        "/gale/events",
	}, "creator"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate address error = %v", err)
	}

	created.Path = "/gale/webhook"
	updated, err := store.UpdateHTTPRouteBinding(ctx, created, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != 2 || updated.State != HTTPRouteBindingPending {
		t.Fatalf("updated binding = %#v", updated)
	}
	if _, err := store.UpdateHTTPRouteBindingStatus(ctx, workspaceID, updated.ID, HTTPRouteBindingStatus{
		State:              HTTPRouteBindingReady,
		PublicURL:          "https://hooks.example.com/gale/events",
		ObservedGeneration: 1,
	}, "provider:test"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale ready error = %v", err)
	}
	failed, err := store.UpdateHTTPRouteBindingStatus(ctx, workspaceID, updated.ID, HTTPRouteBindingStatus{
		State:              HTTPRouteBindingError,
		ErrorSummary:       "temporary provider failure",
		ObservedGeneration: updated.Generation,
	}, "provider:test")
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != HTTPRouteBindingError || failed.ErrorSummary == "" {
		t.Fatalf("failed binding = %#v", failed)
	}
	ready, err := store.UpdateHTTPRouteBindingStatus(ctx, workspaceID, updated.ID, HTTPRouteBindingStatus{
		State:              HTTPRouteBindingReady,
		PublicURL:          "https://hooks.example.com/gale/webhook",
		ObservedGeneration: updated.Generation,
	}, "provider:test")
	if err != nil {
		t.Fatal(err)
	}
	if ready.State != HTTPRouteBindingReady || ready.PublicURL == "" {
		t.Fatalf("ready binding = %#v", ready)
	}

	deleting, err := store.RequestDeleteHTTPRouteBinding(ctx, workspaceID, trigger.ID, ready.ID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if deleting.State != HTTPRouteBindingDeleting ||
		deleting.Generation != ready.Generation+1 ||
		deleting.DeleteRequestedAt == nil {
		t.Fatalf("deleting binding = %#v", deleting)
	}
	deletingAgain, err := store.RequestDeleteHTTPRouteBinding(ctx, workspaceID, trigger.ID, ready.ID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if deletingAgain.Generation != deleting.Generation || deletingAgain.State != HTTPRouteBindingDeleting {
		t.Fatalf("idempotent delete = %#v", deletingAgain)
	}
	deleted, err := store.UpdateHTTPRouteBindingStatus(ctx, workspaceID, deleting.ID, HTTPRouteBindingStatus{
		State:              HTTPRouteBindingDeleted,
		ObservedGeneration: deleting.Generation,
	}, "provider:test")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil || deleted.PublicURL != "" {
		t.Fatalf("deleted binding = %#v", deleted)
	}
	active, err := store.ListHTTPRouteBindings(ctx, workspaceID, trigger.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active bindings = %#v", active)
	}
	all, err := store.ListHTTPRouteBindings(ctx, workspaceID, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != created.ID {
		t.Fatalf("all bindings = %#v", all)
	}
	audit, err := store.ListHTTPRouteBindingAudit(ctx, workspaceID, trigger.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 6 ||
		audit[0].Kind != "created" ||
		audit[1].Kind != "updated" ||
		audit[2].Kind != "status_changed" ||
		audit[3].Kind != "status_changed" ||
		audit[4].Kind != "delete_requested" ||
		audit[5].Kind != "status_changed" {
		t.Fatalf("binding audit = %#v", audit)
	}
}

func TestLocalTriggerDeleteRequestsHTTPRouteDeletion(t *testing.T) {
	ctx := context.Background()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "test-http-route-binding-key"
	trigger, err := store.CreateTrigger(ctx, TriggerDefinition{
		WorkspaceID: "cascade",
		Name:        "incoming",
		Kind:        "webhook",
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{}`),
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.CreateHTTPRouteBinding(ctx, HTTPRouteBinding{
		WorkspaceID: "cascade",
		TriggerID:   trigger.ID,
		Path:        "/incoming",
	}, "creator")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTrigger(ctx, "cascade", trigger.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTrigger(ctx, "cascade", trigger.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted trigger error = %v", err)
	}
	items, err := store.ListHTTPRouteBindings(ctx, "cascade", trigger.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 ||
		items[0].ID != binding.ID ||
		items[0].State != HTTPRouteBindingDeleting ||
		items[0].DeleteRequestedAt == nil {
		t.Fatalf("bindings after trigger delete = %#v", items)
	}
}
