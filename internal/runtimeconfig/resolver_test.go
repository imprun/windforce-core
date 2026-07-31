package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
)

func TestBuildAccessAndResolveRuntimeInput(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	store.SecretKey = "runtime-config-test-secret-key"
	if _, err := store.CreateWorkspace(ctx, "ws-a", "Workspace A", "test"); err != nil {
		t.Fatal(err)
	}
	secrets := secretbackend.NewDatabase(store, store.SecretKey, "")
	ciphertext, err := secrets.Store(ctx, secretbackend.Reference{
		WorkspaceID: "ws-a",
		Kind:        "variable",
		Path:        "credentials/api-key",
	}, "top-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(ctx, "ws-a", "shop", "credentials/api-key", ciphertext, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVariable(ctx, "ws-a", "", "region", "ap-northeast-2", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetResource(ctx, "ws-a", "database/base", json.RawMessage(`{"region":"$var:region"}`), "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetResource(ctx, "ws-a", "service/api", json.RawMessage(`{"token":"$var:credentials/api-key","database":"$res:database/base"}`), "", ""); err != nil {
		t.Fatal(err)
	}

	resolver := New(store, secrets)
	access, err := resolver.BuildAccess(ctx, "ws-a", "shop", contract.RuntimeAccess{}, json.RawMessage(`{"config":"$res:service/api"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(access.Variables, []string{"credentials/api-key", "region"}) {
		t.Fatalf("variables = %#v", access.Variables)
	}
	if !slices.Equal(access.Resources, []string{"database/base", "service/api"}) {
		t.Fatalf("resources = %#v", access.Resources)
	}
	job := state.Job{
		ID:      "job-a",
		Attempt: 2,
		Payload: state.JobPayload{
			Workspace:     "ws-a",
			App:           "shop",
			Action:        "sync",
			RuntimeAccess: access,
		},
	}
	resolved, secretValues, err := resolver.ResolveRuntimeInput(ctx, job, json.RawMessage(`{"config":"$res:service/api"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(resolved), "$var:") || strings.Contains(string(resolved), "$res:") {
		t.Fatalf("references remain in resolved input: %s", resolved)
	}
	if !strings.Contains(string(resolved), "top-secret-value") || !strings.Contains(string(resolved), "ap-northeast-2") {
		t.Fatalf("resolved input = %s", resolved)
	}
	if !slices.Contains(secretValues, "top-secret-value") {
		t.Fatalf("secret values = %#v", secretValues)
	}
	audits, err := store.ListSecretAccessAudits(ctx, "ws-a", "job-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) == 0 || audits[0].Path != "credentials/api-key" || audits[0].Attempt != 2 {
		t.Fatalf("secret access audits = %#v", audits)
	}
}

func TestBuildAccessRejectsResourceCycle(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SetResource(ctx, "ws-a", "a", json.RawMessage(`{"next":"$res:b"}`), "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetResource(ctx, "ws-a", "b", json.RawMessage(`{"next":"$res:a"}`), "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := New(store, nil).BuildAccess(ctx, "ws-a", "app", contract.RuntimeAccess{}, json.RawMessage(`"$res:a"`))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("BuildAccess error = %v, want cycle", err)
	}
}

func TestResolveJobVariableRejectsUnadmittedPath(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SetVariable(context.Background(), "ws-a", "app", "allowed", "value", false, ""); err != nil {
		t.Fatal(err)
	}
	job := state.Job{Payload: state.JobPayload{Workspace: "ws-a", App: "app"}}
	_, _, err := New(store, nil).ResolveJobVariable(context.Background(), job, "allowed")
	if !errors.Is(err, state.ErrForbidden) {
		t.Fatalf("ResolveJobVariable error = %v, want forbidden", err)
	}
}

type failingAuditStore struct {
	*state.LocalStore
}

func (s failingAuditStore) AppendSecretAccessAudit(context.Context, state.SecretAccessAudit) error {
	return errors.New("audit unavailable")
}

func TestSecretResolutionFailsClosedWhenAuditCannotPersist(t *testing.T) {
	ctx := context.Background()
	local := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	local.SecretKey = "runtime-config-test-secret-key"
	if _, err := local.CreateWorkspace(ctx, "ws-a", "Workspace A", "test"); err != nil {
		t.Fatal(err)
	}
	secrets := secretbackend.NewDatabase(local, local.SecretKey, "")
	ciphertext, err := secrets.Store(ctx, secretbackend.Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "token"}, "hidden")
	if err != nil {
		t.Fatal(err)
	}
	if err := local.SetVariable(ctx, "ws-a", "app", "token", ciphertext, true, ""); err != nil {
		t.Fatal(err)
	}
	store := failingAuditStore{LocalStore: local}
	job := state.Job{
		ID:      "job-a",
		Attempt: 1,
		Payload: state.JobPayload{
			Workspace: "ws-a",
			App:       "app",
			Action:    "run",
			RuntimeAccess: contract.RuntimeAccess{
				Variables: []string{"token"},
			},
		},
	}
	_, _, err = New(store, secrets).ResolveJobVariable(ctx, job, "token")
	if err == nil || !strings.Contains(err.Error(), "audit unavailable") {
		t.Fatalf("ResolveJobVariable error = %v, want audit failure", err)
	}
}
