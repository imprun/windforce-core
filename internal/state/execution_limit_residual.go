package state

import (
	"context"
	"sort"
	"strconv"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
)

func (s *LocalStore) ListExecutionLimitResiduals(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitResidual, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	jobs := make([]Job, 0)
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	for _, job := range snapshot.Jobs {
		if normalizedJobWorkspace("", job) == workspaceID && jobAppKey(job) == appKey && (job.State == JobQueued || job.State == JobRunning) {
			jobs = append(jobs, job)
		}
	}
	return buildExecutionLimitResiduals(jobs), nil
}

func (s *PostgresStore) ListExecutionLimitResiduals(ctx context.Context, workspaceID string, appKey string) ([]ExecutionLimitResidual, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `SELECT `+jobColumns+`
FROM jobs
WHERE state IN ('queued', 'running')
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$1
  AND COALESCE(NULLIF(payload->>'app', ''), NULLIF(payload->'deployment'->>'app', ''), '')=$2
`, workspaceID, appKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildExecutionLimitResiduals(jobs), nil
}

func buildExecutionLimitResiduals(jobs []Job) []ExecutionLimitResidual {
	type residualCohort struct {
		residual     ExecutionLimitResidual
		runningByKey map[string]int64
	}
	cohorts := map[string]*residualCohort{}
	add := func(job Job, residual ExecutionLimitResidual, keyDigest string) {
		if !executionlimit.IsFingerprint(residual.ShapeFingerprint) {
			return
		}
		cohortKey := executionLimitPolicyStorageKey(residual.ExecutionLimitPolicyKey) + "\x1f" + residual.ShapeFingerprint + "\x1f" + executionLimitCeilingKey(residual.PinnedCeiling)
		current := cohorts[cohortKey]
		if current == nil {
			copy := residual
			copy.PinnedCeiling = cloneInt32Pointer(residual.PinnedCeiling)
			current = &residualCohort{residual: copy, runningByKey: map[string]int64{}}
			cohorts[cohortKey] = current
		}
		if job.State == JobQueued {
			current.residual.Queued++
		} else if job.State == JobRunning {
			current.residual.Running++
			current.runningByKey[keyDigest]++
			if current.runningByKey[keyDigest] > current.residual.MaxRunningForKey {
				current.residual.MaxRunningForKey = current.runningByKey[keyDigest]
			}
		}
	}
	for _, job := range jobs {
		workspaceID := normalizedJobWorkspace("", job)
		appKey := jobAppKey(job)
		appFingerprint := ""
		appCeiling := (*int32)(nil)
		if pin := job.Payload.ExecutionLimits.AppConcurrency; pin != nil {
			appFingerprint = pin.ShapeFingerprint
			appCeiling = pin.MaxConcurrent
		} else {
			appFingerprint, _ = executionlimit.AppConcurrencyFingerprint(workspaceID, appKey)
			if value, ok := jobMaxConcurrent(job); ok {
				ceiling := int32(value)
				appCeiling = &ceiling
			}
		}
		add(job, ExecutionLimitResidual{
			ExecutionLimitPolicyKey: ExecutionLimitPolicyKey{WorkspaceID: workspaceID, AppKey: appKey, Scope: executionlimit.ScopeApp, PolicyID: executionlimit.ImplicitAppConcurrencyPolicyID, Kind: executionlimit.KindConcurrency},
			ShapeFingerprint:        appFingerprint, PinnedCeiling: appCeiling,
		}, "app")
		for _, pin := range job.Payload.ExecutionLimits.Concurrency {
			ceiling := pin.MaxConcurrent
			add(job, ExecutionLimitResidual{
				ExecutionLimitPolicyKey: executionPolicyKeyForJob(job, pin.Scope, pin.PolicyID, executionlimit.KindConcurrency),
				ShapeFingerprint:        pin.ShapeFingerprint, PinnedCeiling: &ceiling,
			}, pin.KeyDigest)
		}
		for _, pin := range job.Payload.ExecutionLimits.Rate {
			ceiling := pin.MaxAttempts
			add(job, ExecutionLimitResidual{
				ExecutionLimitPolicyKey: executionPolicyKeyForJob(job, pin.Scope, pin.PolicyID, executionlimit.KindRate),
				ShapeFingerprint:        pin.ShapeFingerprint, PinnedCeiling: &ceiling, WindowSeconds: pin.WindowSeconds,
			}, pin.KeyDigest)
		}
	}
	residuals := make([]ExecutionLimitResidual, 0, len(cohorts))
	for _, cohort := range cohorts {
		residuals = append(residuals, cohort.residual)
	}
	sort.Slice(residuals, func(i, j int) bool {
		left := executionLimitPolicyStorageKey(residuals[i].ExecutionLimitPolicyKey) + residuals[i].ShapeFingerprint + executionLimitCeilingKey(residuals[i].PinnedCeiling)
		right := executionLimitPolicyStorageKey(residuals[j].ExecutionLimitPolicyKey) + residuals[j].ShapeFingerprint + executionLimitCeilingKey(residuals[j].PinnedCeiling)
		return left < right
	})
	return residuals
}

func executionLimitCeilingKey(value *int32) string {
	if value == nil {
		return "unlimited"
	}
	return strconv.FormatInt(int64(*value), 10)
}
