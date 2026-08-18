package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
	controlevent "github.com/imprun/windforce-core/internal/event"
	"github.com/imprun/windforce-core/internal/webhook"
)

func TestLocalStoreEmptyHumanTaskDeadlineSweepDoesNotWrite(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	task := seedHeldHumanTask(t, store, "empty-deadline-sweep")
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	marker := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(store.Path, marker, marker); err != nil {
		t.Fatal(err)
	}

	expired, err := store.ExpireDueHeldHumanTasks(context.Background(), task.ExpiresAt.Add(-time.Second), 100)
	if err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("expired = %d, want 0", expired)
	}
	after, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.SnapshotRevision != before.SnapshotRevision {
		t.Fatalf("snapshot revision = %d, want unchanged %d", after.SnapshotRevision, before.SnapshotRevision)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(marker) {
		t.Fatalf("state mtime = %s, want unchanged %s", info.ModTime(), marker)
	}
}

func TestLocalStoreDueHumanTaskDeadlineSweepStillWrites(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	task := seedHeldHumanTask(t, store, "due-deadline-sweep")
	before, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	expired, err := store.ExpireDueHeldHumanTasks(context.Background(), task.ExpiresAt.Add(time.Second), 100)
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	after, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.SnapshotRevision != before.SnapshotRevision+1 {
		t.Fatalf("snapshot revision = %d, want %d", after.SnapshotRevision, before.SnapshotRevision+1)
	}
	if got := after.HumanTasks[task.ID].State; got != HumanTaskExpired {
		t.Fatalf("HumanTask state = %q, want %q", got, HumanTaskExpired)
	}
}

func TestLocalStoreHeldHumanTaskLifecycleEncryptsSensitiveValues(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("human-task-test-secret", "")
	if _, err := store.CreateSubscription(context.Background(), webhook.Subscription{
		WorkspaceID: "default", Name: "HumanTask adapter", Endpoint: "https://adapter.example.test/events",
		SigningSecret: "webhook-signing-secret", EventTypes: []string{controlevent.HumanTaskCreatedType, controlevent.HumanTaskDecidedType},
		Enabled: true, CreatedBy: "operator:test",
	}); err != nil {
		t.Fatalf("CreateSubscription returned error: %v", err)
	}
	exerciseHeldHumanTaskLifecycle(t, store)

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(snapshot.HumanTasks) != 1 {
		t.Fatalf("HumanTasks = %#v", snapshot.HumanTasks)
	}
	eventTypes := map[string]bool{}
	for _, lifecycleEvent := range snapshot.ControlPlaneEvents {
		eventTypes[lifecycleEvent.Type] = true
		if bytes.Contains(lifecycleEvent.Data, []byte("callback-secret")) || bytes.Contains(lifecycleEvent.Data, []byte("otp-secret")) {
			t.Fatalf("HumanTask lifecycle event contains a sensitive value: %s", lifecycleEvent.Data)
		}
	}
	if !eventTypes[controlevent.HumanTaskCreatedType] || !eventTypes[controlevent.HumanTaskDecidedType] {
		t.Fatalf("HumanTask lifecycle event types = %#v", eventTypes)
	}
	if len(snapshot.WebhookDeliveries) != 2 {
		t.Fatalf("HumanTask lifecycle webhook deliveries = %#v", snapshot.WebhookDeliveries)
	}
	for _, task := range snapshot.HumanTasks {
		if !wfcrypto.IsEnc(task.PrivateContextEncrypted) || !wfcrypto.IsEnc(task.DecisionEncrypted) {
			t.Fatalf("HumanTask sensitive fields are not encrypted: %#v", task)
		}
		encoded, _ := json.Marshal(snapshot)
		for _, secret := range [][]byte{[]byte("callback-secret"), []byte("otp-secret")} {
			if bytes.Contains(encoded, secret) {
				t.Fatalf("local state contains HumanTask plaintext %q", secret)
			}
		}
	}
}

func TestLocalStoreHeldHumanTaskDecisionAndExpiryRace(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("human-task-race-secret", "")
	exerciseHeldHumanTaskTerminalRace(t, store)
}

func TestLocalStoreHeldHumanTaskChangeSignal(t *testing.T) {
	store := NewLocalStore(t.TempDir() + "/state.json")
	store.ConfigureInputCrypto("human-task-signal-secret", "")
	task := seedHeldHumanTask(t, store, "local-signal")
	changed, cancel := store.SubscribeHumanTaskChanges(task.ID)
	defer cancel()
	if _, err := store.DecideHeldHumanTask(context.Background(), "default", task.ID, HumanTaskDecision{
		Outcome: HumanTaskOutcomeSubmit, Value: json.RawMessage(`{}`), IdempotencyKey: "signal-decision",
		Fingerprint: "signal-fingerprint", Actor: "operator:test",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("LocalStore HumanTask change did not wake the keyed subscriber")
	}
}

func TestPostgresStoreHeldHumanTaskDecisionAndExpiryRace(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("OpenPostgresStore returned error: %v", err)
	}
	defer store.Close()
	store.ConfigureInputCrypto("human-task-race-secret", "")
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if _, err := store.pool.Exec(context.Background(), `TRUNCATE webhook_delivery, control_plane_event, job_logs, run_events, human_tasks, jobs, runs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE returned error: %v", err)
	}
	exerciseHeldHumanTaskTerminalRace(t, store)
}

func TestPostgresStoreHeldHumanTaskLifecycleEncryptsSensitiveValues(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("OpenPostgresStore returned error: %v", err)
	}
	defer store.Close()
	store.ConfigureInputCrypto("human-task-test-secret", "")
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if _, err := store.pool.Exec(context.Background(), `TRUNCATE webhook_delivery, control_plane_event, job_logs, run_events, human_tasks, jobs, runs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE returned error: %v", err)
	}
	exerciseHeldHumanTaskLifecycle(t, store)
	var lifecycleEvents int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM control_plane_event WHERE event_type LIKE 'windforce.human_task.%'`).Scan(&lifecycleEvents); err != nil || lifecycleEvents != 2 {
		t.Fatalf("PostgreSQL HumanTask lifecycle events = %d, err=%v", lifecycleEvents, err)
	}
	var privateEncrypted []byte
	var decisionEncrypted []byte
	if err := store.pool.QueryRow(context.Background(), `SELECT private_context_encrypted, decision_encrypted FROM human_tasks LIMIT 1`).Scan(&privateEncrypted, &decisionEncrypted); err != nil {
		t.Fatalf("read encrypted HumanTask fields: %v", err)
	}
	for _, stored := range [][]byte{privateEncrypted, decisionEncrypted} {
		if bytes.Contains(stored, []byte("callback-secret")) || bytes.Contains(stored, []byte("otp-secret")) || !wfcrypto.IsEnc(stored) {
			t.Fatalf("PostgreSQL HumanTask value is not protected: %s", stored)
		}
	}
}

func TestPostgresStoreHeldHumanTaskNotificationAcrossInstances(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	writer, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := OpenPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer.ConfigureInputCrypto("human-task-notify-secret", "")
	reader.ConfigureInputCrypto("human-task-notify-secret", "")
	if err := writer.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.pool.Exec(context.Background(), `TRUNCATE webhook_delivery, control_plane_event, job_logs, run_events, human_tasks, jobs, runs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	task := seedHeldHumanTask(t, writer, "postgres-signal")
	changed, cancel := reader.SubscribeHumanTaskChanges(task.ID)
	defer cancel()
	select {
	case <-reader.listenerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL HumanTask listener did not become ready")
	}
	if _, err := writer.DecideHeldHumanTask(context.Background(), "default", task.ID, HumanTaskDecision{
		Outcome: HumanTaskOutcomeSubmit, Value: json.RawMessage(`{}`), IdempotencyKey: "postgres-signal-decision",
		Fingerprint: "postgres-signal-fingerprint", Actor: "operator:test",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("committed PostgreSQL HumanTask change did not wake another Core instance")
	}
}

func seedHeldHumanTask(t *testing.T, store Store, key string) HumanTask {
	t.Helper()
	ctx := context.Background()
	deployment := contract.Deployment{
		Workspace: "default", App: "signal-app", Commit: "signal-commit",
		Actions: map[string]contract.Action{"wait": {Action: "wait", Command: []string{"helper"}}},
	}
	run := NewRun("api", NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-"+key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(time.Minute)
	task, _, err := store.CreateHeldHumanTask(ctx, HumanTask{
		WorkspaceID: "default", RunID: run.ID, JobID: claimed.ID, Attempt: claimed.Attempt,
		Key: key, RequestFingerprint: key + "-fingerprint", Mode: HumanTaskModeHold,
		Kind: "form", Title: "Signal", Schema: json.RawMessage(`{"type":"object"}`), ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func exerciseHeldHumanTaskLifecycle(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	deployment := contract.Deployment{
		Workspace: "default",
		App:       "held-app",
		Commit:    "held-commit",
		Actions: map[string]contract.Action{
			"wait": {Action: "wait", Command: []string{"helper"}},
		},
	}
	run := NewRun("api", NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue returned error: %v", err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-hold", time.Minute)
	if err != nil {
		t.Fatalf("ClaimJob returned error: %v", err)
	}
	expiresAt := time.Now().UTC().Add(time.Minute)
	candidate := HumanTask{
		WorkspaceID:        "default",
		RunID:              run.ID,
		JobID:              claimed.ID,
		Attempt:            claimed.Attempt,
		Key:                "login-otp",
		RequestFingerprint: "request-fingerprint",
		Mode:               HumanTaskModeHold,
		Kind:               "form",
		Title:              "Enter code",
		Schema:             json.RawMessage(`{"type":"object","required":["otp"],"properties":{"otp":{"type":"string"}}}`),
		PrivateContext:     json.RawMessage(`{"callback":"callback-secret"}`),
		ExpiresAt:          &expiresAt,
	}
	created, wasCreated, err := store.CreateHeldHumanTask(ctx, candidate)
	if err != nil || !wasCreated {
		t.Fatalf("CreateHeldHumanTask = created:%v err:%v task:%#v", wasCreated, err, created)
	}
	replayedCreate, wasCreated, err := store.CreateHeldHumanTask(ctx, candidate)
	if err != nil || wasCreated || replayedCreate.ID != created.ID {
		t.Fatalf("idempotent CreateHeldHumanTask = created:%v err:%v task:%#v", wasCreated, err, replayedCreate)
	}
	conflict := candidate
	conflict.RequestFingerprint = "different"
	if _, _, err := store.CreateHeldHumanTask(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting HumanTask create error = %v, want ErrConflict", err)
	}
	if _, err := store.GetHumanTaskForWorkspace(ctx, "other", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace GetHumanTaskForWorkspace error = %v, want ErrNotFound", err)
	}
	decision := HumanTaskDecision{
		Outcome:        HumanTaskOutcomeSubmit,
		Value:          json.RawMessage(`{"otp":"otp-secret"}`),
		IdempotencyKey: "decision-1",
		Fingerprint:    "decision-fingerprint",
		Actor:          "operator:test",
	}
	decided, err := store.DecideHeldHumanTask(ctx, "default", created.ID, decision)
	if err != nil || decided.Task.State != HumanTaskDecided || decided.Replayed {
		t.Fatalf("DecideHeldHumanTask = %#v, err=%v", decided, err)
	}
	replayRequest := decision
	replayRequest.Value = json.RawMessage(`{"otp":"must-not-replace-stored-decision"}`)
	replayedDecision, err := store.DecideHeldHumanTask(ctx, "default", created.ID, replayRequest)
	if err != nil || !replayedDecision.Replayed || string(replayedDecision.Decision.Value) != `{"otp":"otp-secret"}` {
		t.Fatalf("idempotent DecideHeldHumanTask = %#v, err=%v", replayedDecision, err)
	}
	conflictingDecision := decision
	conflictingDecision.IdempotencyKey = "decision-2"
	conflictingDecision.Fingerprint = "different"
	if _, err := store.DecideHeldHumanTask(ctx, "default", created.ID, conflictingDecision); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting decision error = %v, want ErrConflict", err)
	}
	read, err := store.GetHeldHumanTaskDecision(ctx, "default", created.ID)
	if err != nil || read.Decision.Outcome != HumanTaskOutcomeSubmit || string(read.Decision.Value) != `{"otp":"otp-secret"}` {
		t.Fatalf("GetHeldHumanTaskDecision = %#v, err=%v", read, err)
	}
	jobs, err := store.ListJobs(ctx, JobListQuery{WorkspaceID: "default", Limit: 10})
	if err != nil || len(jobs) != 1 || jobs[0].ID != claimed.ID || !jobs[0].Running {
		t.Fatalf("hold created another Job or released the lease: jobs=%#v err=%v", jobs, err)
	}
}

func exerciseHeldHumanTaskTerminalRace(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	deployment := contract.Deployment{
		Workspace: "default", App: "race-app", Commit: "race-commit",
		Actions: map[string]contract.Action{"wait": {Action: "wait", Command: []string{"helper"}}},
	}
	run := NewRun("api", NewID("run"), deployment.App, "wait", deployment, json.RawMessage(`{}`))
	job := NewActionJob(run, json.RawMessage(`{}`))
	if err := store.CreateRunAndEnqueue(ctx, run, job); err != nil {
		t.Fatalf("CreateRunAndEnqueue returned error: %v", err)
	}
	claimed, _, err := store.ClaimJob(ctx, "worker-race", time.Minute)
	if err != nil {
		t.Fatalf("ClaimJob returned error: %v", err)
	}
	expiresAt := time.Now().UTC().Add(time.Minute)
	task, _, err := store.CreateHeldHumanTask(ctx, HumanTask{
		WorkspaceID: "default", RunID: run.ID, JobID: claimed.ID, Attempt: claimed.Attempt,
		Key: "race", RequestFingerprint: "race-request", Mode: HumanTaskModeHold, Kind: "form",
		Title: "Race", Schema: json.RawMessage(`{"type":"object"}`), ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateHeldHumanTask returned error: %v", err)
	}
	start := make(chan struct{})
	errorsByOperation := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, err := store.DecideHeldHumanTask(ctx, "default", task.ID, HumanTaskDecision{
			Outcome: HumanTaskOutcomeSubmit, Value: json.RawMessage(`{}`), IdempotencyKey: "race-decision",
			Fingerprint: "race-fingerprint", Actor: "operator:test",
		})
		errorsByOperation <- err
	}()
	go func() {
		defer wait.Done()
		<-start
		_, err := store.ExpireHeldHumanTask(ctx, "default", task.ID, HumanTaskCauseDeadline)
		errorsByOperation <- err
	}()
	close(start)
	wait.Wait()
	close(errorsByOperation)
	succeeded, rejected := 0, 0
	for err := range errorsByOperation {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrInvalidState) || errors.Is(err, ErrConflict) {
			rejected++
		} else {
			t.Fatalf("terminal race returned unexpected error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("terminal race success/rejection = %d/%d, want 1/1", succeeded, rejected)
	}
	terminal, err := store.GetHumanTaskForWorkspace(ctx, "default", task.ID)
	if err != nil || (terminal.State != HumanTaskDecided && terminal.State != HumanTaskExpired) {
		t.Fatalf("terminal HumanTask = %#v, err=%v", terminal, err)
	}
}
