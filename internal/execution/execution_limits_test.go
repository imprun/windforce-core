package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
	"github.com/imprun/windforce-core/internal/state"
)

func TestResolveExecutionLimitPinsUsesOpaqueWorkspaceScopedDigests(t *testing.T) {
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("execution-limit-test-secret", "")
	deployment := contract.Deployment{
		App: "orders",
		ExecutionLimits: contract.ExecutionLimits{
			Concurrency: []contract.KeyedConcurrencyLimit{{
				ID: "account-egress", MaxConcurrent: 2, InputPointers: []string{"/account/id", "/egress~1name"},
			}},
			Rate: []contract.KeyedRateLimit{{
				ID: "vendor-api", MaxAttempts: 10, WindowSeconds: 60, InputPointers: []string{"/account/id"},
			}},
		},
	}
	action := contract.Action{Action: "collect", ExecutionLimits: contract.ExecutionLimits{
		Concurrency: []contract.KeyedConcurrencyLimit{{
			ID: "client", MaxConcurrent: 1, InputPointers: []string{"/client"},
		}},
		Rate: []contract.KeyedRateLimit{{
			ID: "client-api", MaxAttempts: 2, WindowSeconds: 5, InputPointers: []string{"/client"},
		}},
	}}
	input := json.RawMessage(`{"account":{"id":42},"egress/name":"kr-seoul","client":"tenant-a"}`)

	first, err := resolveExecutionLimitPins(context.Background(), store, "workspace-a", "orders", "collect", deployment, action, input)
	if err != nil {
		t.Fatalf("resolve limits: %v", err)
	}
	second, err := resolveExecutionLimitPins(context.Background(), store, "workspace-a", "orders", "collect", deployment, action,
		json.RawMessage(`{"account":{"id":42.0},"egress/name":"kr-seoul","client":"tenant-a"}`))
	if err != nil {
		t.Fatalf("resolve equivalent numeric key: %v", err)
	}
	otherWorkspace, err := resolveExecutionLimitPins(context.Background(), store, "workspace-b", "orders", "collect", deployment, action, input)
	if err != nil {
		t.Fatalf("resolve other workspace: %v", err)
	}
	if len(first.Concurrency) != 2 {
		t.Fatalf("pins = %#v, want app and action pins", first.Concurrency)
	}
	if first.AppConcurrency == nil || !executionlimit.IsFingerprint(first.AppConcurrency.ShapeFingerprint) || first.AppConcurrency.MaxConcurrent != nil {
		t.Fatalf("implicit app concurrency pin = %#v", first.AppConcurrency)
	}
	if first.AppConcurrency.ShapeFingerprint == otherWorkspace.AppConcurrency.ShapeFingerprint {
		t.Fatal("implicit app concurrency shape must be workspace scoped")
	}
	for _, pin := range first.Concurrency {
		if !executionlimit.IsFingerprint(pin.ShapeFingerprint) {
			t.Fatalf("concurrency pin lacks shape fingerprint: %#v", pin)
		}
	}
	for _, pin := range first.Rate {
		if !executionlimit.IsFingerprint(pin.ShapeFingerprint) {
			t.Fatalf("rate pin lacks shape fingerprint: %#v", pin)
		}
	}
	if first.Concurrency[0].Scope != state.ExecutionLimitScopeApp || first.Concurrency[1].Scope != state.ExecutionLimitScopeAction {
		t.Fatalf("pin scopes = %#v", first.Concurrency)
	}
	if first.Concurrency[0].KeyDigest != second.Concurrency[0].KeyDigest {
		t.Fatalf("semantic numeric equivalents produced different digests: %q != %q", first.Concurrency[0].KeyDigest, second.Concurrency[0].KeyDigest)
	}
	if first.Concurrency[0].KeyDigest == otherWorkspace.Concurrency[0].KeyDigest {
		t.Fatal("workspace-scoped digests unexpectedly match")
	}
	if len(first.Rate) != 2 || first.Rate[0].Scope != state.ExecutionLimitScopeApp || first.Rate[1].Scope != state.ExecutionLimitScopeAction {
		t.Fatalf("rate pins = %#v, want app and action pins", first.Rate)
	}
	if first.Rate[0].KeyDigest != second.Rate[0].KeyDigest {
		t.Fatalf("semantic numeric equivalents produced different rate digests: %q != %q", first.Rate[0].KeyDigest, second.Rate[0].KeyDigest)
	}
	if first.Rate[0].KeyDigest == otherWorkspace.Rate[0].KeyDigest || first.Rate[0].KeyDigest == first.Concurrency[0].KeyDigest {
		t.Fatal("rate digest namespace is not isolated by workspace and limit kind")
	}
	updatedDeployment := deployment
	updatedDeployment.ExecutionLimits = contract.ExecutionLimits{
		Concurrency: append([]contract.KeyedConcurrencyLimit(nil), deployment.ExecutionLimits.Concurrency...),
		Rate:        append([]contract.KeyedRateLimit(nil), deployment.ExecutionLimits.Rate...),
	}
	updatedDeployment.ExecutionLimits.Rate[0].MaxAttempts = 11
	updated, err := resolveExecutionLimitPins(context.Background(), store, "workspace-a", "orders", "collect", updatedDeployment, action, input)
	if err != nil {
		t.Fatalf("resolve updated rate limit: %v", err)
	}
	if updated.Rate[0].KeyDigest != first.Rate[0].KeyDigest || updated.Rate[0].PolicyRevision == first.Rate[0].PolicyRevision {
		t.Fatalf("updated rate pin = %#v, want stable key digest and changed policy revision", updated.Rate[0])
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"tenant-a", "kr-seoul", "/account/id", "/client"} {
		if strings.Contains(string(encoded), raw) {
			t.Fatalf("persisted pins leak %q: %s", raw, encoded)
		}
	}
}

func TestResolveRateLimitPinsRejectsMissingOrCompositeValues(t *testing.T) {
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("execution-limit-test-secret", "")
	deployment := contract.Deployment{App: "orders", ExecutionLimits: contract.ExecutionLimits{Rate: []contract.KeyedRateLimit{{
		ID: "account-rate", MaxAttempts: 10, WindowSeconds: 60, InputPointers: []string{"/account"},
	}}}}

	for name, input := range map[string]json.RawMessage{
		"missing":   json.RawMessage(`{}`),
		"composite": json.RawMessage(`{"account":{"id":"tenant-a"}}`),
		"null":      json.RawMessage(`{"account":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveExecutionLimitPins(context.Background(), store, "workspace-a", "orders", "collect", deployment, contract.Action{}, input)
			var inputError *executionLimitInputError
			if !errors.As(err, &inputError) {
				t.Fatalf("error = %T %v, want executionLimitInputError", err, err)
			}
		})
	}
}

func TestResolveExecutionLimitPinsRejectsMissingOrCompositeValues(t *testing.T) {
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("execution-limit-test-secret", "")
	deployment := contract.Deployment{App: "orders", ExecutionLimits: contract.ExecutionLimits{Concurrency: []contract.KeyedConcurrencyLimit{{
		ID: "account", MaxConcurrent: 1, InputPointers: []string{"/account"},
	}}}}

	for name, input := range map[string]json.RawMessage{
		"missing":   json.RawMessage(`{}`),
		"composite": json.RawMessage(`{"account":{"id":"tenant-a"}}`),
		"null":      json.RawMessage(`{"account":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := resolveExecutionLimitPins(context.Background(), store, "workspace-a", "orders", "collect", deployment, contract.Action{}, input)
			var inputError *executionLimitInputError
			if !errors.As(err, &inputError) {
				t.Fatalf("error = %T %v, want executionLimitInputError", err, err)
			}
		})
	}
}

func TestResolveExecutionLimitPinsDoesNotRequireDigesterWithoutLimits(t *testing.T) {
	pins, err := resolveExecutionLimitPins(context.Background(), nil, "workspace-a", "orders", "collect", contract.Deployment{}, contract.Action{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("resolve empty limits: %v", err)
	}
	if len(pins.Concurrency) != 0 || len(pins.Rate) != 0 || pins.AppConcurrency == nil || !executionlimit.IsFingerprint(pins.AppConcurrency.ShapeFingerprint) {
		t.Fatalf("pins = %#v, want only the implicit app concurrency shape", pins)
	}
}
