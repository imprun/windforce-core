package state

import (
	"context"
	"fmt"
	"time"
)

func (s *LocalStore) GetAdmissionOutcome(ctx context.Context, workspaceID string, admissionID string) (AdmissionOutcome, bool, error) {
	workspaceID, admissionID, err := normalizeAdmissionOutcomeIdentity(workspaceID, admissionID)
	if err != nil {
		return AdmissionOutcome{}, false, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return AdmissionOutcome{}, false, err
	}
	if outcome, ok := snapshot.AdmissionOutcomes[admissionOutcomeKey(workspaceID, admissionID)]; ok {
		if err := validateAdmissionOutcome(outcome); err != nil {
			return AdmissionOutcome{}, false, err
		}
		return outcome, true, nil
	}
	run, ok := localAdmissionRun(&snapshot, workspaceID, admissionID)
	if !ok {
		return AdmissionOutcome{}, false, nil
	}
	outcome := newAdmissionOutcome(
		workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
		run.RequestFingerprint, "core:legacy-admission", run.CreatedAt,
	)
	outcome.UpdatedAt = run.UpdatedAt
	if err := validateAdmissionOutcome(outcome); err != nil {
		return AdmissionOutcome{}, false, err
	}
	return outcome, true, nil
}

func (s *LocalStore) ResolveAdmissionOutcome(
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
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := admissionOutcomeKey(workspaceID, admissionID)
		if existing, ok := snapshot.AdmissionOutcomes[key]; ok {
			if err := validateAdmissionOutcome(existing); err != nil {
				return err
			}
			if err := admissionOutcomeMatchesFingerprint(existing, expectedFingerprint); err != nil {
				return err
			}
			resolved = existing
			return nil
		}
		if run, ok := localAdmissionRun(snapshot, workspaceID, admissionID); ok {
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
		snapshot.AdmissionOutcomes[key] = resolved
		return nil
	})
	return resolved, err
}

func bindLocalAdmissionOutcome(snapshot *Snapshot, workspaceID string, run Run, now time.Time) error {
	admissionID, fingerprint, tracked, err := admissionIdentityForRun(workspaceID, run)
	if err != nil || !tracked {
		return err
	}
	key := admissionOutcomeKey(workspaceID, admissionID)
	if existing, ok := snapshot.AdmissionOutcomes[key]; ok {
		if err := validateAdmissionOutcome(existing); err != nil {
			return err
		}
		if err := admissionOutcomeMatchesFingerprint(existing, fingerprint); err != nil {
			return err
		}
		if existing.State == AdmissionOutcomeAborted {
			return fmt.Errorf("%w: admission %q", ErrAdmissionAborted, admissionID)
		}
		return fmt.Errorf("%w: admission %q was already admitted", ErrConflict, admissionID)
	}
	snapshot.AdmissionOutcomes[key] = newAdmissionOutcome(
		workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
		fingerprint, "core:admission", now,
	)
	return nil
}

func migrateLocalAdmissionOutcomes(snapshot *Snapshot) {
	for _, job := range snapshot.Jobs {
		run, ok := snapshot.Runs[job.RunID]
		if !ok {
			continue
		}
		workspaceID := normalizedJobWorkspace("", job)
		admissionID, fingerprint, tracked, err := admissionIdentityForRun(workspaceID, run)
		if err != nil || !tracked {
			continue
		}
		key := admissionOutcomeKey(workspaceID, admissionID)
		if _, exists := snapshot.AdmissionOutcomes[key]; exists {
			continue
		}
		outcome := newAdmissionOutcome(
			workspaceID, admissionID, run.ID, AdmissionOutcomeAdmitted,
			fingerprint, "core:legacy-admission", run.CreatedAt,
		)
		outcome.UpdatedAt = run.UpdatedAt
		snapshot.AdmissionOutcomes[key] = outcome
	}
}

func localAdmissionRun(snapshot *Snapshot, workspaceID string, admissionID string) (Run, bool) {
	run, ok := snapshot.Runs[admissionID]
	if !ok || run.IdempotencyHash == "" {
		return Run{}, false
	}
	for _, job := range snapshot.Jobs {
		if job.RunID == run.ID && normalizedJobWorkspace("", job) == workspaceID {
			return run, true
		}
	}
	return Run{}, false
}
