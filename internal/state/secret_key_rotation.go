package state

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

var (
	ErrSecretKeyRotationBlocked    = errors.New("secret-key rotation is blocked by active or unsupported state")
	ErrSecretKeyRotationUnreadable = errors.New("an encrypted record cannot be decrypted by the configured rotation keys")
)

type SecretKeyRotationBlockers struct {
	QueuedJobs               int `json:"queuedJobs"`
	RunningJobs              int `json:"runningJobs"`
	PendingHumanTasks        int `json:"pendingHumanTasks"`
	UnexpiredRateBuckets     int `json:"unexpiredRateBuckets"`
	LegacySecretVariables    int `json:"legacySecretVariables"`
	ActiveWebhookDeliveries  int `json:"activeWebhookDeliveries"`
	ActiveTriggerCompletions int `json:"activeTriggerCompletions"`
}

func (b SecretKeyRotationBlockers) Any() bool {
	return b.QueuedJobs > 0 || b.RunningJobs > 0 || b.PendingHumanTasks > 0 || b.UnexpiredRateBuckets > 0 ||
		b.LegacySecretVariables > 0 || b.ActiveWebhookDeliveries > 0 || b.ActiveTriggerCompletions > 0
}

type SecretKeyRotationReport struct {
	Backend                   string                    `json:"backend"`
	Mode                      string                    `json:"mode"`
	Changed                   bool                      `json:"changed"`
	Applied                   bool                      `json:"applied"`
	WorkspacesScanned         int                       `json:"workspacesScanned"`
	WorkspaceKeysRewrapped    int                       `json:"workspaceKeysRewrapped"`
	LegacyWorkspacesMigrated  int                       `json:"legacyWorkspacesMigrated"`
	WorkspacesAlreadyCurrent  int                       `json:"workspacesAlreadyCurrent"`
	EncryptedRecordsVerified  int                       `json:"encryptedRecordsVerified"`
	EncryptedRecordsRewritten int                       `json:"encryptedRecordsRewritten"`
	Blockers                  SecretKeyRotationBlockers `json:"blockers"`
}

type workspaceSecretKeyRotation struct {
	workspaceID      string
	decryptionKeys   []string
	targetDEK        string
	wrappedTargetDEK string
	reencrypt        bool
	auditDetail      string
}

// RotateSecretKey prepares or applies an atomic local-state migration from
// previousSecret to currentSecret. Values are accepted by the caller from
// environment variables; this method never includes key material in its
// report or errors.
func (s *LocalStore) RotateSecretKey(ctx context.Context, currentSecret, previousSecret string, apply bool, actor string) (SecretKeyRotationReport, error) {
	report := SecretKeyRotationReport{Backend: "local", Mode: "dry-run"}
	if apply {
		report.Mode = "apply"
	}
	currentSecret = strings.TrimSpace(currentSecret)
	previousSecret = strings.TrimSpace(previousSecret)
	if currentSecret == "" || previousSecret == "" {
		return report, errors.New("current and previous secret-key environment variables are required")
	}
	if currentSecret == previousSecret {
		return report, errors.New("current and previous secret keys must differ")
	}
	if strings.TrimSpace(s.Path) == "" {
		return report, errors.New("state path is required")
	}
	if strings.TrimSpace(actor) == "" {
		actor = "operator:secret-key-rotation"
	}

	err := s.withLock(ctx, func() error {
		snapshot, err := s.Load(ctx)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		report.Blockers = localSecretKeyRotationBlockers(snapshot, now)
		legacyWorkspaces := localLegacyEncryptionWorkspaces(snapshot)
		report.Blockers.LegacySecretVariables = localLegacySecretVariableCount(snapshot, legacyWorkspaces)
		if report.Blockers.Any() {
			return ErrSecretKeyRotationBlocked
		}

		plans, err := planLocalSecretKeyRotation(snapshot, currentSecret, previousSecret, &report)
		if err != nil {
			return err
		}
		changedWorkspaces := make(map[string]string)
		for _, plan := range plans {
			verified, rewritten, err := rotateLocalWorkspaceRecords(&snapshot, plan)
			if err != nil {
				return err
			}
			report.EncryptedRecordsVerified += verified
			report.EncryptedRecordsRewritten += rewritten
			if plan.wrappedTargetDEK != "" {
				snapshot.WorkspaceKeys[plan.workspaceID] = WorkspaceKey{Key: plan.wrappedTargetDEK, KEKVersion: wfcrypto.WrappedDEKVersion}
			}
			if plan.wrappedTargetDEK != "" || rewritten > 0 {
				detail := plan.auditDetail
				if detail == "" {
					detail = "legacy encrypted records migrated to the workspace data-encryption key"
				}
				changedWorkspaces[plan.workspaceID] = detail
			}
		}
		report.Changed = len(changedWorkspaces) > 0
		if err := verifyLocalSecretKeyRotation(snapshot, currentSecret); err != nil {
			return err
		}
		if !apply || !report.Changed {
			return nil
		}
		workspaceIDs := make([]string, 0, len(changedWorkspaces))
		for workspaceID := range changedWorkspaces {
			workspaceIDs = append(workspaceIDs, workspaceID)
		}
		slices.Sort(workspaceIDs)
		for _, workspaceID := range workspaceIDs {
			appendLocalWorkspaceAudit(&snapshot, workspaceID, "secret_key_rotated", changedWorkspaces[workspaceID], actor, now)
		}
		if err := s.write(snapshot); err != nil {
			return err
		}
		report.Applied = true
		return nil
	})
	return report, err
}

func localSecretKeyRotationBlockers(snapshot Snapshot, now time.Time) SecretKeyRotationBlockers {
	var blockers SecretKeyRotationBlockers
	for _, job := range snapshot.Jobs {
		switch job.State {
		case JobQueued:
			blockers.QueuedJobs++
		case JobRunning:
			blockers.RunningJobs++
		}
	}
	for _, task := range snapshot.HumanTasks {
		if task.State == HumanTaskPending {
			blockers.PendingHumanTasks++
		}
	}
	for _, bucket := range snapshot.ExecutionRateBuckets {
		if bucket.WindowEnd.After(now) {
			blockers.UnexpiredRateBuckets++
		}
	}
	for _, delivery := range snapshot.WebhookDeliveries {
		if isActiveWebhookDelivery(delivery.State) {
			blockers.ActiveWebhookDeliveries++
		}
	}
	for _, delivery := range snapshot.TriggerDeliveries {
		switch delivery.CompletionState {
		case TriggerCompletionPending, TriggerCompletionDelivering, TriggerCompletionRetrying:
			blockers.ActiveTriggerCompletions++
		}
	}
	return blockers
}

func localLegacyEncryptionWorkspaces(snapshot Snapshot) map[string]struct{} {
	legacy := make(map[string]struct{})
	for workspaceID := range snapshot.Workspaces {
		workspaceID = contract.NormalizeWorkspace(workspaceID)
		key, ok := snapshot.WorkspaceKeys[workspaceID]
		if !ok || key.Key == "" || key.KEKVersion == 0 {
			legacy[workspaceID] = struct{}{}
		}
	}
	return legacy
}

func localLegacySecretVariableCount(snapshot Snapshot, legacy map[string]struct{}) int {
	count := 0
	for workspaceID, variables := range snapshot.Variables {
		if _, ok := legacy[contract.NormalizeWorkspace(workspaceID)]; !ok {
			continue
		}
		for _, variable := range variables {
			if variable.IsSecret {
				count++
			}
		}
	}
	return count
}

func planLocalSecretKeyRotation(snapshot Snapshot, currentSecret, previousSecret string, report *SecretKeyRotationReport) ([]workspaceSecretKeyRotation, error) {
	workspaceIDs := make([]string, 0, len(snapshot.Workspaces))
	for workspaceID := range snapshot.Workspaces {
		workspaceIDs = append(workspaceIDs, contract.NormalizeWorkspace(workspaceID))
	}
	slices.Sort(workspaceIDs)
	plans := make([]workspaceSecretKeyRotation, 0, len(workspaceIDs))
	currentKEK := wfcrypto.DeriveKEK(currentSecret)
	previousKEK := wfcrypto.DeriveKEK(previousSecret)
	for _, workspaceID := range workspaceIDs {
		report.WorkspacesScanned++
		stored, exists := snapshot.WorkspaceKeys[workspaceID]
		if exists && stored.Key != "" && stored.KEKVersion == wfcrypto.WrappedDEKVersion {
			if dek, err := wfcrypto.UnwrapDEK(currentKEK, stored.Key); err == nil {
				report.WorkspacesAlreadyCurrent++
				plans = append(plans, workspaceSecretKeyRotation{
					workspaceID: workspaceID,
					decryptionKeys: uniqueNonEmpty([]string{
						dek,
						wfcrypto.DeriveWorkspaceKey(currentSecret, workspaceID),
						wfcrypto.DeriveWorkspaceKey(previousSecret, workspaceID),
					}),
					targetDEK: dek,
					reencrypt: true,
				})
				continue
			}
			dek, err := wfcrypto.UnwrapDEK(previousKEK, stored.Key)
			if err != nil {
				return nil, errors.New("a wrapped workspace key cannot be unwrapped by the configured rotation keys")
			}
			wrapped, err := wfcrypto.WrapDEK(currentKEK, dek)
			if err != nil {
				return nil, fmt.Errorf("rewrap workspace data-encryption key: %w", err)
			}
			report.WorkspaceKeysRewrapped++
			plans = append(plans, workspaceSecretKeyRotation{
				workspaceID: workspaceID,
				decryptionKeys: uniqueNonEmpty([]string{
					dek,
					wfcrypto.DeriveWorkspaceKey(currentSecret, workspaceID),
					wfcrypto.DeriveWorkspaceKey(previousSecret, workspaceID),
				}),
				targetDEK: dek, wrappedTargetDEK: wrapped, reencrypt: true,
				auditDetail: "workspace data-encryption key rewrapped",
			})
			continue
		}
		if exists && stored.Key != "" && stored.KEKVersion != 0 {
			return nil, errors.New("workspace key uses an unsupported wrapping version")
		}

		freshDEK, err := wfcrypto.GenerateDEK()
		if err != nil {
			return nil, fmt.Errorf("generate workspace data-encryption key: %w", err)
		}
		wrapped, err := wfcrypto.WrapDEK(currentKEK, freshDEK)
		if err != nil {
			return nil, fmt.Errorf("wrap workspace data-encryption key: %w", err)
		}
		keys := make([]string, 0, 3)
		if stored.KEKVersion == 0 && stored.Key != "" {
			keys = append(keys, stored.Key)
		}
		keys = append(keys,
			wfcrypto.DeriveWorkspaceKey(currentSecret, workspaceID),
			wfcrypto.DeriveWorkspaceKey(previousSecret, workspaceID),
		)
		report.LegacyWorkspacesMigrated++
		plans = append(plans, workspaceSecretKeyRotation{
			workspaceID: workspaceID, decryptionKeys: uniqueNonEmpty(keys), targetDEK: freshDEK,
			wrappedTargetDEK: wrapped, reencrypt: true, auditDetail: "legacy encrypted records migrated to a random workspace data-encryption key",
		})
	}
	return plans, nil
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func rotateLocalWorkspaceRecords(snapshot *Snapshot, plan workspaceSecretKeyRotation) (int, int, error) {
	verified := 0
	rewritten := 0
	rotate := func(value []byte, category string) ([]byte, error) {
		updated, encrypted, changed, err := rotateEncryptedJSON(value, plan.decryptionKeys, plan.targetDEK, plan.reencrypt)
		if err != nil {
			return nil, fmt.Errorf("rotate %s: %w", category, err)
		}
		if encrypted {
			verified++
		}
		if changed {
			rewritten++
		}
		return updated, nil
	}

	for id, run := range snapshot.Runs {
		if contract.NormalizeWorkspace(run.Deployment.SourceWorkspace()) != plan.workspaceID {
			continue
		}
		var err error
		if run.Input, err = rotate(run.Input, "run input"); err != nil {
			return 0, 0, err
		}
		if run.Output, err = rotate(run.Output, "run output"); err != nil {
			return 0, 0, err
		}
		if run.Result != nil {
			result := *run.Result
			if result.Output, err = rotate(result.Output, "run result output"); err != nil {
				return 0, 0, err
			}
			run.Result = &result
		}
		snapshot.Runs[id] = run
	}
	for id, job := range snapshot.Jobs {
		if normalizedJobWorkspace("", job) != plan.workspaceID {
			continue
		}
		updated, err := rotate(job.Payload.Input, "job input")
		if err != nil {
			return 0, 0, err
		}
		job.Payload.Input = updated
		snapshot.Jobs[id] = job
	}
	for id, task := range snapshot.HumanTasks {
		if contract.NormalizeWorkspace(task.WorkspaceID) != plan.workspaceID {
			continue
		}
		var err error
		if task.PrivateContextEncrypted, err = rotate(task.PrivateContextEncrypted, "HumanTask private context"); err != nil {
			return 0, 0, err
		}
		if task.DecisionEncrypted, err = rotate(task.DecisionEncrypted, "HumanTask decision"); err != nil {
			return 0, 0, err
		}
		snapshot.HumanTasks[id] = task
	}
	for workspaceID, configs := range snapshot.InputConfigs {
		if contract.NormalizeWorkspace(workspaceID) != plan.workspaceID {
			continue
		}
		for key, config := range configs {
			updated, err := rotate(config.Config, "input config")
			if err != nil {
				return 0, 0, err
			}
			config.Config = updated
			configs[key] = config
		}
	}
	for id, trigger := range snapshot.Triggers {
		if contract.NormalizeWorkspace(trigger.WorkspaceID) != plan.workspaceID {
			continue
		}
		updated, err := rotate(trigger.SecretConfigEncrypted, "trigger secret config")
		if err != nil {
			return 0, 0, err
		}
		trigger.SecretConfigEncrypted = updated
		snapshot.Triggers[id] = trigger
	}
	for id, subscription := range snapshot.WebhookSubscriptions {
		if contract.NormalizeWorkspace(subscription.WorkspaceID) != plan.workspaceID {
			continue
		}
		var err error
		if subscription.EndpointEncrypted, err = rotate(subscription.EndpointEncrypted, "webhook endpoint"); err != nil {
			return 0, 0, err
		}
		if subscription.SigningSecretEncrypted, err = rotate(subscription.SigningSecretEncrypted, "webhook signing secret"); err != nil {
			return 0, 0, err
		}
		snapshot.WebhookSubscriptions[id] = subscription
	}
	return verified, rewritten, nil
}

func rotateEncryptedJSON(value []byte, decryptionKeys []string, targetDEK string, reencrypt bool) ([]byte, bool, bool, error) {
	if !wfcrypto.IsEnc(value) {
		return value, false, false, nil
	}
	var plaintext []byte
	var decryptionKey string
	var err error
	for _, key := range decryptionKeys {
		plaintext, err = wfcrypto.UnwrapEnc(key, value)
		if err == nil {
			decryptionKey = key
			break
		}
	}
	if err != nil {
		return nil, true, false, ErrSecretKeyRotationUnreadable
	}
	if !reencrypt || decryptionKey == targetDEK {
		return value, true, false, nil
	}
	updated, err := wfcrypto.WrapEnc(targetDEK, plaintext)
	if err != nil {
		return nil, true, false, err
	}
	return updated, true, true, nil
}

func verifyLocalSecretKeyRotation(snapshot Snapshot, currentSecret string) error {
	workspaceIDs := make([]string, 0, len(snapshot.Workspaces))
	for workspaceID := range snapshot.Workspaces {
		workspaceIDs = append(workspaceIDs, contract.NormalizeWorkspace(workspaceID))
	}
	slices.Sort(workspaceIDs)
	for _, workspaceID := range workspaceIDs {
		stored, ok := snapshot.WorkspaceKeys[workspaceID]
		if !ok || stored.Key == "" {
			return errors.New("workspace key is missing after rotation")
		}
		dek, err := wfcrypto.ResolveDEK(stored.Key, stored.KEKVersion, []string{wfcrypto.DeriveKEK(currentSecret)})
		if err != nil {
			return errors.New("workspace key cannot be resolved by the current key after rotation")
		}
		if _, _, err := rotateLocalWorkspaceRecords(&snapshot, workspaceSecretKeyRotation{
			workspaceID: workspaceID, decryptionKeys: []string{dek}, targetDEK: dek,
		}); err != nil {
			return fmt.Errorf("verify current-key-only reads: %w", err)
		}
	}
	return nil
}
