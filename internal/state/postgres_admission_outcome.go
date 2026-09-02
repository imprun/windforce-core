package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const postgresAdmissionOutcomeColumns = `
workspace_id, admission_id, COALESCE(run_id, ''), state, request_fingerprint,
resolved_by, created_at, updated_at`

type postgresAdmissionOutcomeQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type postgresAdmissionRunRecord struct {
	ID                 string
	RequestFingerprint string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (s *PostgresStore) GetAdmissionOutcome(ctx context.Context, workspaceID string, admissionID string) (AdmissionOutcome, bool, error) {
	workspaceID, admissionID, err := normalizeAdmissionOutcomeIdentity(workspaceID, admissionID)
	if err != nil {
		return AdmissionOutcome{}, false, err
	}
	outcome, found, err := postgresGetAdmissionOutcome(ctx, s.pool, workspaceID, admissionID)
	if err != nil || found {
		return outcome, found, err
	}
	run, found, err := postgresGetAdmissionRun(ctx, s.pool, workspaceID, admissionID)
	if err != nil || !found {
		return AdmissionOutcome{}, false, err
	}
	outcome = newAdmissionOutcome(
		workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
		run.RequestFingerprint, "core:legacy-admission", run.CreatedAt,
	)
	outcome.UpdatedAt = run.UpdatedAt
	if err := validateAdmissionOutcome(outcome); err != nil {
		return AdmissionOutcome{}, false, err
	}
	return outcome, true, nil
}

func (s *PostgresStore) ResolveAdmissionOutcome(
	ctx context.Context,
	workspaceID string,
	admissionID string,
	expectedFingerprint string,
	actor string,
) (AdmissionOutcome, error) {
	workspaceID, admissionID, err := normalizeAdmissionOutcomeIdentity(workspaceID, admissionID)
	if err != nil {
		return AdmissionOutcome{}, err
	}
	expectedFingerprint, err = normalizeAdmissionFingerprint(expectedFingerprint)
	if err != nil {
		return AdmissionOutcome{}, err
	}
	var resolved AdmissionOutcome
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := lockPostgresAdmissionOutcome(ctx, tx, workspaceID, admissionID); err != nil {
			return err
		}
		existing, found, err := postgresGetAdmissionOutcome(ctx, tx, workspaceID, admissionID)
		if err != nil {
			return err
		}
		if found {
			if err := admissionOutcomeMatchesFingerprint(existing, expectedFingerprint); err != nil {
				return err
			}
			resolved = existing
			return nil
		}

		run, found, err := postgresGetAdmissionRun(ctx, tx, workspaceID, admissionID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if found {
			if run.RequestFingerprint != expectedFingerprint {
				return fmt.Errorf("%w: admission request fingerprint differs", ErrConflict)
			}
			resolved = newAdmissionOutcome(
				workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
				expectedFingerprint, actor, now,
			)
		} else {
			resolved = newAdmissionOutcome(
				workspaceID, admissionID, "", AdmissionOutcomeAborted,
				expectedFingerprint, actor, now,
			)
		}
		return postgresInsertAdmissionOutcome(ctx, tx, resolved)
	})
	return resolved, err
}

func preparePostgresAdmissionOutcome(ctx context.Context, tx pgx.Tx, workspaceID string, run Run, now time.Time) (*AdmissionOutcome, error) {
	admissionID, fingerprint, tracked, err := admissionIdentityForRun(workspaceID, run)
	if err != nil || !tracked {
		return nil, err
	}
	if err := lockPostgresAdmissionOutcome(ctx, tx, workspaceID, admissionID); err != nil {
		return nil, err
	}
	existing, found, err := postgresGetAdmissionOutcome(ctx, tx, workspaceID, admissionID)
	if err != nil {
		return nil, err
	}
	if found {
		if err := admissionOutcomeMatchesFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if existing.State == AdmissionOutcomeAborted {
			return nil, fmt.Errorf("%w: admission %q", ErrAdmissionAborted, admissionID)
		}
		return nil, fmt.Errorf("%w: admission %q was already admitted", ErrConflict, admissionID)
	}
	outcome := newAdmissionOutcome(
		workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
		fingerprint, "core:admission", now,
	)
	return &outcome, nil
}

func lockPostgresAdmissionOutcome(ctx context.Context, tx pgx.Tx, workspaceID string, admissionID string) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"run-admission\x1f"+workspaceID+"\x1f"+admissionID,
	)
	return err
}

func postgresGetAdmissionOutcome(
	ctx context.Context,
	querier postgresAdmissionOutcomeQuerier,
	workspaceID string,
	admissionID string,
) (AdmissionOutcome, bool, error) {
	row := querier.QueryRow(ctx, `SELECT `+postgresAdmissionOutcomeColumns+`
FROM admission_outcome
WHERE workspace_id=$1 AND admission_id=$2`, workspaceID, admissionID)
	var outcome AdmissionOutcome
	var outcomeState string
	if err := row.Scan(
		&outcome.WorkspaceID,
		&outcome.AdmissionID,
		&outcome.RunID,
		&outcomeState,
		&outcome.RequestFingerprint,
		&outcome.ResolvedBy,
		&outcome.CreatedAt,
		&outcome.UpdatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return AdmissionOutcome{}, false, nil
	} else if err != nil {
		return AdmissionOutcome{}, false, err
	}
	outcome.State = AdmissionOutcomeState(outcomeState)
	if err := validateAdmissionOutcome(outcome); err != nil {
		return AdmissionOutcome{}, false, err
	}
	return outcome, true, nil
}

func postgresGetAdmissionRun(
	ctx context.Context,
	querier postgresAdmissionOutcomeQuerier,
	workspaceID string,
	admissionID string,
) (postgresAdmissionRunRecord, bool, error) {
	var run postgresAdmissionRunRecord
	err := querier.QueryRow(ctx, `
SELECT r.id, COALESCE(r.request_fingerprint, ''), r.created_at, r.updated_at
FROM runs r
JOIN jobs j ON j.run_id = r.id
WHERE r.id = $1
  AND COALESCE(r.idempotency_hash, '') <> ''
  AND COALESCE(NULLIF(j.payload->>'workspace', ''), NULLIF(j.payload->'deployment'->>'workspace', ''), 'default') = $2
ORDER BY j.created_at ASC, j.id ASC
LIMIT 1
`, admissionID, workspaceID).Scan(&run.ID, &run.RequestFingerprint, &run.CreatedAt, &run.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return postgresAdmissionRunRecord{}, false, nil
	}
	if err != nil {
		return postgresAdmissionRunRecord{}, false, err
	}
	return run, true, nil
}

func postgresInsertAdmissionOutcome(ctx context.Context, tx pgx.Tx, outcome AdmissionOutcome) error {
	if err := validateAdmissionOutcome(outcome); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
INSERT INTO admission_outcome (
	workspace_id, admission_id, run_id, state, request_fingerprint,
	resolved_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, outcome.WorkspaceID, outcome.AdmissionID, nullableString(outcome.RunID), string(outcome.State),
		outcome.RequestFingerprint, outcome.ResolvedBy, outcome.CreatedAt, outcome.UpdatedAt)
	return err
}
