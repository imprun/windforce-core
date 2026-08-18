package state

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/secretbackend"
)

type runtimeSecretCandidateStore interface {
	Store
	secretbackend.RuntimeCandidateLiveReferenceSource
}

func TestLocalRuntimeSecretCandidateLivenessFollowsRestoreLifecycleAndPurge(t *testing.T) {
	exerciseRuntimeSecretCandidateLivenessFollowsRestoreLifecycleAndPurge(
		t,
		NewLocalStore(filepath.Join(t.TempDir(), "state.json")),
	)
}

func TestPostgresRuntimeSecretCandidateLivenessFollowsRestoreLifecycleAndPurge(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	exerciseRuntimeSecretCandidateLivenessFollowsRestoreLifecycleAndPurge(t, openIsolatedPostgresCatalogStore(t, dsn))
}

func exerciseRuntimeSecretCandidateLivenessFollowsRestoreLifecycleAndPurge(t *testing.T, store runtimeSecretCandidateStore) {
	t.Helper()
	ctx := context.Background()
	candidateID := "0123456789abcdef0123456789abcdef"
	sealed, err := secretbackend.SealRuntimeCandidate(candidateID, "opaque-backend-reference")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyRuntimeConfigProvisioningBatch(ctx, RuntimeConfigProvisioningBatch{
		WorkspaceID: "ws-a",
		Actor:       "system:restore",
		Variables: []ProvisionedRuntimeVariable{{
			AppKey: "shop", Path: "session", Value: sealed, IsSecret: true, Revision: 4,
		}},
		Lifecycles: []ProvisionedAppRuntimeLifecycle{{
			AppKey: "shop", State: AppRuntimeTombstoned, Reason: "restored retirement", Revision: 2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	want := secretbackend.Reference{
		WorkspaceID: "ws-a",
		Kind:        "variable-app",
		Path:        "shop/session/" + candidateID,
	}
	assertLiveRuntimeSecretCandidates(t, store, []secretbackend.Reference{want})

	if _, err := store.SetAppRuntimeLifecycle(ctx, SetAppRuntimeLifecycleRequest{
		WorkspaceID: "ws-a", AppKey: "shop", State: AppRuntimeRevoked,
		Actor: "security", Reason: "incident",
	}); err != nil {
		t.Fatal(err)
	}
	assertLiveRuntimeSecretCandidates(t, store, []secretbackend.Reference{want})

	if err := store.PurgeAppRuntimeConfig(ctx, PurgeAppRuntimeConfigRequest{
		WorkspaceID: "ws-a", AppKey: "shop", Actor: "operator", Reason: "retired",
	}); err != nil {
		t.Fatal(err)
	}
	assertLiveRuntimeSecretCandidates(t, store, nil)
	audits, err := store.ListAppRuntimeLifecycleAudit(ctx, "ws-a", "shop")
	if err != nil || len(audits) != 3 || !audits[2].Purged {
		t.Fatalf("lifecycle audits=%#v err=%v", audits, err)
	}
}

func TestLocalRuntimeSecretCandidateLivenessFailsClosedOnMalformedEnvelope(t *testing.T) {
	store := NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err := store.SetVariable(context.Background(), "ws-a", "shop", "session", "wfruntime:v1:invalid:payload", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListLiveRuntimeSecretCandidateReferences(context.Background()); err == nil {
		t.Fatal("malformed candidate envelope did not fail the live-reference snapshot")
	}
}

func assertLiveRuntimeSecretCandidates(t *testing.T, store runtimeSecretCandidateStore, want []secretbackend.Reference) {
	t.Helper()
	got, err := store.ListLiveRuntimeSecretCandidateReferences(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("live references=%#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("live references=%#v, want %#v", got, want)
		}
	}
}
