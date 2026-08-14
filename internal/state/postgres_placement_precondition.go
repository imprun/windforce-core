package state

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const placementSerializableAttempts = 3

func (s *PostgresStore) UpdateRoutingPolicyWithPrecondition(ctx context.Context, request PlacementPolicyMutationRequest) (PlacementPolicyMutationResult, error) {
	request, err := normalizePlacementPolicyMutationRequest(request)
	if err != nil {
		return PlacementPolicyMutationResult{}, err
	}
	var result PlacementPolicyMutationResult
	for attempt := 0; attempt < placementSerializableAttempts; attempt++ {
		result = PlacementPolicyMutationResult{}
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return PlacementPolicyMutationResult{}, err
		}
		err = func() error {
			defer tx.Rollback(ctx)
			if err := lockPostgresRoutingPolicy(ctx, tx, request.WorkspaceID, request.App); err != nil {
				return err
			}
			deployment, err := postgresDeploymentForUpdate(ctx, tx, request.WorkspaceID, request.App)
			if err != nil {
				return err
			}
			if request.Action != "" {
				if _, ok := deployment.Actions[request.Action]; !ok {
					return catalog.ErrActionNotFound
				}
			}
			policy, err := postgresRoutingPolicy(ctx, tx, request.WorkspaceID, request.App, true)
			if err != nil {
				return err
			}
			if replay, ok, err := replayPlacementPolicyMutation(policy, request); err != nil {
				return err
			} else if ok {
				result = PlacementPolicyMutationResult{
					Deployment: catalog.ApplyRoutingPolicy(deployment, policy), Policy: policy, Check: replay, Replayed: true,
				}
				return tx.Commit(ctx)
			}
			if policy.Revision != request.ExpectedRevision {
				return ErrConflict
			}
			var checkedAt time.Time
			if err := tx.QueryRow(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&checkedAt); err != nil {
				return err
			}
			checkedAt = checkedAt.UTC()
			workers, err := postgresPlacementWorkers(ctx, tx)
			if err != nil {
				return err
			}
			credentials, err := postgresPlacementCredentials(ctx, tx)
			if err != nil {
				return err
			}
			runStates, err := postgresPlacementRunStates(ctx, tx)
			if err != nil {
				return err
			}
			candidatePolicy := catalog.ApplyRoutingPolicyPatch(policy, request.Action, request.Patch, checkedAt)
			candidateDeployment := catalog.ApplyRoutingPolicy(deployment, candidatePolicy)
			check, sufficient, err := observePlacementTargets(
				candidateDeployment, request.Action, request.MinimumMatchingSlots, checkedAt,
				workers, credentials, runStates,
			)
			result = PlacementPolicyMutationResult{Deployment: candidateDeployment, Policy: policy, Check: check}
			if err != nil {
				return err
			}
			if !sufficient {
				return ErrInsufficientPlacementCapacity
			}
			candidatePolicy = catalog.RecordRoutingPolicyOperation(candidatePolicy, request.OperationID, request.RequestFingerprint, check)
			check = *candidatePolicy.LastOperationResult
			if err := upsertPostgresRoutingPolicy(ctx, tx, candidatePolicy); err != nil {
				return err
			}
			if err := insertPostgresCatalogAudit(ctx, tx,
				placementPolicyAuditRecord(deployment, request.Action, policy, candidatePolicy, request.Actor, checkedAt)); err != nil {
				return err
			}
			result = PlacementPolicyMutationResult{Deployment: candidateDeployment, Policy: candidatePolicy, Check: check}
			return tx.Commit(ctx)
		}()
		if err == nil || !postgresSerializationFailure(err) {
			return result, err
		}
	}
	return PlacementPolicyMutationResult{}, err
}

func postgresPlacementWorkers(ctx context.Context, tx pgx.Tx) ([]WorkerRecord, error) {
	rows, err := tx.Query(ctx, `
SELECT id, worker_group, engine_version, build_revision, tags, labels, execution_profiles, slots, status, credential_id, credential_generation, started_at, last_heartbeat_at
FROM worker_registry ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workers := []WorkerRecord{}
	for rows.Next() {
		var worker WorkerRecord
		var tags, labels, profiles []byte
		if err := rows.Scan(
			&worker.ID, &worker.Group, &worker.EngineVersion, &worker.BuildRevision, &tags, &labels, &profiles,
			&worker.Slots, &worker.Status, &worker.CredentialID, &worker.CredentialGeneration, &worker.StartedAt, &worker.LastHeartbeatAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tags, &worker.Tags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(labels, &worker.Labels); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(profiles, &worker.ExecutionProfiles); err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	return workers, rows.Err()
}

func postgresPlacementCredentials(ctx context.Context, tx pgx.Tx) (map[string]WorkerCredential, error) {
	rows, err := tx.Query(ctx, `SELECT `+workerCredentialColumns+` FROM worker_credential`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	credentials := map[string]WorkerCredential{}
	for rows.Next() {
		credential, err := scanWorkerCredential(rows)
		if err != nil {
			return nil, err
		}
		credentials[credential.ID] = credential
	}
	return credentials, rows.Err()
}

func postgresPlacementRunStates(ctx context.Context, tx pgx.Tx) (map[string]WorkerGroupRunState, error) {
	rows, err := tx.Query(ctx, `
SELECT worker_group, state, operation_id, revision, deadline_at, request_fingerprint, updated_by, updated_at
FROM worker_group_run_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runStates := map[string]WorkerGroupRunState{}
	for rows.Next() {
		var runState WorkerGroupRunState
		if err := rows.Scan(
			&runState.Group, &runState.State, &runState.OperationID, &runState.Revision, &runState.DeadlineAt,
			&runState.RequestFingerprint, &runState.UpdatedBy, &runState.UpdatedAt,
		); err != nil {
			return nil, err
		}
		runStates[runState.Group] = runState
	}
	return runStates, rows.Err()
}

func postgresSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
