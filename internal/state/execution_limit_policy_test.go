package state

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
)

func TestLocalExecutionLimitPolicyStore(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	exerciseExecutionLimitPolicyStore(t, store, "local-policy")
	exerciseExecutionLimitPolicyClaimGate(t, store, "local-claim-policy")
}

func TestExecutionLimitResidualTracksMaximumPerOpaqueKey(t *testing.T) {
	fingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
		WorkspaceID: "default", AppKey: "keyed-drain", Scope: executionlimit.ScopeApp,
		PolicyID: "account", Kind: executionlimit.KindConcurrency, InputPointers: []string{"/account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := func(id string, digest string) Job {
		return Job{ID: id, State: JobRunning, Payload: JobPayload{
			Workspace: "default", App: "keyed-drain", Action: "run",
			ExecutionLimits: ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{{
				PolicyID: "account", PolicyRevision: "release", ShapeFingerprint: fingerprint,
				Scope: executionlimit.ScopeApp, KeyDigest: digest, MaxConcurrent: 3,
			}}},
		}}
	}
	residuals := buildExecutionLimitResiduals([]Job{job("one", "digest-a"), job("two", "digest-b")})
	keyed := findResidualByPolicyID(t, residuals, "account")
	if keyed.Running != 2 || keyed.MaxRunningForKey != 1 {
		t.Fatalf("different-key residual = %#v", keyed)
	}
	residuals = buildExecutionLimitResiduals(append([]Job{job("one", "digest-a"), job("two", "digest-b")}, job("three", "digest-a")))
	keyed = findResidualByPolicyID(t, residuals, "account")
	if keyed.Running != 3 || keyed.MaxRunningForKey != 2 {
		t.Fatalf("same-key residual = %#v", keyed)
	}
}

func findResidualByPolicyID(t *testing.T, residuals []ExecutionLimitResidual, policyID string) ExecutionLimitResidual {
	t.Helper()
	for _, residual := range residuals {
		if residual.PolicyID == policyID {
			return residual
		}
	}
	t.Fatalf("policy %q residual not found in %#v", policyID, residuals)
	return ExecutionLimitResidual{}
}

func TestPostgresExecutionLimitPolicyStore(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	workspaceID := "test-limit-policy-" + time.Now().UTC().Format("20060102150405.000000000")
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM execution_rate_bucket WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM human_tasks WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM jobs WHERE COALESCE(NULLIF(payload->>'workspace', ''), 'default')=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM runs WHERE deployment->>'workspace'=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM execution_limit_policy_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM execution_limit_policy WHERE workspace_id=$1`, workspaceID)
	}()
	exerciseExecutionLimitPolicyStore(t, store, workspaceID)
	exerciseExecutionLimitPolicyClaimGate(t, store, workspaceID)
}

func TestPostgresExecutionLimitPolicyCommitOrder(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	workspaceID := "test-limit-policy-order-" + time.Now().UTC().Format("20060102150405.000000000")
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM jobs WHERE COALESCE(NULLIF(payload->>'workspace', ''), 'default')=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM runs WHERE deployment->>'workspace'=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM execution_limit_policy_audit WHERE workspace_id=$1`, workspaceID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM execution_limit_policy WHERE workspace_id=$1`, workspaceID)
	}()

	appKey := "commit-order"
	fingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, appKey)
	if err != nil {
		t.Fatal(err)
	}
	key := ExecutionLimitPolicyKey{
		WorkspaceID: workspaceID, AppKey: appKey, Scope: executionlimit.ScopeApp,
		PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency,
	}
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, MutateExecutionLimitPolicyRequest{
		Policy: ExecutionLimitPolicy{
			ExecutionLimitPolicyKey: key, ShapeFingerprint: fingerprint, Allowance: 2,
		},
		ExpectedRevision: 0, OperationID: "commit-order-create",
		RequestFingerprint: "commit-order-create-request", Actor: "test",
	}); err != nil {
		t.Fatal(err)
	}
	pins := ExecutionLimitPins{AppConcurrency: &AppConcurrencyLimitPin{
		PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: fingerprint,
	}}
	for _, suffix := range []string{"one", "two"} {
		deployment := contract.Deployment{Workspace: workspaceID, App: appKey, Actions: map[string]contract.Action{"run": {Action: "run"}}}
		run := NewRun("api", "commit-order-"+suffix, appKey, "run", deployment, nil)
		run.ExecutionLimits = cloneExecutionLimitPins(pins)
		job := NewActionJob(run, nil)
		job.ID = "job-commit-order-" + suffix
		if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.ClaimJob(ctx, "commit-order-worker-one", time.Minute); err != nil {
		t.Fatalf("claim first job: %v", err)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, workspaceID, executionLimitPolicyLockKey(key)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_limit_policy SET allowance=1, revision=2, updated_at=now() WHERE workspace_id=$1 AND app_key=$2 AND action_key='' AND scope=$3 AND policy_id=$4 AND kind=$5`, workspaceID, appKey, key.Scope, key.PolicyID, key.Kind); err != nil {
		t.Fatal(err)
	}

	claimResult := make(chan error, 1)
	go func() {
		_, _, claimErr := store.ClaimJob(context.Background(), "commit-order-worker-two", time.Minute)
		claimResult <- claimErr
	}()
	select {
	case claimErr := <-claimResult:
		t.Fatalf("claim returned before the policy update committed: %v", claimErr)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case claimErr := <-claimResult:
		if !errors.Is(claimErr, ErrNoQueuedJob) {
			t.Fatalf("claim after committed tightening err=%v, want ErrNoQueuedJob", claimErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not resume after the policy update committed")
	}
	queued, _, ok, err := store.GetJob(ctx, workspaceID, "job-commit-order-two")
	if err != nil || !ok || queued.State != JobQueued {
		t.Fatalf("second job after tightening = %#v, exists=%v, err=%v", queued, ok, err)
	}
}

func exerciseExecutionLimitPolicyClaimGate(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	createJob := func(runID string, appKey string, pins ExecutionLimitPins) {
		deployment := contract.Deployment{
			Workspace: workspaceID,
			App:       appKey,
			Actions:   map[string]contract.Action{"run": {Action: "run"}},
		}
		run := NewRun("api", runID, appKey, "run", deployment, nil)
		run.ExecutionLimits = cloneExecutionLimitPins(pins)
		job := NewActionJob(run, nil)
		job.ID = "job_" + runID
		if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
			t.Fatalf("enqueue %s: %v", runID, err)
		}
	}
	mutate := func(policy ExecutionLimitPolicy, expected int64, allowance int32, operationID string) ExecutionLimitPolicy {
		policy.Allowance = allowance
		result, replayed, err := store.MutateExecutionLimitPolicy(ctx, MutateExecutionLimitPolicyRequest{
			Policy: policy, ExpectedRevision: expected, OperationID: operationID,
			RequestFingerprint: "request-" + operationID, Actor: "operator",
		})
		if err != nil || replayed {
			t.Fatalf("mutate %s = %#v, replayed=%v, err=%v", operationID, result, replayed, err)
		}
		return result
	}

	appKey := "implicit-app"
	appFingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, appKey)
	if err != nil {
		t.Fatal(err)
	}
	appPolicy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: appKey, Scope: executionlimit.ScopeApp, PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        appFingerprint,
	}
	mutate(appPolicy, 0, 1, "op-app-one")
	appPins := ExecutionLimitPins{AppConcurrency: &AppConcurrencyLimitPin{PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: appFingerprint}}
	createJob("implicit-one", appKey, appPins)
	createJob("implicit-two", appKey, appPins)
	if _, _, err := store.ClaimJob(ctx, "worker-one", time.Minute); err != nil {
		t.Fatalf("claim first implicit job: %v", err)
	}
	if _, _, err := store.ClaimJob(ctx, "worker-two", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("second implicit claim err=%v, want ErrNoQueuedJob", err)
	}
	mutate(appPolicy, 1, 2, "op-app-two")
	if _, _, err := store.ClaimJob(ctx, "worker-two", time.Minute); err != nil {
		t.Fatalf("claim after allowance increase: %v", err)
	}

	keyedApp := "keyed-app"
	keyedFingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
		WorkspaceID: workspaceID, AppKey: keyedApp, Scope: executionlimit.ScopeApp,
		PolicyID: "account", Kind: executionlimit.KindConcurrency, InputPointers: []string{"/account"},
	})
	if err != nil {
		t.Fatal(err)
	}
	keyedPolicy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: keyedApp, Scope: executionlimit.ScopeApp, PolicyID: "account", Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        keyedFingerprint,
	}
	mutate(keyedPolicy, 0, 1, "op-keyed-one")
	keyedPin := KeyedConcurrencyLimitPin{
		PolicyID: "account", PolicyRevision: "sha256:" + strings.Repeat("1", 64), ShapeFingerprint: keyedFingerprint,
		Scope: executionlimit.ScopeApp, KeyDigest: "hmac-sha256:" + strings.Repeat("2", 64), MaxConcurrent: 3,
	}
	createJob("keyed-one", keyedApp, ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{keyedPin}})
	createJob("keyed-two", keyedApp, ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{keyedPin}})
	if _, _, err := store.ClaimJob(ctx, "worker-keyed-one", time.Minute); err != nil {
		t.Fatalf("claim first keyed job: %v", err)
	}
	if _, _, err := store.ClaimJob(ctx, "worker-keyed-two", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("second keyed claim err=%v, want ErrNoQueuedJob", err)
	}

	rateApp := "rate-app"
	rateFingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
		WorkspaceID: workspaceID, AppKey: rateApp, Scope: executionlimit.ScopeApp,
		PolicyID: "vendor", Kind: executionlimit.KindRate, InputPointers: []string{"/account"}, WindowSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	ratePolicy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: rateApp, Scope: executionlimit.ScopeApp, PolicyID: "vendor", Kind: executionlimit.KindRate},
		ShapeFingerprint:        rateFingerprint, WindowSeconds: 60,
	}
	mutate(ratePolicy, 0, 2, "op-rate-two")
	ratePin := KeyedRateLimitPin{
		PolicyID: "vendor", PolicyRevision: "sha256:" + strings.Repeat("3", 64), ShapeFingerprint: rateFingerprint,
		Scope: executionlimit.ScopeApp, KeyDigest: "hmac-sha256:" + strings.Repeat("4", 64), MaxAttempts: 10, WindowSeconds: 60,
	}
	createJob("rate-one", rateApp, ExecutionLimitPins{Rate: []KeyedRateLimitPin{ratePin}})
	createJob("rate-two", rateApp, ExecutionLimitPins{Rate: []KeyedRateLimitPin{ratePin}})
	createJob("rate-three", rateApp, ExecutionLimitPins{Rate: []KeyedRateLimitPin{ratePin}})
	if _, _, err := store.ClaimJob(ctx, "worker-rate-one", time.Minute); err != nil {
		t.Fatalf("claim first rate job: %v", err)
	}
	if _, _, err := store.ClaimJob(ctx, "worker-rate-two", time.Minute); err != nil {
		t.Fatalf("claim second rate job: %v", err)
	}
	mutate(ratePolicy, 1, 1, "op-rate-one")
	if _, _, err := store.ClaimJob(ctx, "worker-rate-three", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("claim after rate tightening err=%v, want ErrNoQueuedJob", err)
	}

	retryApp := "retry-policy"
	retryFingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, retryApp)
	if err != nil {
		t.Fatal(err)
	}
	retryPolicy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: retryApp, Scope: executionlimit.ScopeApp, PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        retryFingerprint,
	}
	mutate(retryPolicy, 0, 2, "op-retry-two")
	retryPins := ExecutionLimitPins{AppConcurrency: &AppConcurrencyLimitPin{PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: retryFingerprint}}
	createJob("retry-original", retryApp, retryPins)
	retryOriginal, retryLease, err := store.ClaimJob(ctx, "worker-retry-original", time.Minute)
	if err != nil {
		t.Fatalf("claim retry original: %v", err)
	}
	if err := store.CompleteJobFailed(ctx, retryLease, contract.JobResult{JobID: retryOriginal.ID, App: retryApp, Action: "run", Error: "retry test"}); err != nil {
		t.Fatalf("fail retry original: %v", err)
	}
	createJob("retry-blocker", retryApp, retryPins)
	if _, _, err := store.ClaimJob(ctx, "worker-retry-blocker", time.Minute); err != nil {
		t.Fatalf("claim retry blocker: %v", err)
	}
	_, retryJob, err := store.RetryRun(ctx, "retry-original")
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if retryJob.Payload.ExecutionLimits.AppConcurrency == nil || retryJob.Payload.ExecutionLimits.AppConcurrency.ShapeFingerprint != retryFingerprint {
		t.Fatalf("retry policy pin = %#v", retryJob.Payload.ExecutionLimits)
	}
	mutate(retryPolicy, 1, 1, "op-retry-one")
	if _, _, err := store.ClaimJob(ctx, "worker-retry-next", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("retried claim after tightening err=%v, want ErrNoQueuedJob", err)
	}

	resumeApp := "resume-policy"
	resumeFingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, resumeApp)
	if err != nil {
		t.Fatal(err)
	}
	resumePolicy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: resumeApp, Scope: executionlimit.ScopeApp, PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        resumeFingerprint,
	}
	mutate(resumePolicy, 0, 2, "op-resume-two")
	resumePins := ExecutionLimitPins{AppConcurrency: &AppConcurrencyLimitPin{PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: resumeFingerprint}}
	createJob("resume-original", resumeApp, resumePins)
	resumeOriginal, resumeLease, err := store.ClaimJob(ctx, "worker-resume-original", time.Minute)
	if err != nil {
		t.Fatalf("claim resume original: %v", err)
	}
	if err := store.CompleteJobWaitingHuman(ctx, resumeLease, contract.JobResult{
		JobID: resumeOriginal.ID, App: resumeApp, Action: "run", Output: []byte(`{"$windforce":{"type":"human_task"}}`),
	}, HumanTask{ID: "human-" + workspaceID, WorkspaceID: workspaceID, RunID: "resume-original", Title: "Resume test"}); err != nil {
		t.Fatalf("suspend resume original: %v", err)
	}
	createJob("resume-blocker", resumeApp, resumePins)
	if _, _, err := store.ClaimJob(ctx, "worker-resume-blocker", time.Minute); err != nil {
		t.Fatalf("claim resume blocker: %v", err)
	}
	_, resumeJob, err := store.ResumeRun(ctx, "resume-original", []byte(`{"approved":true}`))
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if resumeJob.Payload.ExecutionLimits.AppConcurrency == nil || resumeJob.Payload.ExecutionLimits.AppConcurrency.ShapeFingerprint != resumeFingerprint {
		t.Fatalf("resume policy pin = %#v", resumeJob.Payload.ExecutionLimits)
	}
	mutate(resumePolicy, 1, 1, "op-resume-one")
	if _, _, err := store.ClaimJob(ctx, "worker-resume-next", time.Minute); !errors.Is(err, ErrNoQueuedJob) {
		t.Fatalf("resumed claim after tightening err=%v, want ErrNoQueuedJob", err)
	}

	cohortApp := "rollback-cohorts"
	oldShape, err := executionlimit.Fingerprint(executionlimit.Shape{WorkspaceID: workspaceID, AppKey: cohortApp, Scope: executionlimit.ScopeApp, PolicyID: "account", Kind: executionlimit.KindConcurrency, InputPointers: []string{"/old"}})
	if err != nil {
		t.Fatal(err)
	}
	newShape, err := executionlimit.Fingerprint(executionlimit.Shape{WorkspaceID: workspaceID, AppKey: cohortApp, Scope: executionlimit.ScopeApp, PolicyID: "account", Kind: executionlimit.KindConcurrency, InputPointers: []string{"/new"}})
	if err != nil {
		t.Fatal(err)
	}
	mutate(ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: cohortApp, Scope: executionlimit.ScopeApp, PolicyID: "account", Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        oldShape,
	}, 0, 1, "op-cohort-old")
	cohortPin := func(shape string) KeyedConcurrencyLimitPin {
		return KeyedConcurrencyLimitPin{
			PolicyID: "account", PolicyRevision: "sha256:" + strings.Repeat("5", 64), ShapeFingerprint: shape,
			Scope: executionlimit.ScopeApp, KeyDigest: "hmac-sha256:" + strings.Repeat("6", 64), MaxConcurrent: 1,
		}
	}
	createJob("cohort-old", cohortApp, ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{cohortPin(oldShape)}})
	if _, _, err := store.ClaimJob(ctx, "worker-cohort-old", time.Minute); err != nil {
		t.Fatalf("claim old rollback cohort: %v", err)
	}
	createJob("cohort-new", cohortApp, ExecutionLimitPins{Concurrency: []KeyedConcurrencyLimitPin{cohortPin(newShape)}})
	if _, _, err := store.ClaimJob(ctx, "worker-cohort-new", time.Minute); err != nil {
		t.Fatalf("new shape was blocked by old running cohort: %v", err)
	}

	rateCohortApp := "rollback-rate-cohorts"
	oldRateShape, err := executionlimit.Fingerprint(executionlimit.Shape{WorkspaceID: workspaceID, AppKey: rateCohortApp, Scope: executionlimit.ScopeApp, PolicyID: "vendor", Kind: executionlimit.KindRate, InputPointers: []string{"/old"}, WindowSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	newRateShape, err := executionlimit.Fingerprint(executionlimit.Shape{WorkspaceID: workspaceID, AppKey: rateCohortApp, Scope: executionlimit.ScopeApp, PolicyID: "vendor", Kind: executionlimit.KindRate, InputPointers: []string{"/new"}, WindowSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	mutate(ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: rateCohortApp, Scope: executionlimit.ScopeApp, PolicyID: "vendor", Kind: executionlimit.KindRate},
		ShapeFingerprint:        oldRateShape, WindowSeconds: 60,
	}, 0, 1, "op-rate-cohort-old")
	rateCohortPin := func(shape string) KeyedRateLimitPin {
		return KeyedRateLimitPin{
			PolicyID: "vendor", PolicyRevision: "sha256:" + strings.Repeat("7", 64), ShapeFingerprint: shape,
			Scope: executionlimit.ScopeApp, KeyDigest: "hmac-sha256:" + strings.Repeat("8", 64), MaxAttempts: 1, WindowSeconds: 60,
		}
	}
	createJob("rate-cohort-old", rateCohortApp, ExecutionLimitPins{Rate: []KeyedRateLimitPin{rateCohortPin(oldRateShape)}})
	if _, _, err := store.ClaimJob(ctx, "worker-rate-cohort-old", time.Minute); err != nil {
		t.Fatalf("claim old rate cohort: %v", err)
	}
	createJob("rate-cohort-new", rateCohortApp, ExecutionLimitPins{Rate: []KeyedRateLimitPin{rateCohortPin(newRateShape)}})
	if _, _, err := store.ClaimJob(ctx, "worker-rate-cohort-new", time.Minute); err != nil {
		t.Fatalf("new rate shape reused old bucket: %v", err)
	}

	demandApp := "policy-demand"
	demandFingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, demandApp)
	if err != nil {
		t.Fatal(err)
	}
	mutate(ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: demandApp, Scope: executionlimit.ScopeApp, PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency},
		ShapeFingerprint:        demandFingerprint,
	}, 0, 1, "op-demand-one")
	demandPins := ExecutionLimitPins{AppConcurrency: &AppConcurrencyLimitPin{PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, ShapeFingerprint: demandFingerprint}}
	createJob("demand-one", demandApp, demandPins)
	createJob("demand-two", demandApp, demandPins)
	demand, err := store.QueueDemandSnapshot(ctx, []QueueDemandSelector{{Key: "all", WorkspaceID: workspaceID}})
	if err != nil || len(demand.Items) != 1 || demand.Items[0].Eligible != 1 || demand.Items[0].Queued != 1 {
		t.Fatalf("policy queue demand = %#v err=%v", demand, err)
	}
	if _, _, err := store.ClaimJob(ctx, "worker-demand", time.Minute); err != nil {
		t.Fatalf("claim demand cohort: %v", err)
	}
	demand, err = store.QueueDemandSnapshot(ctx, []QueueDemandSelector{{Key: "all", WorkspaceID: workspaceID}})
	if err != nil || demand.Items[0].Eligible != 0 || demand.Items[0].Claimed < 1 {
		t.Fatalf("policy queue demand after claim = %#v err=%v", demand, err)
	}
}

func exerciseExecutionLimitPolicyStore(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	fingerprint, err := executionlimit.AppConcurrencyFingerprint(workspaceID, "orders")
	if err != nil {
		t.Fatal(err)
	}
	policy := ExecutionLimitPolicy{
		ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{
			WorkspaceID: workspaceID,
			AppKey:      "orders",
			Scope:       executionlimit.ScopeApp,
			PolicyID:    executionlimit.ImplicitAppConcurrencyPolicyID,
			Kind:        executionlimit.KindConcurrency,
		},
		ShapeFingerprint: fingerprint,
		Allowance:        3,
	}
	create := MutateExecutionLimitPolicyRequest{
		Policy: policy, ExpectedRevision: 0, OperationID: "op-create", RequestFingerprint: "request-create", Actor: "operator", Reason: "protect shared browser capacity",
	}
	created, replayed, err := store.MutateExecutionLimitPolicy(ctx, create)
	if err != nil || replayed || created.Revision != 1 || created.Allowance != 3 || created.Deleted {
		t.Fatalf("create = %#v, replayed=%v, err=%v", created, replayed, err)
	}
	replayedPolicy, replayed, err := store.MutateExecutionLimitPolicy(ctx, create)
	if err != nil || !replayed || replayedPolicy.Revision != 1 {
		t.Fatalf("replay = %#v, replayed=%v, err=%v", replayedPolicy, replayed, err)
	}
	conflictingReplay := create
	conflictingReplay.RequestFingerprint = "different-request"
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, conflictingReplay); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay err=%v, want ErrConflict", err)
	}
	stale := create
	stale.OperationID = "op-stale"
	stale.RequestFingerprint = "request-stale"
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update err=%v, want ErrConflict", err)
	}
	update := create
	update.Policy.Allowance = 2
	update.ExpectedRevision = 1
	update.OperationID = "op-update"
	update.RequestFingerprint = "request-update"
	updated, replayed, err := store.MutateExecutionLimitPolicy(ctx, update)
	if err != nil || replayed || updated.Revision != 2 || updated.Allowance != 2 {
		t.Fatalf("update = %#v, replayed=%v, err=%v", updated, replayed, err)
	}
	wrongShape := update
	wrongShape.ExpectedRevision = 2
	wrongShape.OperationID = "op-wrong-shape"
	wrongShape.RequestFingerprint = "request-wrong-shape"
	wrongShape.Policy.ShapeFingerprint = executionlimit.FingerprintPrefix + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, wrongShape); !errors.Is(err, ErrConflict) {
		t.Fatalf("shape update err=%v, want ErrConflict", err)
	}
	zero := update
	zero.ExpectedRevision = 2
	zero.OperationID = "op-zero"
	zero.RequestFingerprint = "request-zero"
	zero.Policy.Allowance = 0
	if _, _, err := store.MutateExecutionLimitPolicy(ctx, zero); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("zero allowance err=%v, want ErrInvalidState", err)
	}
	deleteRequest := update
	deleteRequest.ExpectedRevision = 2
	deleteRequest.OperationID = "op-delete"
	deleteRequest.RequestFingerprint = "request-delete"
	deleteRequest.Delete = true
	deleted, replayed, err := store.MutateExecutionLimitPolicy(ctx, deleteRequest)
	if err != nil || replayed || deleted.Revision != 3 || !deleted.Deleted || deleted.Allowance != 0 {
		t.Fatalf("delete = %#v, replayed=%v, err=%v", deleted, replayed, err)
	}
	if _, err := store.GetExecutionLimitPolicy(ctx, policy.ExecutionLimitPolicyKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetExecutionLimitPolicy after delete err=%v, want ErrNotFound", err)
	}
	listed, err := store.ListExecutionLimitPolicies(ctx, workspaceID, "orders")
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed after delete = %#v, err=%v", listed, err)
	}
	deletedReplay, replayed, err := store.MutateExecutionLimitPolicy(ctx, deleteRequest)
	if err != nil || !replayed || !deletedReplay.Deleted || deletedReplay.Revision != 3 {
		t.Fatalf("delete replay = %#v, replayed=%v, err=%v", deletedReplay, replayed, err)
	}
	audits, err := store.ListExecutionLimitPolicyAudit(ctx, workspaceID, "orders")
	if err != nil || len(audits) != 3 || audits[0].EventKind != "created" || audits[1].EventKind != "updated" || audits[2].EventKind != "deleted" {
		t.Fatalf("audits = %#v, err=%v", audits, err)
	}
	exerciseExecutionLimitPolicyBatchAtomicity(t, store, workspaceID)
}

func exerciseExecutionLimitPolicyBatchAtomicity(t *testing.T, store Store, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	makeRequest := func(policyID string, expectedRevision int64) MutateExecutionLimitPolicyRequest {
		fingerprint, err := executionlimit.Fingerprint(executionlimit.Shape{
			WorkspaceID: workspaceID, AppKey: "batch-app", Scope: executionlimit.ScopeApp,
			PolicyID: policyID, Kind: executionlimit.KindConcurrency, InputPointers: []string{"/account"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return MutateExecutionLimitPolicyRequest{
			Policy: ExecutionLimitPolicy{
				ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: "batch-app", Scope: executionlimit.ScopeApp, PolicyID: policyID, Kind: executionlimit.KindConcurrency},
				ShapeFingerprint:        fingerprint, Allowance: 2,
			},
			ExpectedRevision: expectedRevision, OperationID: "op-batch-" + policyID,
			RequestFingerprint: "request-batch-" + policyID, Actor: "provisioning",
		}
	}
	first := makeRequest("first", 0)
	second := makeRequest("second", 1)
	if _, err := store.MutateExecutionLimitPolicies(ctx, []MutateExecutionLimitPolicyRequest{first, second}); !errors.Is(err, ErrConflict) {
		t.Fatalf("failing batch err=%v, want ErrConflict", err)
	}
	if _, err := store.GetExecutionLimitPolicy(ctx, first.Policy.ExecutionLimitPolicyKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partially applied batch first policy err=%v", err)
	}
	second.ExpectedRevision = 0
	results, err := store.MutateExecutionLimitPolicies(ctx, []MutateExecutionLimitPolicyRequest{first, second})
	if err != nil || len(results) != 2 || results[0].Policy.Revision != 1 || results[1].Policy.Revision != 1 {
		t.Fatalf("successful batch = %#v, err=%v", results, err)
	}
}
