package state

import (
	"strconv"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
)

func BenchmarkExecutionLimitPolicyEvaluation16Pins(b *testing.B) {
	candidate, policies := executionLimitPolicyBenchmarkFixture(b, "sixteen-pins", false)
	benchmarkExecutionLimitPolicyEvaluation(b, "release-only", candidate, nil, 17)
	benchmarkExecutionLimitPolicyEvaluation(b, "operator-allowances", candidate, policies, 17)
}

func BenchmarkExecutionLimitPolicyEvaluation33Shapes(b *testing.B) {
	candidate, policies := executionLimitPolicyBenchmarkFixture(b, "thirty-three-shapes", true)
	benchmarkExecutionLimitPolicyEvaluation(b, "release-only", candidate, nil, 33)
	benchmarkExecutionLimitPolicyEvaluation(b, "operator-allowances", candidate, policies, 33)
}

func executionLimitPolicyBenchmarkFixture(b *testing.B, appKey string, includeActionScope bool) (Job, []ExecutionLimitPolicy) {
	b.Helper()
	const workspaceID = "benchmark"
	deployment := contract.Deployment{
		Workspace: workspaceID,
		App:       appKey,
		Actions:   map[string]contract.Action{"run": {Action: "run"}},
	}
	run := NewRun("benchmark", "benchmark-run", appKey, "run", deployment, nil)
	policyCapacity := 16
	if includeActionScope {
		policyCapacity = 32
	}
	policies := make([]ExecutionLimitPolicy, 0, policyCapacity)
	appendScope := func(scope string) {
		actionKey := ""
		if scope == executionlimit.ScopeAction {
			actionKey = "run"
		}
		for index := 0; index < 8; index++ {
			policyID := "concurrency-" + strconv.Itoa(index)
			fingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
				WorkspaceID: workspaceID, AppKey: appKey, ActionKey: actionKey, Scope: scope,
				PolicyID: policyID, Kind: executionlimit.KindConcurrency,
				InputPointers: []string{"/account/" + strconv.Itoa(index)},
			})
			if err != nil {
				b.Fatal(err)
			}
			run.ExecutionLimits.Concurrency = append(run.ExecutionLimits.Concurrency, KeyedConcurrencyLimitPin{
				PolicyID: policyID, PolicyRevision: "benchmark", ShapeFingerprint: fingerprint,
				Scope: scope, KeyDigest: scope + "-benchmark-" + strconv.Itoa(index), MaxConcurrent: 10,
			})
			policies = append(policies, ExecutionLimitPolicy{
				ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{
					WorkspaceID: workspaceID, AppKey: appKey, ActionKey: actionKey, Scope: scope,
					PolicyID: policyID, Kind: executionlimit.KindConcurrency,
				},
				ShapeFingerprint: fingerprint, Allowance: 8,
			})
		}
		for index := 0; index < 8; index++ {
			policyID := "rate-" + strconv.Itoa(index)
			fingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
				WorkspaceID: workspaceID, AppKey: appKey, ActionKey: actionKey, Scope: scope,
				PolicyID: policyID, Kind: executionlimit.KindRate,
				InputPointers: []string{"/vendor/" + strconv.Itoa(index)}, WindowSeconds: 60,
			})
			if err != nil {
				b.Fatal(err)
			}
			run.ExecutionLimits.Rate = append(run.ExecutionLimits.Rate, KeyedRateLimitPin{
				PolicyID: policyID, PolicyRevision: "benchmark", ShapeFingerprint: fingerprint,
				Scope: scope, KeyDigest: scope + "-benchmark-" + strconv.Itoa(index),
				MaxAttempts: 100, WindowSeconds: 60,
			})
			policies = append(policies, ExecutionLimitPolicy{
				ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{
					WorkspaceID: workspaceID, AppKey: appKey, ActionKey: actionKey, Scope: scope,
					PolicyID: policyID, Kind: executionlimit.KindRate,
				},
				ShapeFingerprint: fingerprint, Allowance: 80, WindowSeconds: 60,
			})
		}
	}
	appendScope(executionlimit.ScopeApp)
	if includeActionScope {
		appendScope(executionlimit.ScopeAction)
	}
	return NewActionJob(run, nil), policies
}

func benchmarkExecutionLimitPolicyEvaluation(b *testing.B, name string, candidate Job, policies []ExecutionLimitPolicy, expectedRequirements int) {
	b.Helper()
	lookup := executionLimitPolicyLookupFromSlice(policies)
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		for iteration := 0; iteration < b.N; iteration++ {
			if got := len(candidateExecutionPolicyRequirements(candidate)); got != expectedRequirements {
				b.Fatalf("expected %d execution-limit requirements, got %d", expectedRequirements, got)
			}
			total := 0
			for _, pin := range candidate.Payload.ExecutionLimits.Concurrency {
				limit, valid := effectiveKeyedConcurrencyLimit(candidate, pin, lookup)
				if !valid {
					b.Fatal("invalid concurrency pin")
				}
				total += int(limit)
			}
			for _, pin := range candidate.Payload.ExecutionLimits.Rate {
				limit, valid := effectiveKeyedRateLimit(candidate, pin, lookup)
				if !valid {
					b.Fatal("invalid rate pin")
				}
				total += int(limit)
			}
			if total == 0 {
				b.Fatal("compiler-elided policy evaluation")
			}
		}
	})
}
