package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const maxAdmissionFingerprintBytes = 512

func normalizeAdmissionOutcomeIdentity(workspaceID string, admissionID string) (string, string, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	admissionID = strings.TrimSpace(admissionID)
	if !contract.ValidWorkspaceID(workspaceID) || admissionID == "" || len(admissionID) > 128 || CleanID(admissionID) != admissionID {
		return "", "", fmt.Errorf("%w: invalid admission outcome identity", ErrInvalidState)
	}
	return workspaceID, admissionID, nil
}

func normalizeAdmissionFingerprint(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxAdmissionFingerprintBytes {
		return "", fmt.Errorf("%w: invalid admission request fingerprint", ErrInvalidState)
	}
	return value, nil
}

func admissionOutcomeKey(workspaceID string, admissionID string) string {
	return workspaceID + "\x00" + admissionID
}

func newAdmissionOutcome(
	workspaceID string,
	admissionID string,
	runID string,
	outcomeState AdmissionOutcomeState,
	fingerprint string,
	actor string,
	now time.Time,
) AdmissionOutcome {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = defaultActorSubject
	}
	return AdmissionOutcome{
		WorkspaceID:        workspaceID,
		AdmissionID:        admissionID,
		RunID:              strings.TrimSpace(runID),
		State:              outcomeState,
		RequestFingerprint: strings.TrimSpace(fingerprint),
		ResolvedBy:         actor,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func validateAdmissionOutcome(outcome AdmissionOutcome) error {
	workspaceID, admissionID, err := normalizeAdmissionOutcomeIdentity(outcome.WorkspaceID, outcome.AdmissionID)
	if err != nil || workspaceID != outcome.WorkspaceID || admissionID != outcome.AdmissionID {
		return fmt.Errorf("%w: invalid admission outcome identity", ErrInvalidState)
	}
	if _, err := normalizeAdmissionFingerprint(outcome.RequestFingerprint); err != nil {
		return err
	}
	switch outcome.State {
	case AdmissionOutcomeAdmitted:
		if outcome.RunID == "" || len(outcome.RunID) > 128 || CleanID(outcome.RunID) != outcome.RunID {
			return fmt.Errorf("%w: admitted outcome requires a valid Run id", ErrInvalidState)
		}
	case AdmissionOutcomeAborted:
		if outcome.RunID != "" {
			return fmt.Errorf("%w: aborted outcome cannot reference a Run", ErrInvalidState)
		}
	default:
		return fmt.Errorf("%w: invalid admission outcome state", ErrInvalidState)
	}
	return nil
}

func admissionIdentityForRun(workspaceID string, run Run) (string, string, bool, error) {
	if strings.TrimSpace(run.IdempotencyHash) == "" {
		return "", "", false, nil
	}
	_, admissionID, err := normalizeAdmissionOutcomeIdentity(workspaceID, run.ID)
	if err != nil {
		return "", "", false, err
	}
	fingerprint, err := normalizeAdmissionFingerprint(run.RequestFingerprint)
	if err != nil {
		return "", "", false, err
	}
	return admissionID, fingerprint, true, nil
}

func admissionOutcomeMatchesFingerprint(outcome AdmissionOutcome, expectedFingerprint string) error {
	expectedFingerprint, err := normalizeAdmissionFingerprint(expectedFingerprint)
	if err != nil {
		return err
	}
	if outcome.RequestFingerprint != expectedFingerprint {
		return fmt.Errorf("%w: admission request fingerprint differs", ErrConflict)
	}
	return nil
}
