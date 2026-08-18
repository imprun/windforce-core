package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
)

func TestResolveJobVariableActorScopeIsolatesSubjects(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "actor-runtime-config-test-secret-key"
	physicalPath, err := contract.ActorRuntimeConfigPath("account:alpha", "connections/tistory/session")
	if err != nil {
		t.Fatal(err)
	}
	secrets := secretbackend.NewDatabase(store, store.SecretKey, "")
	ciphertext, err := secrets.Store(ctx, secretbackend.Reference{
		WorkspaceID: "ws-a",
		Kind:        "variable-app",
		Path:        "publication/" + physicalPath,
	}, "alpha-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(ctx, "ws-a", "publication", physicalPath, ciphertext, true, ""); err != nil {
		t.Fatal(err)
	}
	access := contract.RuntimeAccess{VariableTargets: []contract.RuntimeConfigTarget{{
		Scope: contract.RuntimeConfigScopeActor,
		Path:  "connections/tistory/session",
	}}}
	resolver := New(store, secrets)
	access, err = resolver.BuildAccess(ctx, "ws-a", "publication", access, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	job := state.Job{ID: "job-alpha", State: state.JobRunning, Payload: state.JobPayload{
		Workspace: "ws-a", App: "publication", RuntimeAccess: access, PermissionedAs: "account:alpha",
	}}
	value, secret, err := resolver.ResolveJobVariableScoped(ctx, job, contract.RuntimeConfigScopeActor, "connections/tistory/session")
	if err != nil || !secret || value != "alpha-session" {
		t.Fatalf("actor variable = %q secret=%v err=%v", value, secret, err)
	}
	resolved, secretValues, err := resolver.ResolveRuntimeInput(ctx, job, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("prepare actor-scoped runtime input: %v", err)
	}
	if string(resolved) != `{}` || len(secretValues) != 1 || secretValues[0] != "alpha-session" {
		t.Fatalf("resolved=%s secrets=%#v", resolved, secretValues)
	}
	job.Payload.PermissionedAs = "account:beta"
	if _, _, err := resolver.ResolveJobVariableScoped(ctx, job, contract.RuntimeConfigScopeActor, "connections/tistory/session"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("other actor read error = %v, want not found", err)
	}
}
