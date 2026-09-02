package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
	"github.com/imprun/windforce-core/internal/webhook"
)

const (
	testRotationPreviousSecret = "previous-instance-secret-for-tests"
	testRotationCurrentSecret  = "current-instance-secret-for-tests"
)

func TestLocalSecretKeyRotationMigratesLegacyRecordsAtomically(t *testing.T) {
	store, snapshot := newSecretKeyRotationTestStore(t)
	workspaceID := contract.DefaultWorkspace
	legacyDEK := wfcrypto.DeriveWorkspaceKey(testRotationPreviousSecret, workspaceID)
	encrypt := func(value string) json.RawMessage {
		t.Helper()
		return mustWrapRotationJSON(t, legacyDEK, value)
	}

	snapshot.Runs["run"] = Run{
		ID: "run", State: RunSucceeded, Deployment: contract.Deployment{Workspace: workspaceID},
		Input: encrypt(`{"input":1}`), Output: encrypt(`{"output":2}`),
		Result: &contract.JobResult{Output: encrypt(`{"result":3}`)},
	}
	snapshot.Jobs["job"] = Job{
		ID: "job", RunID: "run", State: JobSucceeded,
		Payload: JobPayload{Workspace: workspaceID, Input: encrypt(`{"job":4}`)},
	}
	snapshot.HumanTasks["task"] = HumanTask{
		ID: "task", WorkspaceID: workspaceID, State: HumanTaskCompleted,
		PrivateContextEncrypted: encrypt(`{"private":5}`), DecisionEncrypted: encrypt(`{"decision":6}`),
	}
	snapshot.InputConfigs[workspaceID] = map[string]InputConfig{
		"config": {WorkspaceID: workspaceID, Config: encrypt(`{"config":7}`)},
	}
	snapshot.Triggers["trigger"] = TriggerRecord{
		TriggerDefinition:     TriggerDefinition{ID: "trigger", WorkspaceID: workspaceID},
		SecretConfigEncrypted: encrypt(`{"trigger":8}`),
	}
	snapshot.WebhookSubscriptions["webhook"] = WebhookSubscriptionRecord{
		ID: "webhook", WorkspaceID: workspaceID,
		EndpointEncrypted: encrypt(`"https://example.test/hook"`), SigningSecretEncrypted: encrypt(`"signing-secret"`),
	}
	writeSecretKeyRotationSnapshot(t, store, snapshot)
	beforeDryRun := readSecretKeyRotationState(t, store.Path)

	dryRun, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, false, "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if !dryRun.Changed || dryRun.Applied || dryRun.LegacyWorkspacesMigrated != 1 ||
		dryRun.EncryptedRecordsVerified != 10 || dryRun.EncryptedRecordsRewritten != 10 {
		t.Fatalf("unexpected dry-run counts: %+v", dryRun)
	}
	if afterDryRun := readSecretKeyRotationState(t, store.Path); string(afterDryRun) != string(beforeDryRun) {
		t.Fatal("dry-run changed local state")
	}

	applied, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Changed || !applied.Applied || applied.LegacyWorkspacesMigrated != 1 || applied.EncryptedRecordsRewritten != 10 {
		t.Fatalf("unexpected apply counts: %+v", applied)
	}
	rotated, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	storedKey := rotated.WorkspaceKeys[workspaceID]
	currentDEK, err := wfcrypto.ResolveDEK(storedKey.Key, storedKey.KEKVersion, []string{wfcrypto.DeriveKEK(testRotationCurrentSecret)})
	if err != nil {
		t.Fatal("current key cannot resolve the migrated workspace key")
	}
	if _, err := wfcrypto.ResolveDEK(storedKey.Key, storedKey.KEKVersion, []string{wfcrypto.DeriveKEK(testRotationPreviousSecret)}); err == nil {
		t.Fatal("previous key still resolves the migrated workspace key")
	}
	verified, _, err := rotateLocalWorkspaceRecords(&rotated, workspaceSecretKeyRotation{
		workspaceID: workspaceID, decryptionKeys: []string{currentDEK}, targetDEK: currentDEK,
	})
	if err != nil || verified != 10 {
		t.Fatalf("current-only verification count = %d, err = %v", verified, err)
	}
	if _, _, err := rotateLocalWorkspaceRecords(&rotated, workspaceSecretKeyRotation{
		workspaceID: workspaceID, decryptionKeys: []string{legacyDEK}, targetDEK: legacyDEK,
	}); !errors.Is(err, ErrSecretKeyRotationUnreadable) {
		t.Fatal("legacy derived key still reads migrated records")
	}
	if len(rotated.WorkspaceAudits) == 0 || rotated.WorkspaceAudits[len(rotated.WorkspaceAudits)-1].Kind != "secret_key_rotated" {
		t.Fatal("rotation audit was not recorded")
	}

	beforeRetry := readSecretKeyRotationState(t, store.Path)
	retry, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if retry.Changed || retry.Applied || retry.WorkspacesAlreadyCurrent != 1 || retry.EncryptedRecordsRewritten != 0 {
		t.Fatalf("rotation retry was not idempotent: %+v", retry)
	}
	if afterRetry := readSecretKeyRotationState(t, store.Path); string(afterRetry) != string(beforeRetry) {
		t.Fatal("idempotent retry changed local state")
	}
}

func TestLocalSecretKeyRotationRewrapsModernDEKAndMigratesMixedRecords(t *testing.T) {
	store, snapshot := newSecretKeyRotationTestStore(t)
	workspaceID := contract.DefaultWorkspace
	dek, err := wfcrypto.GenerateDEK()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := wfcrypto.WrapDEK(wfcrypto.DeriveKEK(testRotationPreviousSecret), dek)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.WorkspaceKeys[workspaceID] = WorkspaceKey{Key: wrapped, KEKVersion: wfcrypto.WrappedDEKVersion}
	snapshot.Runs["run"] = Run{
		ID: "run", State: RunSucceeded, Deployment: contract.Deployment{Workspace: workspaceID},
		Input: mustWrapRotationJSON(t, dek, `{"input":1}`),
	}
	legacyDerivedDEK := wfcrypto.DeriveWorkspaceKey(testRotationPreviousSecret, workspaceID)
	snapshot.WebhookSubscriptions["subscription"] = WebhookSubscriptionRecord{
		ID:                     "subscription",
		WorkspaceID:            workspaceID,
		EndpointEncrypted:      mustWrapRotationJSON(t, legacyDerivedDEK, `"https://hooks.example.test/legacy"`),
		SigningSecretEncrypted: mustWrapRotationJSON(t, legacyDerivedDEK, `"legacy-signing-secret"`),
	}
	writeSecretKeyRotationSnapshot(t, store, snapshot)
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	originalCiphertext := append([]byte(nil), before.Runs["run"].Input...)

	report, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.WorkspaceKeysRewrapped != 1 || report.EncryptedRecordsVerified != 3 || report.EncryptedRecordsRewritten != 2 {
		t.Fatalf("unexpected rewrap report: %+v", report)
	}
	after, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Runs["run"].Input) != string(originalCiphertext) {
		t.Fatal("modern DEK rewrap rewrote record ciphertext")
	}
	currentDEK, err := wfcrypto.ResolveDEK(after.WorkspaceKeys[workspaceID].Key, after.WorkspaceKeys[workspaceID].KEKVersion, []string{wfcrypto.DeriveKEK(testRotationCurrentSecret)})
	if err != nil {
		t.Fatal("rewrapped workspace key is not current-key readable")
	}
	for _, encrypted := range [][]byte{
		after.WebhookSubscriptions["subscription"].EndpointEncrypted,
		after.WebhookSubscriptions["subscription"].SigningSecretEncrypted,
	} {
		if _, err := wfcrypto.UnwrapEnc(currentDEK, encrypted); err != nil {
			t.Fatal("mixed legacy Webhook ciphertext was not migrated to the workspace data-encryption key")
		}
		if _, err := wfcrypto.UnwrapEnc(legacyDerivedDEK, encrypted); err == nil {
			t.Fatal("mixed legacy Webhook ciphertext remains readable by the source derived key")
		}
	}
}

func TestLocalSecretKeyRotationReplacesVersionZeroKeyWithFreshWrappedDEK(t *testing.T) {
	store, snapshot := newSecretKeyRotationTestStore(t)
	workspaceID := contract.DefaultWorkspace
	legacyDEK := wfcrypto.DeriveWorkspaceKey(testRotationPreviousSecret, workspaceID)
	snapshot.WorkspaceKeys[workspaceID] = WorkspaceKey{Key: legacyDEK, KEKVersion: 0}
	snapshot.Runs["run"] = Run{
		ID: "run", State: RunSucceeded, Deployment: contract.Deployment{Workspace: workspaceID},
		Input: mustWrapRotationJSON(t, legacyDEK, `{"input":1}`),
	}
	writeSecretKeyRotationSnapshot(t, store, snapshot)

	report, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.LegacyWorkspacesMigrated != 1 || report.EncryptedRecordsRewritten != 1 {
		t.Fatalf("unexpected version-zero migration report: %+v", report)
	}
	after, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stored := after.WorkspaceKeys[workspaceID]
	if stored.KEKVersion != wfcrypto.WrappedDEKVersion {
		t.Fatalf("workspace key version = %d", stored.KEKVersion)
	}
	currentDEK, err := wfcrypto.ResolveDEK(stored.Key, stored.KEKVersion, []string{wfcrypto.DeriveKEK(testRotationCurrentSecret)})
	if err != nil {
		t.Fatal(err)
	}
	if currentDEK == legacyDEK {
		t.Fatal("version-zero key was rewrapped instead of replaced")
	}
	if _, err := wfcrypto.UnwrapEnc(legacyDEK, after.Runs["run"].Input); err == nil {
		t.Fatal("version-zero key still decrypts migrated records")
	}
}

func TestLocalSecretKeyRotationFailureLeavesStateUntouched(t *testing.T) {
	store, snapshot := newSecretKeyRotationTestStore(t)
	workspaceID := contract.DefaultWorkspace
	legacyDEK := wfcrypto.DeriveWorkspaceKey(testRotationPreviousSecret, workspaceID)
	snapshot.Runs["run"] = Run{
		ID: "run", State: RunSucceeded, Deployment: contract.Deployment{Workspace: workspaceID},
		Input:  mustWrapRotationJSON(t, legacyDEK, `{"input":1}`),
		Output: json.RawMessage(`{"__wf_enc":1,"ct":"not-valid-base64"}`),
	}
	writeSecretKeyRotationSnapshot(t, store, snapshot)
	before := readSecretKeyRotationState(t, store.Path)

	_, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if !errors.Is(err, ErrSecretKeyRotationUnreadable) {
		t.Fatalf("rotation error = %v", err)
	}
	if after := readSecretKeyRotationState(t, store.Path); string(after) != string(before) {
		t.Fatal("failed rotation changed local state")
	}
}

func TestLocalSecretKeyRotationReportsBlockersWithoutMutation(t *testing.T) {
	store, snapshot := newSecretKeyRotationTestStore(t)
	workspaceID := contract.DefaultWorkspace
	snapshot.Jobs["queued"] = Job{ID: "queued", State: JobQueued, Payload: JobPayload{Workspace: workspaceID}}
	snapshot.Jobs["running"] = Job{ID: "running", State: JobRunning, Payload: JobPayload{Workspace: workspaceID}}
	snapshot.HumanTasks["pending"] = HumanTask{ID: "pending", WorkspaceID: workspaceID, State: HumanTaskPending}
	snapshot.ExecutionRateBuckets["bucket"] = ExecutionRateBucket{WindowEnd: time.Now().Add(time.Hour)}
	snapshot.Variables[workspaceID] = map[string]Variable{"secret": {Path: "secret", IsSecret: true, Value: "opaque"}}
	snapshot.WebhookDeliveries["webhook"] = webhook.Delivery{ID: "webhook", State: webhook.DeliveryDelivering}
	snapshot.TriggerDeliveries["trigger"] = TriggerDelivery{ID: "trigger", CompletionState: TriggerCompletionRetrying}
	writeSecretKeyRotationSnapshot(t, store, snapshot)
	before := readSecretKeyRotationState(t, store.Path)

	report, err := store.RotateSecretKey(context.Background(), testRotationCurrentSecret, testRotationPreviousSecret, true, "test:operator")
	if !errors.Is(err, ErrSecretKeyRotationBlocked) {
		t.Fatalf("rotation error = %v", err)
	}
	if report.Blockers.QueuedJobs != 1 || report.Blockers.RunningJobs != 1 || report.Blockers.PendingHumanTasks != 1 ||
		report.Blockers.UnexpiredRateBuckets != 1 || report.Blockers.LegacySecretVariables != 1 ||
		report.Blockers.ActiveWebhookDeliveries != 1 || report.Blockers.ActiveTriggerCompletions != 1 {
		t.Fatalf("unexpected blocker counts: %+v", report.Blockers)
	}
	if after := readSecretKeyRotationState(t, store.Path); string(after) != string(before) {
		t.Fatal("blocked rotation changed local state")
	}
}

func newSecretKeyRotationTestStore(t *testing.T) (*LocalStore, Snapshot) {
	t.Helper()
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	snapshot := newSnapshot()
	return store, snapshot
}

func writeSecretKeyRotationSnapshot(t *testing.T, store *LocalStore, snapshot Snapshot) {
	t.Helper()
	if err := store.write(snapshot); err != nil {
		t.Fatal(err)
	}
}

func readSecretKeyRotationState(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustWrapRotationJSON(t *testing.T, key, value string) json.RawMessage {
	t.Helper()
	encrypted, err := wfcrypto.WrapEnc(key, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(encrypted)
}
