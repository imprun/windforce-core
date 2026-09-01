package execution

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

func TestResolvedInputRequiresServicePrincipalBeforeRunCreation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	admission := NewAdmissionService(store, store, nil)

	for _, test := range []struct {
		name      string
		principal Principal
	}{
		{
			name: "client principal",
			principal: Principal{
				Kind:      PrincipalClient,
				ID:        "client-untrusted",
				Workspace: "default",
				Scopes:    []Scope{ScopeRunsCreate},
			},
		},
		{name: "principal absent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := admission.CreateRun(ctx, CreateRunRequest{
				Workspace:           "default",
				App:                 "resolved_input_test",
				Action:              "invoke",
				Input:               json.RawMessage(`{"exact":true}`),
				InputConfigResolved: true,
				Principal:           test.principal,
			})
			var fault *Fault
			if !errors.As(err, &fault) || fault.Kind != FaultForbidden {
				t.Fatalf("CreateRun error = %v, want forbidden", err)
			}
		})
	}

	snapshot, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(snapshot.Runs) != 0 || len(snapshot.Jobs) != 0 {
		t.Fatalf("untrusted resolved input created Runs=%d Jobs=%d, want zero", len(snapshot.Runs), len(snapshot.Jobs))
	}
}

func TestOrdinaryAdmissionStillResolvesInputConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.SetInputConfig(ctx, state.InputConfig{
		WorkspaceID: "default",
		AppKey:      "resolved_input_test",
		ActionKey:   "invoke",
		Config:      json.RawMessage(`{"configured":"default"}`),
	}, "test-operator"); err != nil {
		t.Fatalf("set input config: %v", err)
	}
	if _, err := store.PublishRelease(ctx, contract.Deployment{
		Workspace:    "default",
		GitSourceID:  "resolved-input-source",
		APIVersion:   contract.AppManifestV2,
		App:          "resolved_input_test",
		Commit:       "resolved-input-commit",
		BundleDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ObjectURI:    "bundle://resolved-input/source/commit",
		Actions: map[string]contract.Action{
			"invoke": {Action: "invoke"},
		},
	}, time.Now().UTC()); err != nil {
		t.Fatalf("publish release: %v", err)
	}

	admitted, err := NewAdmissionService(store, store, nil).CreateRun(ctx, CreateRunRequest{
		Workspace: "default",
		App:       "resolved_input_test",
		Action:    "invoke",
		Input:     json.RawMessage(`{"request":"value"}`),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	for name, raw := range map[string]json.RawMessage{
		"run": admitted.Run.Input,
		"job": admitted.Job.Payload.Input,
	} {
		var input map[string]any
		if err := json.Unmarshal(raw, &input); err != nil {
			t.Fatalf("decode %s input: %v", name, err)
		}
		if input["configured"] != "default" || input["request"] != "value" {
			t.Fatalf("%s input = %#v, want configured default plus request value", name, input)
		}
	}
}
