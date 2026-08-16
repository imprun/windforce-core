package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/telemetry"
)

const runColumns = `
	id, adapter, app, action, state, deployment, input, output, result, error,
	task_id, correlation_id, env, client_id, principal_kind, principal_id,
	idempotency_hash, request_fingerprint, created_by, permissioned_as,
	created_at, updated_at, expires_at, trace_context, execution_limits
`

const jobColumns = `
	id, run_id, state, kind, payload, priority, attempt, lease_owner,
	lease_expires_at, started_at, canceled_by, canceled_reason, created_at, updated_at,
	trace_context, lease_identity
`

const humanTaskColumns = `
	id, workspace_id, run_id, job_id, attempt, task_key, request_fingerprint,
	mode, kind, state, title, description, schema, presentation,
	private_context_encrypted, decision_outcome, decision_encrypted,
	decision_idempotency_key, decision_fingerprint, decided_by, terminal_cause,
	resume_input, created_at, updated_at, decided_at, completed_at, expires_at
`

const postgresClaimCandidateBatchSize = 64

type PostgresStore struct {
	pool              *pgxpool.Pool
	SecretKey         string
	SecretKeyPrevious string
	leaseNow          nowFunc
	executionMetrics  *executionMetrics
	databaseURL       string
	humanTaskSignals  humanTaskSignalHub
	humanTaskListen   sync.Once
	listenerContext   context.Context
	listenerCancel    context.CancelFunc
	listenerReady     chan struct{}
	listenerReadyOnce sync.Once
}

func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	listenerContext, listenerCancel := context.WithCancel(context.Background())
	store := &PostgresStore{
		pool:             pool,
		executionMetrics: newExecutionMetrics(),
		databaseURL:      databaseURL,
		listenerContext:  listenerContext,
		listenerCancel:   listenerCancel,
		listenerReady:    make(chan struct{}),
	}
	return store, nil
}

func (s *PostgresStore) executionMetricsState() (*executionMetrics, string) {
	return s.executionMetrics, "postgres"
}

func (s *PostgresStore) Close() {
	if s == nil {
		return
	}
	if s.listenerCancel != nil {
		s.listenerCancel()
	}
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) ConfigureInputCrypto(secretKey string, previous string) {
	s.SecretKey = strings.TrimSpace(secretKey)
	s.SecretKeyPrevious = strings.TrimSpace(previous)
}

func (s *PostgresStore) encryptInput(ctx context.Context, workspaceID string, input json.RawMessage) (json.RawMessage, error) {
	return encryptInputAtRest(ctx, s, inputCryptoConfig{
		SecretKey:         s.SecretKey,
		SecretKeyPrevious: s.SecretKeyPrevious,
	}, workspaceID, input)
}

func (s *PostgresStore) decryptInput(ctx context.Context, workspaceID string, input json.RawMessage) (json.RawMessage, error) {
	return decryptInputAtRest(ctx, s, inputCryptoConfig{
		SecretKey:         s.SecretKey,
		SecretKeyPrevious: s.SecretKeyPrevious,
	}, workspaceID, input)
}

func (s *PostgresStore) DecryptInput(ctx context.Context, workspaceID string, input json.RawMessage) (json.RawMessage, error) {
	return s.decryptInput(ctx, workspaceID, input)
}

func (s *PostgresStore) encryptResult(ctx context.Context, workspaceID string, result json.RawMessage) (json.RawMessage, error) {
	return encryptResultAtRest(ctx, s, inputCryptoConfig{
		SecretKey:         s.SecretKey,
		SecretKeyPrevious: s.SecretKeyPrevious,
	}, workspaceID, result)
}

func (s *PostgresStore) decryptResult(ctx context.Context, workspaceID string, result json.RawMessage) (json.RawMessage, error) {
	return decryptResultAtRest(ctx, s, inputCryptoConfig{
		SecretKey:         s.SecretKey,
		SecretKeyPrevious: s.SecretKeyPrevious,
	}, workspaceID, result)
}

func (s *PostgresStore) encryptJobResult(ctx context.Context, workspaceID string, result contract.JobResult) (contract.JobResult, error) {
	output, err := s.encryptResult(ctx, workspaceID, result.Output)
	if err != nil {
		return contract.JobResult{}, err
	}
	result.Output = output
	return result, nil
}

func (s *PostgresStore) decryptRunResult(ctx context.Context, workspaceID string, run *Run) error {
	if len(run.Output) > 0 {
		output, err := s.decryptResult(ctx, workspaceID, run.Output)
		if err != nil {
			return err
		}
		run.Output = output
	}
	if run.Result != nil && len(run.Result.Output) > 0 {
		output, err := s.decryptResult(ctx, workspaceID, run.Result.Output)
		if err != nil {
			return err
		}
		result := *run.Result
		result.Output = output
		run.Result = &result
	}
	return nil
}

func (s *PostgresStore) GetJob(ctx context.Context, workspaceID string, jobID string) (Job, Run, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	job, err := scanJob(s.pool.QueryRow(ctx, `
SELECT `+jobColumns+`
FROM jobs
WHERE id=$1
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
`, jobID, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, Run{}, false, nil
	}
	if err != nil {
		return Job{}, Run{}, false, err
	}
	run, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, job.RunID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, Run{}, false, fmt.Errorf("%w: run %q", ErrNotFound, job.RunID)
	}
	if err != nil {
		return Job{}, Run{}, false, err
	}
	if input, err := s.decryptInput(ctx, normalizedJobWorkspace("", job), job.Payload.Input); err != nil {
		return Job{}, Run{}, false, err
	} else {
		job.Payload.Input = input
	}
	if input, err := s.decryptInput(ctx, run.Deployment.SourceWorkspace(), run.Input); err != nil {
		return Job{}, Run{}, false, err
	} else {
		run.Input = input
	}
	if err := s.decryptRunResult(ctx, normalizedJobWorkspace("", job), &run); err != nil {
		return Job{}, Run{}, false, err
	}
	return job, run, true, nil
}

func (s *PostgresStore) GetJobByRunID(ctx context.Context, workspaceID string, runID string) (Job, Run, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var jobID string
	err := s.pool.QueryRow(ctx, `
SELECT id
FROM jobs
WHERE run_id=$1
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
ORDER BY created_at ASC, id ASC
LIMIT 1
`, runID, workspaceID).Scan(&jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, Run{}, false, nil
	}
	if err != nil {
		return Job{}, Run{}, false, err
	}
	return s.GetJob(ctx, workspaceID, jobID)
}

func (s *PostgresStore) ListJobs(ctx context.Context, query JobListQuery) ([]JobListItem, error) {
	workspaceID := contract.NormalizeWorkspace(query.WorkspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT id
FROM jobs
WHERE COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$1
`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []jobRunRecord{}
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return nil, err
		}
		job, run, found, err := s.GetJob(ctx, workspaceID, jobID)
		if err != nil {
			return nil, err
		}
		if found {
			records = append(records, jobRunRecord{Job: job, Run: run})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	query.WorkspaceID = workspaceID
	return listJobsFromRecords(records, query), nil
}

func (s *PostgresStore) JobSummary(ctx context.Context, workspaceID string, recent time.Duration) (JobSummary, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	rows, err := s.pool.Query(ctx, `
SELECT id
FROM jobs
WHERE COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$1
`, workspaceID)
	if err != nil {
		return JobSummary{}, err
	}
	defer rows.Close()
	records := []jobRunRecord{}
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return JobSummary{}, err
		}
		job, run, found, err := s.GetJob(ctx, workspaceID, jobID)
		if err != nil {
			return JobSummary{}, err
		}
		if found {
			records = append(records, jobRunRecord{Job: job, Run: run})
		}
	}
	if err := rows.Err(); err != nil {
		return JobSummary{}, err
	}
	return summarizeJobs(records, workspaceID, recent), nil
}

func (s *PostgresStore) RequeueQueuedJobsForApp(ctx context.Context, spec RequeueAppSpec) (int64, error) {
	workspaceID := contract.NormalizeWorkspace(spec.WorkspaceID)
	var moved int64
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE state=$1 FOR UPDATE`, string(JobQueued))
		if err != nil {
			return err
		}
		defer rows.Close()

		jobs := []Job{}
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for _, job := range jobs {
			if normalizedJobWorkspace("", job) != workspaceID || job.Payload.App != spec.AppKey {
				continue
			}
			if spec.ActionKey != nil && job.Payload.Action != *spec.ActionKey {
				continue
			}
			tag, ok := spec.ActionTags[job.Payload.Action]
			if !ok || strings.TrimSpace(tag) == "" {
				continue
			}
			tag = strings.TrimSpace(tag)
			if job.Payload.Tag == tag {
				continue
			}
			job.Payload.Tag = tag
			job.UpdatedAt = now
			if _, err := tx.Exec(ctx, `UPDATE jobs SET payload=$1, updated_at=$2 WHERE id=$3 AND state=$4`, mustRaw(job.Payload), now, job.ID, string(JobQueued)); err != nil {
				return err
			}
			moved++
		}
		return nil
	})
	return moved, err
}

func (s *PostgresStore) CreateRunAndEnqueue(ctx context.Context, run Run, job Job) error {
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		now := time.Now().UTC()
		run.CreatedAt = nonZeroTime(run.CreatedAt, now)
		run.UpdatedAt = now
		job.CreatedAt = nonZeroTime(job.CreatedAt, now)
		job.UpdatedAt = now
		if job.Payload.CorrelationID == "" {
			job.Payload.CorrelationID = run.CorrelationID
		}
		workspaceID := normalizedJobWorkspace("", job)
		runInput, err := s.encryptInput(ctx, workspaceID, run.Input)
		if err != nil {
			return err
		}
		jobInput, err := s.encryptInput(ctx, workspaceID, job.Payload.Input)
		if err != nil {
			return err
		}
		run.Input = runInput
		job.Payload.Input = jobInput

		if _, err := tx.Exec(ctx, `
INSERT INTO runs (
	id, adapter, app, action, state, deployment, input, output, result, error,
	task_id, correlation_id, env, client_id, principal_kind, principal_id,
	idempotency_hash, request_fingerprint, created_by, permissioned_as,
	created_at, updated_at, expires_at, trace_context, execution_limits
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
	$21, $22, $23, $24, $25
)
`, run.ID, run.Adapter, run.App, run.Action, string(run.State), mustRaw(run.Deployment), requiredRaw(run.Input),
			nullableRaw(run.Output), nullableResult(run.Result), nullableRaw(run.Error), nullableString(run.TaskID),
			nullableString(run.CorrelationID), nullableStrings(run.Env), nullableString(run.ClientID),
			nullableString(run.PrincipalKind), nullableString(run.PrincipalID), nullableString(run.IdempotencyHash),
			nullableString(run.RequestFingerprint), run.CreatedBy, run.PermissionedAs,
			run.CreatedAt, run.UpdatedAt, run.ExpiresAt, nullableTraceContext(run.TraceContext), mustRaw(run.ExecutionLimits)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO jobs (
	id, run_id, state, kind, payload, priority, attempt, lease_owner,
	lease_expires_at, canceled_by, canceled_reason, created_at, updated_at, trace_context
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`, job.ID, job.RunID, string(job.State), job.Kind, mustRaw(job.Payload), job.Priority, job.Attempt,
			nullableString(job.LeaseOwner), job.LeaseExpiresAt, nullableStringPtr(job.CanceledBy), nullableStringPtr(job.CanceledReason), job.CreatedAt, job.UpdatedAt,
			nullableTraceContext(job.TraceContext)); err != nil {
			return err
		}
		runCreated := eventPayload(run.CorrelationID, map[string]any{"app": run.App, "action": run.Action})
		if err := insertEvent(ctx, tx, run.ID, "run_created", runCreated); err != nil {
			return err
		}
		jobEnqueued := eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID})
		return insertEvent(ctx, tx, run.ID, "job_enqueued", jobEnqueued)
	})
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("%w: run or job already exists", ErrConflict)
	}
	return err
}

func nullableTraceContext(value telemetry.TraceContextV1) any {
	if !value.IsCanonical() {
		return nil
	}
	return mustRaw(value)
}

func (s *PostgresStore) GetRun(ctx context.Context, runID string) (Run, error) {
	run, err := scanRun(s.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1`, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: run %q", ErrNotFound, runID)
	}
	if err != nil {
		return Run{}, err
	}
	if input, err := s.decryptInput(ctx, run.Deployment.SourceWorkspace(), run.Input); err != nil {
		return Run{}, err
	} else {
		run.Input = input
	}
	if err := s.decryptRunResult(ctx, run.Deployment.SourceWorkspace(), &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *PostgresStore) GetHumanTask(ctx context.Context, taskID string) (HumanTask, error) {
	task, err := scanHumanTask(s.pool.QueryRow(ctx, `SELECT `+humanTaskColumns+` FROM human_tasks WHERE id=$1`, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanTask{}, fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
	}
	return task, err
}

func (s *PostgresStore) ClaimJob(ctx context.Context, workerID string, leaseTTL time.Duration) (Job, Lease, error) {
	return s.ClaimJobForWorker(ctx, workerID, nil, nil, leaseTTL)
}

func (s *PostgresStore) ClaimJobForWorker(ctx context.Context, workerID string, tags []string, labels []string, leaseTTL time.Duration) (Job, Lease, error) {
	return s.ClaimJobForWorkerScope(ctx, workerID, tags, labels, nil, leaseTTL)
}

func (s *PostgresStore) ClaimJobForWorkerScope(ctx context.Context, workerID string, tags []string, labels []string, workspaceIDs []string, leaseTTL time.Duration) (Job, Lease, error) {
	if workerID == "" {
		workerID = NewID("worker")
	}
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTime
	}
	requestedTags := NormalizeWorkerScope(tags)
	requestedLabels := NormalizeWorkerScope(labels)
	var claimed Job
	var lease Lease
	rateBlockedCandidates := 0
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		now := currentUTC(s.leaseNow)
		registeredTags, registeredLabels, leaseIdentity, err := postgresRegisteredWorkerClaim(
			ctx, tx, workerID, requestedTags, requestedLabels, now,
		)
		if err != nil {
			return err
		}
		allowedTags := normalizeClaimTags(registeredTags)
		offeredLabels := normalizeClaimTags(registeredLabels)
		canceledRows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE state='running' AND lease_expires_at < $1 AND canceled_by IS NOT NULL FOR UPDATE`, now)
		if err != nil {
			return err
		}
		canceledJobs := []Job{}
		for canceledRows.Next() {
			job, err := scanJob(canceledRows)
			if err != nil {
				canceledRows.Close()
				return err
			}
			canceledJobs = append(canceledJobs, job)
		}
		canceledRows.Close()
		if err := canceledRows.Err(); err != nil {
			return err
		}
		for _, job := range canceledJobs {
			run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1 FOR UPDATE`, job.RunID))
			if err != nil {
				return err
			}
			if err := s.completeCanceledJob(ctx, tx, job, run, *job.CanceledBy, canceledReasonValue(job), cancelWorkerLostMessage, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state='queued', lease_owner=NULL, lease_expires_at=NULL, lease_identity=NULL, started_at=NULL, updated_at=$1
WHERE state='running' AND lease_expires_at < $1 AND canceled_by IS NULL
`, now); err != nil {
			return err
		}
		expiresAt := now.Add(leaseTTL)
		candidates, err := postgresClaimCandidates(ctx, tx, allowedTags, offeredLabels, NormalizeWorkerScope(workspaceIDs))
		if err != nil {
			return err
		}
		var selected Job
		for _, candidate := range candidates {
			active, err := postgresAppRuntimeActive(ctx, tx, workerJobWorkspace(candidate), candidate.Payload.App)
			if err != nil {
				return err
			}
			if !active {
				continue
			}
			result, err := postgresClaimLimitsReached(ctx, tx, candidate)
			if err != nil {
				return err
			}
			if result.RateBlocked {
				rateBlockedCandidates++
			}
			if result.Reached {
				continue
			}
			selected = candidate
			break
		}
		if selected.ID == "" {
			if rateBlockedCandidates == 0 {
				blocked, err := postgresRateBlockedCandidateExists(ctx, tx, allowedTags, offeredLabels, NormalizeWorkerScope(workspaceIDs))
				if err != nil {
					return err
				}
				if blocked {
					rateBlockedCandidates = 1
				}
			}
			return ErrNoQueuedJob
		}
		var leaseIdentityJSON json.RawMessage
		if leaseIdentity != nil {
			leaseIdentityJSON, err = json.Marshal(leaseIdentity)
			if err != nil {
				return err
			}
		}
		job, err := scanJob(tx.QueryRow(ctx, `
UPDATE jobs
SET state='running',
    lease_owner=$1,
    lease_expires_at=$2,
    lease_identity=$3,
    started_at=$4,
    attempt=attempt + 1,
    updated_at=$4
WHERE id=$5
RETURNING `+jobColumns, workerID, expiresAt, leaseIdentityJSON, now, selected.ID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, updated_at=$2
WHERE id=$3 AND state IN ($4, $5)
`, string(RunRunning), now, job.RunID, string(RunQueued), string(RunResuming)); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, job.RunID, "job_claimed", eventPayload(job.Payload.CorrelationID, map[string]any{"jobId": job.ID, "workerId": workerID, "attempt": job.Attempt})); err != nil {
			return err
		}
		claimed = job
		lease = Lease{JobID: job.ID, WorkerID: workerID, ExpiresAt: expiresAt, Attempt: job.Attempt, AcquiredAt: now, Identity: leaseIdentity}
		return nil
	})
	if errors.Is(err, ErrNoQueuedJob) && rateBlockedCandidates > 0 {
		s.executionMetrics.observeRateClaims(executionRateOutcomeBlocked, 1)
	}
	if err != nil {
		return Job{}, Lease{}, err
	}
	if len(claimed.Payload.ExecutionLimits.Rate) > 0 {
		s.executionMetrics.observeRateClaims(executionRateOutcomeConsumed, 1)
	}
	return claimed, lease, nil
}

func postgresRegisteredWorkerClaim(ctx context.Context, tx pgx.Tx, workerID string, requestedTags []string, requestedLabels []string, now time.Time) ([]string, []string, *WorkerLeaseIdentity, error) {
	var record WorkerRecord
	var tagsJSON, labelsJSON, profilesJSON []byte
	err := tx.QueryRow(ctx, `
SELECT id, worker_group, tags, labels, execution_profiles, status, credential_generation, last_heartbeat_at
FROM worker_registry
WHERE id=$1
FOR SHARE`, workerID).Scan(
		&record.ID, &record.Group, &tagsJSON, &labelsJSON, &profilesJSON,
		&record.Status, &record.CredentialGeneration, &record.LastHeartbeatAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return requestedTags, requestedLabels, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(tagsJSON, &record.Tags); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(labelsJSON, &record.Labels); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(profilesJSON, &record.ExecutionProfiles); err != nil {
		return nil, nil, nil, err
	}
	registeredTags, registeredLabels, identity, err := RegisteredWorkerClaim(record, requestedTags, requestedLabels, now)
	if err != nil {
		return nil, nil, nil, err
	}
	return registeredTags, registeredLabels, &identity, nil
}

func postgresClaimCandidates(
	ctx context.Context,
	tx pgx.Tx,
	allowedTags map[string]struct{},
	offeredLabels map[string]struct{},
	workspaceIDs []string,
) ([]Job, error) {
	tags := claimFilterValues(allowedTags)
	labels := claimFilterValues(offeredLabels)
	workspaceIDs = NormalizeWorkerScope(workspaceIDs)
	rows, err := tx.Query(ctx, `
SELECT `+jobColumns+`
FROM jobs AS candidate
CROSS JOIN LATERAL (
  SELECT
    COALESCE(NULLIF(candidate.payload->>'workspace', ''), NULLIF(candidate.payload->'deployment'->>'workspace', ''), 'default') AS workspace_id,
    COALESCE(NULLIF(candidate.payload->>'app', ''), NULLIF(candidate.payload->'deployment'->>'app', ''), '') AS app_key,
    COALESCE(NULLIF(candidate.payload->>'maxConcurrent', '')::integer, NULLIF(candidate.payload->'deployment'->>'maxConcurrent', '')::integer, 0) AS max_concurrent
) AS claim
WHERE candidate.state=$1
	AND (cardinality($4::text[]) = 0 OR claim.workspace_id = ANY($4::text[]))
  AND (
    cardinality($2::text[]) = 0
    OR NULLIF(btrim(candidate.payload->>'tag'), '') = ANY($2::text[])
    OR NULLIF(btrim(candidate.payload->>'tag'), '') IS NULL
  )
  AND (
    CASE
      WHEN candidate.payload ? 'requiredLabels' THEN COALESCE(candidate.payload->'requiredLabels', '[]'::jsonb)
      ELSE COALESCE(candidate.payload->'requiredCapabilities', '[]'::jsonb)
    END
  ) <@ to_jsonb($3::text[])
  AND (
    claim.max_concurrent <= 0
    OR (
      SELECT count(*)
      FROM jobs AS running
      WHERE running.state='running'
        AND COALESCE(NULLIF(running.payload->>'workspace', ''), NULLIF(running.payload->'deployment'->>'workspace', ''), 'default') = claim.workspace_id
        AND COALESCE(NULLIF(running.payload->>'app', ''), NULLIF(running.payload->'deployment'->>'app', ''), '') = claim.app_key
    ) < claim.max_concurrent
  )
  AND NOT EXISTS (
    SELECT 1
    FROM jsonb_array_elements(COALESCE(candidate.payload->'executionLimits'->'concurrency', '[]'::jsonb)) AS limit_pin
    WHERE
      COALESCE(NULLIF(limit_pin->>'maxConcurrent', '')::integer, 0) <= 0
      OR NULLIF(limit_pin->>'policyId', '') IS NULL
      OR NULLIF(limit_pin->>'scope', '') IS NULL
      OR NULLIF(limit_pin->>'keyDigest', '') IS NULL
      OR (
        SELECT count(*)
        FROM jobs AS running
        WHERE running.state='running'
          AND COALESCE(NULLIF(running.payload->>'workspace', ''), NULLIF(running.payload->'deployment'->>'workspace', ''), 'default') = claim.workspace_id
          AND COALESCE(running.payload->'executionLimits'->'concurrency', '[]'::jsonb) @>
            jsonb_build_array(
              jsonb_build_object(
                'policyId', limit_pin->>'policyId',
                'scope', limit_pin->>'scope',
                'keyDigest', limit_pin->>'keyDigest'
              ) || CASE
                WHEN COALESCE(limit_pin->>'shapeFingerprint', '') <> ''
                THEN jsonb_build_object('shapeFingerprint', limit_pin->>'shapeFingerprint')
                ELSE '{}'::jsonb
              END
            )
      ) >= (limit_pin->>'maxConcurrent')::integer
  )
  AND NOT EXISTS (
    SELECT 1
    FROM jsonb_array_elements(COALESCE(candidate.payload->'executionLimits'->'rate', '[]'::jsonb)) AS rate_pin
    WHERE
      COALESCE(NULLIF(rate_pin->>'maxAttempts', '')::integer, 0) <= 0
      OR COALESCE(NULLIF(rate_pin->>'windowSeconds', '')::integer, 0) NOT BETWEEN 1 AND 86400
      OR NULLIF(rate_pin->>'policyId', '') IS NULL
      OR NULLIF(rate_pin->>'scope', '') IS NULL
      OR NULLIF(rate_pin->>'keyDigest', '') IS NULL
      OR EXISTS (
        SELECT 1
        FROM execution_rate_bucket AS rate_bucket
        WHERE rate_bucket.workspace_id = claim.workspace_id
          AND rate_bucket.scope = rate_pin->>'scope'
          AND rate_bucket.policy_id = rate_pin->>'policyId'
          AND rate_bucket.shape_fingerprint = COALESCE(rate_pin->>'shapeFingerprint', '')
          AND rate_bucket.key_digest = rate_pin->>'keyDigest'
          AND rate_bucket.window_seconds = (rate_pin->>'windowSeconds')::integer
          AND rate_bucket.window_start <= CURRENT_TIMESTAMP
          AND rate_bucket.window_end > CURRENT_TIMESTAMP
          AND rate_bucket.consumed >= (rate_pin->>'maxAttempts')::integer
      )
  )
ORDER BY candidate.priority ASC, candidate.created_at ASC, candidate.id ASC
LIMIT $5
FOR UPDATE OF candidate SKIP LOCKED
`, string(JobQueued), tags, labels, workspaceIDs, postgresClaimCandidateBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]Job, 0, postgresClaimCandidateBatchSize)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		// Keep the in-memory matcher as a compatibility guard for jobs written
		// before effective route tags and labels were pinned into the payload.
		if claimAllowed(job, allowedTags, offeredLabels) {
			candidates = append(candidates, job)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func postgresRateBlockedCandidateExists(
	ctx context.Context,
	tx pgx.Tx,
	allowedTags map[string]struct{},
	offeredLabels map[string]struct{},
	workspaceIDs []string,
) (bool, error) {
	tags := claimFilterValues(allowedTags)
	labels := claimFilterValues(offeredLabels)
	workspaceIDs = NormalizeWorkerScope(workspaceIDs)
	var blocked bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM jobs AS candidate
  CROSS JOIN LATERAL (
    SELECT COALESCE(NULLIF(candidate.payload->>'workspace', ''), NULLIF(candidate.payload->'deployment'->>'workspace', ''), 'default') AS workspace_id
  ) AS claim
  WHERE candidate.state=$1
    AND (cardinality($4::text[]) = 0 OR claim.workspace_id = ANY($4::text[]))
    AND (
      cardinality($2::text[]) = 0
      OR NULLIF(btrim(candidate.payload->>'tag'), '') = ANY($2::text[])
      OR NULLIF(btrim(candidate.payload->>'tag'), '') IS NULL
    )
    AND (
      CASE
        WHEN candidate.payload ? 'requiredLabels' THEN COALESCE(candidate.payload->'requiredLabels', '[]'::jsonb)
        ELSE COALESCE(candidate.payload->'requiredCapabilities', '[]'::jsonb)
      END
    ) <@ to_jsonb($3::text[])
    AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(candidate.payload->'executionLimits'->'rate', '[]'::jsonb)) AS rate_pin
      JOIN execution_rate_bucket AS rate_bucket
        ON rate_bucket.workspace_id = claim.workspace_id
       AND rate_bucket.scope = rate_pin->>'scope'
       AND rate_bucket.policy_id = rate_pin->>'policyId'
       AND rate_bucket.shape_fingerprint = COALESCE(rate_pin->>'shapeFingerprint', '')
       AND rate_bucket.key_digest = rate_pin->>'keyDigest'
       AND rate_bucket.window_seconds = (rate_pin->>'windowSeconds')::integer
      WHERE rate_bucket.window_start <= CURRENT_TIMESTAMP
        AND rate_bucket.window_end > CURRENT_TIMESTAMP
        AND rate_bucket.consumed >= (rate_pin->>'maxAttempts')::integer
    )
)
`, string(JobQueued), tags, labels, workspaceIDs).Scan(&blocked)
	return blocked, err
}

func claimFilterValues(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}

func (s *PostgresStore) HeartbeatJob(ctx context.Context, lease Lease, leaseTTL time.Duration) (HeartbeatResult, error) {
	if leaseTTL <= 0 {
		leaseTTL = defaultLeaseTime
	}
	now := currentUTC(s.leaseNow)
	expiresAt := now.Add(leaseTTL)
	var result HeartbeatResult
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var canceledBy sql.NullString
		var canceledReason sql.NullString
		err := tx.QueryRow(ctx, `
UPDATE jobs
SET lease_expires_at=$1, updated_at=$2
WHERE id=$3 AND state=$4 AND lease_owner=$5 AND attempt=$6
RETURNING canceled_by, canceled_reason
`, expiresAt, now, lease.JobID, string(JobRunning), lease.WorkerID, lease.Attempt).Scan(&canceledBy, &canceledReason)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		result.StillOwned = true
		if canceledBy.Valid {
			result.CanceledBy = &canceledBy.String
		}
		if canceledReason.Valid {
			result.CanceledReason = &canceledReason.String
		}
		return nil
	})
	return result, err
}

type postgresClaimLimitResult struct {
	Reached     bool
	RateBlocked bool
}

func postgresClaimLimitsReached(ctx context.Context, tx pgx.Tx, candidate Job) (postgresClaimLimitResult, error) {
	workspaceID := normalizedJobWorkspace("", candidate)
	appKey := jobAppKey(candidate)
	lockKeySet := make(map[string]struct{}, len(candidate.Payload.ExecutionLimits.Concurrency)*2+len(candidate.Payload.ExecutionLimits.Rate)*2+2)
	addLock := func(lockKey string) {
		if lockKey != "" {
			lockKeySet[lockKey] = struct{}{}
		}
	}
	if appKey != "" {
		// Preserve the legacy maxConcurrent advisory-lock identity so mixed
		// Core versions remain coordinated during a rolling upgrade. It is
		// always acquired because an implicit operator allowance can make an
		// otherwise unlimited App finite.
		addLock(appKey)
	}
	for _, requirement := range candidateExecutionPolicyRequirements(candidate) {
		addLock(executionLimitPolicyLockKey(requirement.Key))
	}
	for _, limit := range candidate.Payload.ExecutionLimits.Concurrency {
		if !validKeyedConcurrencyPin(limit) {
			return postgresClaimLimitResult{Reached: true}, nil
		}
		addLock("keyed:" + limit.Scope + ":" + limit.PolicyID + ":" + limit.ShapeFingerprint + ":" + limit.KeyDigest)
	}
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		if !validKeyedRatePin(limit) {
			return postgresClaimLimitResult{Reached: true}, nil
		}
		addLock("rate:" + limit.Scope + ":" + limit.PolicyID + ":" + limit.ShapeFingerprint + ":" + limit.KeyDigest + ":" + fmt.Sprint(limit.WindowSeconds))
	}
	lockKeys := make([]string, 0, len(lockKeySet))
	for lockKey := range lockKeySet {
		lockKeys = append(lockKeys, lockKey)
	}
	sort.Strings(lockKeys)
	for _, lockKey := range lockKeys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, workspaceID, lockKey); err != nil {
			return postgresClaimLimitResult{}, err
		}
	}
	// The policy read must happen after every shared identity lock. A policy
	// mutation that commits first is therefore visible to this claim.
	policies, err := postgresExecutionPoliciesForCandidate(ctx, tx, candidate)
	if err != nil {
		return postgresClaimLimitResult{}, err
	}
	appLimit, appLimited, appValid := effectiveAppConcurrencyLimit(candidate, policies)
	if !appValid {
		return postgresClaimLimitResult{Reached: true}, nil
	}
	if appLimited && appKey != "" {
		running, err := postgresRunningJobsForApp(ctx, tx, workspaceID, appKey)
		if err != nil {
			return postgresClaimLimitResult{}, err
		}
		if running >= appLimit {
			return postgresClaimLimitResult{Reached: true}, nil
		}
	}
	for _, limit := range candidate.Payload.ExecutionLimits.Concurrency {
		effectiveLimit, valid := effectiveKeyedConcurrencyLimit(candidate, limit, policies)
		if !valid {
			return postgresClaimLimitResult{Reached: true}, nil
		}
		running, err := postgresRunningJobsForKeyedConcurrency(ctx, tx, workspaceID, limit)
		if err != nil {
			return postgresClaimLimitResult{}, err
		}
		if running >= effectiveLimit {
			return postgresClaimLimitResult{Reached: true}, nil
		}
	}
	if len(candidate.Payload.ExecutionLimits.Rate) > 0 {
		rateNow, err := postgresCurrentRateTime(ctx, tx)
		if err != nil {
			return postgresClaimLimitResult{}, err
		}
		reached, err := postgresRateLimitsReached(ctx, tx, candidate, rateNow, policies)
		if err != nil {
			return postgresClaimLimitResult{}, err
		}
		if reached {
			return postgresClaimLimitResult{Reached: true, RateBlocked: true}, nil
		}
		if err := postgresConsumeRateLimits(ctx, tx, candidate, rateNow); err != nil {
			return postgresClaimLimitResult{}, err
		}
	}
	return postgresClaimLimitResult{}, nil
}

func postgresRunningJobsForKeyedConcurrency(ctx context.Context, tx pgx.Tx, workspaceID string, limit KeyedConcurrencyLimitPin) (int, error) {
	matchFields := map[string]string{
		"policyId":  limit.PolicyID,
		"scope":     limit.Scope,
		"keyDigest": limit.KeyDigest,
	}
	if limit.ShapeFingerprint != "" {
		matchFields["shapeFingerprint"] = limit.ShapeFingerprint
	}
	match, err := json.Marshal([]map[string]string{matchFields})
	if err != nil {
		return 0, err
	}
	var running int
	err = tx.QueryRow(ctx, `
SELECT COUNT(*)
FROM jobs
WHERE state=$1
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
  AND COALESCE(payload->'executionLimits'->'concurrency', '[]'::jsonb) @> $3::jsonb
`, string(JobRunning), workspaceID, match).Scan(&running)
	return running, err
}

func postgresRunningJobsForApp(ctx context.Context, tx pgx.Tx, workspaceID string, appKey string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT `+jobColumns+` FROM jobs WHERE state=$1`, string(JobRunning))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return 0, err
		}
		if normalizedJobWorkspace("", job) == workspaceID && jobAppKey(job) == appKey {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *PostgresStore) CompleteJobSucceeded(ctx context.Context, lease Lease, result contract.JobResult) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		job, run, err := leasedJobAndRunPostgres(ctx, tx, lease)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if job.CanceledBy != nil {
			return s.completeCanceledJob(ctx, tx, job, run, *job.CanceledBy, canceledReasonValue(job), cancelDuringExecutionMessage, now)
		}
		storedResult, err := s.encryptJobResult(ctx, normalizedJobWorkspace("", job), result)
		if err != nil {
			return err
		}
		if err := updateJobComplete(ctx, tx, job.ID, JobSucceeded, now); err != nil {
			return err
		}
		if err := cancelHeldHumanTasksPostgresTx(ctx, tx, normalizedJobWorkspace("", job), job.ID, HumanTaskCauseJobCompleted, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, output=$2, result=$3, error=NULL, task_id=NULL, updated_at=$4
WHERE id=$5
`, string(RunSucceeded), nullableRaw(storedResult.Output), mustRaw(storedResult), now, run.ID); err != nil {
			return err
		}
		return insertEvent(ctx, tx, run.ID, "run_succeeded", eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID}))
	})
}

func (s *PostgresStore) CompleteJobFailed(ctx context.Context, lease Lease, result contract.JobResult) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		job, run, err := leasedJobAndRunPostgres(ctx, tx, lease)
		if err != nil {
			return err
		}
		if result.Error == "" && result.ExitCode != 0 {
			result.Error = fmt.Sprintf("action exited with code %d", result.ExitCode)
		}
		now := time.Now().UTC()
		if job.CanceledBy != nil {
			return s.completeCanceledJob(ctx, tx, job, run, *job.CanceledBy, canceledReasonValue(job), cancelDuringExecutionMessage, now)
		}
		storedResult, err := s.encryptJobResult(ctx, normalizedJobWorkspace("", job), result)
		if err != nil {
			return err
		}
		if err := updateJobComplete(ctx, tx, job.ID, JobFailed, now); err != nil {
			return err
		}
		if err := cancelHeldHumanTasksPostgresTx(ctx, tx, normalizedJobWorkspace("", job), job.ID, humanTaskTerminalCause(result), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, result=$2, error=$3, updated_at=$4
WHERE id=$5
`, string(RunFailed), mustRaw(storedResult), mustRaw(map[string]any{"message": result.Error, "exitCode": result.ExitCode}), now, run.ID); err != nil {
			return err
		}
		return insertEvent(ctx, tx, run.ID, "run_failed", eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID, "error": result.Error, "exitCode": result.ExitCode}))
	})
}

func (s *PostgresStore) CompleteJobWaitingHuman(ctx context.Context, lease Lease, result contract.JobResult, task HumanTask) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		job, run, err := leasedJobAndRunPostgres(ctx, tx, lease)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if task.ID == "" {
			task.ID = NewID("human")
		}
		if task.RunID == "" {
			task.RunID = run.ID
		}
		if task.State == "" {
			task.State = HumanTaskPending
		}
		task.CreatedAt = nonZeroTime(task.CreatedAt, now)
		if job.CanceledBy != nil {
			return s.completeCanceledJob(ctx, tx, job, run, *job.CanceledBy, canceledReasonValue(job), cancelDuringExecutionMessage, now)
		}
		storedResult, err := s.encryptJobResult(ctx, normalizedJobWorkspace("", job), result)
		if err != nil {
			return err
		}
		if err := updateJobComplete(ctx, tx, job.ID, JobSucceeded, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO human_tasks (
	id, run_id, state, title, description, schema, resume_input,
	created_at, completed_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`, task.ID, task.RunID, string(task.State), task.Title, nullableString(task.Description), nullableRaw(task.Schema),
			nullableRaw(task.ResumeInput), task.CreatedAt, task.CompletedAt, task.ExpiresAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, result=$2, error=NULL, task_id=$3, updated_at=$4
WHERE id=$5
`, string(RunWaitingHuman), mustRaw(storedResult), task.ID, now, run.ID); err != nil {
			return err
		}
		return insertEvent(ctx, tx, run.ID, "human_task_created", eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID, "taskId": task.ID}))
	})
}

func (s *PostgresStore) ResumeHumanTask(ctx context.Context, taskID string, resumeInput json.RawMessage) (Run, Job, error) {
	var resumedRun Run
	var enqueuedJob Job
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		task, err := scanHumanTask(tx.QueryRow(ctx, `SELECT `+humanTaskColumns+` FROM human_tasks WHERE id=$1 FOR UPDATE`, taskID))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: human task %q", ErrNotFound, taskID)
		}
		if err != nil {
			return err
		}
		if task.State != HumanTaskPending {
			return fmt.Errorf("%w: human task %q is %s", ErrInvalidState, taskID, task.State)
		}
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1 FOR UPDATE`, task.RunID))
		if err != nil {
			return err
		}
		if run.State != RunWaitingHuman {
			return fmt.Errorf("%w: run %q is %s", ErrInvalidState, run.ID, run.State)
		}
		if len(resumeInput) == 0 {
			resumeInput = json.RawMessage("{}")
		}
		if !json.Valid(resumeInput) {
			return errors.New("resume input is not valid JSON")
		}

		now := time.Now().UTC()
		task.State = HumanTaskCompleted
		task.ResumeInput = cloneRaw(resumeInput)
		task.CompletedAt = &now
		run.State = RunResuming
		run.TaskID = ""
		run.UpdatedAt = now
		plainRunInput, err := s.decryptInput(ctx, run.Deployment.SourceWorkspace(), run.Input)
		if err != nil {
			return err
		}
		job := NewActionJob(run, mergeResumeInput(plainRunInput, task.ID, resumeInput))
		job.CreatedAt = now
		job.UpdatedAt = now
		storedJobInput, err := s.encryptInput(ctx, normalizedJobWorkspace("", job), job.Payload.Input)
		if err != nil {
			return err
		}
		storedJob := job
		storedJob.Payload.Input = storedJobInput

		if _, err := tx.Exec(ctx, `
UPDATE human_tasks
SET state=$1, resume_input=$2, completed_at=$3
WHERE id=$4
`, string(task.State), nullableRaw(task.ResumeInput), task.CompletedAt, task.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, task_id=NULL, updated_at=$2
WHERE id=$3
`, string(run.State), run.UpdatedAt, run.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO jobs (
	id, run_id, state, kind, payload, priority, attempt, lease_owner,
	lease_expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, storedJob.ID, storedJob.RunID, string(storedJob.State), storedJob.Kind, mustRaw(storedJob.Payload), storedJob.Priority, storedJob.Attempt,
			nullableString(storedJob.LeaseOwner), storedJob.LeaseExpiresAt, storedJob.CreatedAt, storedJob.UpdatedAt); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, run.ID, "human_task_resumed", eventPayload(run.CorrelationID, map[string]any{"taskId": task.ID, "jobId": job.ID})); err != nil {
			return err
		}
		resumedRun = run
		enqueuedJob = job
		return nil
	})
	return resumedRun, enqueuedJob, err
}

func (s *PostgresStore) ResumeRun(ctx context.Context, runID string, resumeInput json.RawMessage) (Run, Job, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return Run{}, Job{}, err
	}
	if run.TaskID == "" {
		return Run{}, Job{}, fmt.Errorf("%w: run %q has no pending human task", ErrInvalidState, runID)
	}
	return s.ResumeHumanTask(ctx, run.TaskID, resumeInput)
}

func (s *PostgresStore) CancelJob(ctx context.Context, workspaceID string, jobID string, by string, reason string) (CancelResult, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	var result CancelResult
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		job, err := scanJob(tx.QueryRow(ctx, `
SELECT `+jobColumns+`
FROM jobs
WHERE id=$1
  AND COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')=$2
FOR UPDATE
`, jobID, workspaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1 FOR UPDATE`, job.RunID))
		if err != nil {
			return err
		}
		result.Found = true
		if IsTerminal(run) || job.State == JobSucceeded || job.State == JobFailed {
			result.AlreadyCompleted = true
			return nil
		}
		canceledBy := cancelActorSubject(job, run, by)
		if job.State == JobRunning {
			if _, err := tx.Exec(ctx, `
UPDATE jobs
SET canceled_by=$1, canceled_reason=$2, updated_at=$3
WHERE id=$4
`, canceledBy, reason, time.Now().UTC(), job.ID); err != nil {
				return err
			}
			result.SoftCanceled = true
			return insertEvent(ctx, tx, run.ID, "job_cancel_requested", eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID, "by": canceledBy, "reason": reason}))
		}
		result.CompletedNow = true
		now := time.Now().UTC()
		return s.completeCanceledJob(ctx, tx, job, run, canceledBy, reason, cancelBeforeExecutionMessage, now)
	})
	return result, err
}

func (s *PostgresStore) CancelRun(ctx context.Context, runID string, reason string) (Run, error) {
	var canceled Run
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1 FOR UPDATE`, runID))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: run %q", ErrNotFound, runID)
		}
		if err != nil {
			return err
		}
		if IsTerminal(run) {
			return fmt.Errorf("%w: run %q is %s", ErrInvalidState, runID, run.State)
		}
		now := time.Now().UTC()
		run.State = RunCanceled
		run.Error = mustRaw(map[string]string{"message": reason})
		run.UpdatedAt = now
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, error=$2, updated_at=$3
WHERE id=$4
`, string(run.State), run.Error, run.UpdatedAt, run.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state=$1, lease_owner=NULL, lease_expires_at=NULL, lease_identity=NULL, updated_at=$2
WHERE run_id=$3 AND state IN ($4, $5)
`, string(JobFailed), now, run.ID, string(JobQueued), string(JobRunning)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE human_tasks
SET state=$1, updated_at=$4
WHERE run_id=$2 AND state=$3 AND mode <> 'hold'
`, string(HumanTaskExpired), run.ID, string(HumanTaskPending), now); err != nil {
			return err
		}
		if err := cancelHeldHumanTasksForRunPostgresTx(ctx, tx, run.ID, HumanTaskCauseRunCanceled, now); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, run.ID, "run_canceled", eventPayload(run.CorrelationID, map[string]any{"reason": reason})); err != nil {
			return err
		}
		canceled = run
		return nil
	})
	return canceled, err
}

func (s *PostgresStore) RetryRun(ctx context.Context, runID string) (Run, Job, error) {
	var retried Run
	var enqueued Job
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		run, err := scanRun(tx.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id=$1 FOR UPDATE`, runID))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: run %q", ErrNotFound, runID)
		}
		if err != nil {
			return err
		}
		switch run.State {
		case RunFailed, RunCanceled, RunExpired:
		default:
			return fmt.Errorf("%w: run %q is %s", ErrInvalidState, runID, run.State)
		}
		now := time.Now().UTC()
		run.State = RunQueued
		run.Output = nil
		run.Result = nil
		run.Error = nil
		run.TaskID = ""
		run.UpdatedAt = now
		plainRunInput, err := s.decryptInput(ctx, run.Deployment.SourceWorkspace(), run.Input)
		if err != nil {
			return err
		}
		job := NewActionJob(run, plainRunInput)
		job.CreatedAt = now
		job.UpdatedAt = now
		storedJobInput, err := s.encryptInput(ctx, normalizedJobWorkspace("", job), job.Payload.Input)
		if err != nil {
			return err
		}
		storedJob := job
		storedJob.Payload.Input = storedJobInput
		if _, err := tx.Exec(ctx, `
UPDATE runs
SET state=$1, output=NULL, result=NULL, error=NULL, task_id=NULL, updated_at=$2
WHERE id=$3
`, string(run.State), run.UpdatedAt, run.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO jobs (
	id, run_id, state, kind, payload, priority, attempt, lease_owner,
	lease_expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`, storedJob.ID, storedJob.RunID, string(storedJob.State), storedJob.Kind, mustRaw(storedJob.Payload), storedJob.Priority, storedJob.Attempt,
			nullableString(storedJob.LeaseOwner), storedJob.LeaseExpiresAt, storedJob.CreatedAt, storedJob.UpdatedAt); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, run.ID, "run_retried", eventPayload(run.CorrelationID, map[string]any{"jobId": job.ID})); err != nil {
			return err
		}
		retried = run
		enqueued = job
		return nil
	})
	return retried, enqueued, err
}

func (s *PostgresStore) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PruneSettledJobs removes settled runs together with their jobs, logs,
// events, and human tasks: succeeded runs older than successOlderThan, and
// failed/canceled/expired runs older than failureOlderThan. Queued, running,
// and waiting-human runs are never touched. It returns the number of jobs
// removed.
func (s *PostgresStore) PruneSettledJobs(ctx context.Context, successOlderThan time.Time, failureOlderThan time.Time) (int64, error) {
	var pruned int64
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
CREATE TEMPORARY TABLE prune_runs ON COMMIT DROP AS
SELECT id FROM runs
WHERE (state = 'SUCCEEDED' AND updated_at < $1)
   OR (state IN ('FAILED', 'CANCELED', 'EXPIRED') AND updated_at < $2)
`, successOlderThan, failureOlderThan); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
DELETE FROM job_logs WHERE job_id IN (SELECT id FROM jobs WHERE run_id IN (SELECT id FROM prune_runs))
`); err != nil {
			return err
		}
		jobs, err := tx.Exec(ctx, `DELETE FROM jobs WHERE run_id IN (SELECT id FROM prune_runs)`)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM run_events WHERE run_id IN (SELECT id FROM prune_runs)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM human_tasks WHERE run_id IN (SELECT id FROM prune_runs)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM runs WHERE id IN (SELECT id FROM prune_runs)`); err != nil {
			return err
		}
		pruned = jobs.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pruned, nil
}

// ExpireStuckJobs transitions runs stuck in queued/running/resuming without
// progress since stuckBefore into the expired/failure family. Heartbeats
// refresh jobs.updated_at, so actively leased jobs are never stuck. It
// returns the number of runs expired.
func (s *PostgresStore) ExpireStuckJobs(ctx context.Context, stuckBefore time.Time) (int64, error) {
	var expired int64
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
CREATE TEMPORARY TABLE stuck_runs ON COMMIT DROP AS
SELECT r.id FROM runs r
WHERE r.state IN ('QUEUED', 'RUNNING', 'RESUMING')
  AND r.updated_at < $1
  AND NOT EXISTS (
    SELECT 1 FROM jobs j WHERE j.run_id = r.id AND j.updated_at >= $1
  )
`, stuckBefore); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE jobs SET state = 'failed',
  canceled_reason = COALESCE(canceled_reason, 'expired by retention policy: no progress before ' || to_char($1::timestamptz at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
  updated_at = now()
WHERE run_id IN (SELECT id FROM stuck_runs)
  AND state NOT IN ('succeeded', 'failed')
`, stuckBefore); err != nil {
			return err
		}
		if err := cancelHeldHumanTasksForStuckRunsPostgresTx(ctx, tx, HumanTaskCauseWorkerLost, time.Now().UTC()); err != nil {
			return err
		}
		runs, err := tx.Exec(ctx, `
UPDATE runs SET state = 'EXPIRED', updated_at = now()
WHERE id IN (SELECT id FROM stuck_runs)
`)
		if err != nil {
			return err
		}
		expired = runs.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return expired, nil
}

func (s *PostgresStore) RegisterWorker(ctx context.Context, record WorkerRecord) error {
	engineVersion, err := NormalizeWorkerBuildValue(record.EngineVersion)
	if err != nil {
		return err
	}
	buildRevision, err := NormalizeWorkerBuildValue(record.BuildRevision)
	if err != nil {
		return err
	}
	record.EngineVersion = engineVersion
	record.BuildRevision = buildRevision
	profilesNormalized, err := contract.NormalizeExecutionProfiles(record.ExecutionProfiles)
	if err != nil {
		return err
	}
	record.ExecutionProfiles = profilesNormalized
	if record.Slots <= 0 {
		record.Slots = 1
	}
	status, err := NormalizeWorkerStatus(record.Status)
	if err != nil {
		return err
	}
	tags, err := json.Marshal(append([]string{}, record.Tags...))
	if err != nil {
		return err
	}
	labels, err := json.Marshal(append([]string{}, record.Labels...))
	if err != nil {
		return err
	}
	profiles, err := json.Marshal(append([]contract.ExecutionProfile{}, record.ExecutionProfiles...))
	if err != nil {
		return err
	}
	commandTag, err := s.pool.Exec(ctx, `
INSERT INTO worker_registry (id, worker_group, engine_version, build_revision, tags, labels, execution_profiles, slots, status, credential_id, credential_generation, started_at, last_heartbeat_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())
ON CONFLICT (id) DO UPDATE SET
    worker_group = EXCLUDED.worker_group,
    engine_version = EXCLUDED.engine_version,
    build_revision = EXCLUDED.build_revision,
    tags = EXCLUDED.tags,
    labels = EXCLUDED.labels,
    execution_profiles = EXCLUDED.execution_profiles,
    slots = EXCLUDED.slots,
    status = EXCLUDED.status,
    credential_id = EXCLUDED.credential_id,
    credential_generation = EXCLUDED.credential_generation,
    last_heartbeat_at = now()
WHERE worker_registry.credential_id = EXCLUDED.credential_id
  AND worker_registry.credential_generation = EXCLUDED.credential_generation`,
		record.ID, record.Group, record.EngineVersion, record.BuildRevision, tags, labels, profiles, record.Slots, status, record.CredentialID, record.CredentialGeneration)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return ErrForbidden
	}
	return nil
}

func (s *PostgresStore) HeartbeatWorker(ctx context.Context, workerID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE worker_registry SET last_heartbeat_at = now() WHERE id = $1`, workerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: worker %q", ErrNotFound, workerID)
	}
	return nil
}

func (s *PostgresStore) DeregisterWorker(ctx context.Context, workerID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM worker_registry WHERE id = $1`, workerID)
	return err
}

func (s *PostgresStore) ListWorkers(ctx context.Context) ([]WorkerRecord, error) {
	// Crashed workers never deregister — drop their silent records.
	if _, err := s.pool.Exec(ctx, `
DELETE FROM worker_registry WHERE last_heartbeat_at < now() - $1::interval`,
		WorkerRegistryExpiry.String()); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, worker_group, engine_version, build_revision, tags, labels, execution_profiles, slots, status, credential_id, credential_generation, started_at, last_heartbeat_at
FROM worker_registry ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkerRecord{}
	for rows.Next() {
		var record WorkerRecord
		var tags, labels, profiles []byte
		if err := rows.Scan(&record.ID, &record.Group, &record.EngineVersion, &record.BuildRevision, &tags, &labels, &profiles, &record.Slots, &record.Status, &record.CredentialID, &record.CredentialGeneration, &record.StartedAt, &record.LastHeartbeatAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tags, &record.Tags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(labels, &record.Labels); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profiles, &record.ExecutionProfiles); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
