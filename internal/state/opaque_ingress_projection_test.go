package state

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

type opaqueIngressProjectionTestStore interface {
	PutOpaqueIngressCredentialSnapshot(context.Context, OpaqueIngressCredentialSnapshotRequest) (OpaqueIngressCredentialSnapshot, bool, error)
	RevokeOpaqueIngressCredentialSnapshot(context.Context, OpaqueIngressCredentialRevocationRequest) (OpaqueIngressCredentialRevocation, bool, error)
	PutOpaqueIngressPublicationRevision(context.Context, OpaqueIngressPublicationRevisionRequest) (OpaqueIngressPublicationRevision, bool, error)
	ActivateOpaqueIngressPublication(context.Context, OpaqueIngressActivationRequest) (OpaqueIngressActivation, bool, error)
	ResolveOpaqueIngressProjection(context.Context, OpaqueIngressResolutionRequest) (OpaqueIngressResolvedProjection, error)
	ListOpaqueIngressProjectionAudit(context.Context, string, string, int) ([]OpaqueIngressAudit, error)
	PruneOpaqueIngressProjectionHistory(context.Context, OpaqueIngressRetentionRequest) (OpaqueIngressRetentionResult, bool, error)
	PublishRelease(context.Context, contract.Deployment, time.Time) (catalog.ReleasePublication, error)
}

func TestLocalOpaqueIngressProjectionStoreContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewLocalStore(path)
	restartRequest := exerciseOpaqueIngressProjectionStore(t, store)

	restarted := NewLocalStore(path)
	if _, err := restarted.ResolveOpaqueIngressProjection(context.Background(), restartRequest); err != nil {
		t.Fatalf("resolve after Local restart: %v", err)
	}
	audits, err := restarted.ListOpaqueIngressProjectionAudit(context.Background(), contract.DefaultWorkspace, "identity-check", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 5 {
		t.Fatalf("restart audit count = %d, want durable history", len(audits))
	}
}

func TestPostgresOpaqueIngressProjectionStoreContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("PostgreSQL test DSN is not configured")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	restartRequest := exerciseOpaqueIngressProjectionStore(t, store)

	connectionString := store.pool.Config().ConnString()
	store.Close()
	restarted, err := OpenPostgresStore(context.Background(), connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if _, err := restarted.ResolveOpaqueIngressProjection(context.Background(), restartRequest); err != nil {
		t.Fatalf("resolve after PostgreSQL Store restart: %v", err)
	}
	audits, err := restarted.ListOpaqueIngressProjectionAudit(context.Background(), contract.DefaultWorkspace, "identity-check", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 5 {
		t.Fatalf("restart audit count = %d, want durable history", len(audits))
	}
}

func TestPostgresOpaqueIngressPublicationAndPruneLinearize(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("PostgreSQL test DSN is not configured")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Millisecond)
	release := publishOpaqueIngressTestRelease(t, store, "release-publish-prune", "commit-publish-prune", "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", now)
	credentialRequest := opaqueIngressCredentialRequest(now, "publish-prune-customer", opaqueIngressTestRevision("publish-prune"), "publish-prune-credential")
	credentialRequest.NotAfter = now.Add(time.Hour)
	credentialRequest.Reference.Digest = OpaqueIngressCredentialSnapshotDigest(opaqueIngressCredentialSnapshotFromRequest(credentialRequest))
	credential, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, credentialRequest)
	if err != nil {
		t.Fatal(err)
	}

	// Hold the exact credential row so publication can acquire the workspace
	// retention lock and then pause while validating its immutable pin.
	blocker, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blockerOpen := true
	t.Cleanup(func() {
		if blockerOpen {
			_ = blocker.Rollback(context.Background())
		}
	})
	var locked int
	if err := blocker.QueryRow(ctx, `
SELECT 1
FROM opaque_ingress_credential_snapshot
WHERE workspace_id=$1 AND issuer=$2 AND audience=$3
  AND credential_id=$4 AND credential_revision=$5
FOR UPDATE
`, contract.DefaultWorkspace, "gateway.test", "opaque.test", credential.Reference.ID, credential.Reference.Revision).Scan(&locked); err != nil {
		t.Fatal(err)
	}

	publicationRequest := opaqueIngressPublicationRequest(now, "publish-prune-revision", "publish-prune-publication", release, credential.Reference)
	type publicationOutcome struct {
		publication OpaqueIngressPublicationRevision
		err         error
	}
	publicationDone := make(chan publicationOutcome, 1)
	go func() {
		publication, _, err := store.PutOpaqueIngressPublicationRevision(ctx, publicationRequest)
		publicationDone <- publicationOutcome{publication: publication, err: err}
	}()

	// Observe that publication owns the same transaction-scoped advisory lock
	// used by retention before starting the competing prune.
	retentionLockKey := "retention\x1f" + contract.DefaultWorkspace
	lockObserved := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		var acquired bool
		if err := store.pool.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, retentionLockKey).Scan(&acquired); err != nil {
			t.Fatal(err)
		}
		if !acquired {
			lockObserved = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !lockObserved {
		t.Fatal("publication did not acquire the workspace retention lock")
	}

	type retentionOutcome struct {
		result OpaqueIngressRetentionResult
		replay bool
		err    error
	}
	retentionStarted := make(chan struct{})
	retentionDone := make(chan retentionOutcome, 1)
	go func() {
		close(retentionStarted)
		result, replay, err := store.PruneOpaqueIngressProjectionHistory(ctx, OpaqueIngressRetentionRequest{
			WorkspaceID: contract.DefaultWorkspace, Before: now.Add(2 * time.Hour), Limit: 10,
			OperationID: "publish-prune-retention", RequestFingerprint: "publish-prune-retention-fingerprint", Actor: "operator:test",
		})
		retentionDone <- retentionOutcome{result: result, replay: replay, err: err}
	}()
	<-retentionStarted
	select {
	case outcome := <-retentionDone:
		t.Fatalf("retention completed before publication released its lock: result=%#v replay=%v err=%v", outcome.result, outcome.replay, outcome.err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	blockerOpen = false
	publication := <-publicationDone
	if publication.err != nil || publication.publication.Revision != publicationRequest.Revision.Revision {
		t.Fatalf("publication outcome = %#v err=%v", publication.publication, publication.err)
	}
	retention := <-retentionDone
	if retention.err != nil || retention.replay || retention.result.PublicationRevisions != 0 || retention.result.CredentialSnapshots != 0 {
		t.Fatalf("retention outcome = %#v replay=%v err=%v", retention.result, retention.replay, retention.err)
	}

	var credentialCount int
	if err := store.pool.QueryRow(ctx, `
SELECT count(*)
FROM opaque_ingress_credential_snapshot
WHERE workspace_id=$1 AND issuer=$2 AND audience=$3
  AND credential_id=$4 AND credential_revision=$5
`, contract.DefaultWorkspace, "gateway.test", "opaque.test", credential.Reference.ID, credential.Reference.Revision).Scan(&credentialCount); err != nil {
		t.Fatal(err)
	}
	if credentialCount != 1 {
		t.Fatalf("credential count after publish-vs-prune = %d, want 1", credentialCount)
	}
}

func TestLocalOpaqueIngressConcurrentStoreHandlesPreserveCAS(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewLocalStore(path)
	second := NewLocalStore(path)
	now := time.Now().UTC().Truncate(time.Millisecond)
	release := publishOpaqueIngressTestRelease(t, first, "release-local-race", "commit-local-race", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", now)
	credentialRequest := opaqueIngressCredentialRequest(now, "local-race-customer", opaqueIngressTestRevision("local-race"), "local-race-credential")
	credential, _, err := first.PutOpaqueIngressCredentialSnapshot(ctx, credentialRequest)
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"local-race-one", "local-race-two"} {
		if _, _, err := first.PutOpaqueIngressPublicationRevision(ctx, opaqueIngressPublicationRequest(now, revision, revision+"-operation", release, credential.Reference)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := first.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 0, TargetRevision: "local-race-one",
		Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
		OperationID: "local-race-initial", RequestFingerprint: "local-race-initial-fingerprint", Actor: "operator:test",
	}); err != nil {
		t.Fatal(err)
	}

	stores := []*LocalStore{first, second}
	results := make(chan error, 12)
	var group sync.WaitGroup
	for index := 0; index < 12; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, _, err := stores[index%len(stores)].ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
				WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
				PublicationRef: "identity-check", ExpectedGeneration: 1, TargetRevision: "local-race-two",
				Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
				OperationID: fmt.Sprintf("local-handle-race-%02d", index), RequestFingerprint: fmt.Sprintf("local-handle-race-fingerprint-%02d", index), Actor: "operator:test",
			})
			results <- err
		}(index)
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatalf("concurrent Local handle error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Local handle successes = %d, want 1", successes)
	}
	restarted := NewLocalStore(path)
	snapshot, err := restarted.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := snapshot.OpaqueIngressHeads[opaqueIngressHeadKey("gateway.test", "opaque.test", "identity-check")]
	if head.Generation != 2 || head.Revision != "local-race-two" {
		t.Fatalf("restart head = %#v", head)
	}
}

func exerciseOpaqueIngressProjectionStore(t *testing.T, store opaqueIngressProjectionTestStore) OpaqueIngressResolutionRequest {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	release := publishOpaqueIngressTestRelease(t, store, "release-v1", "commit-v1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now)

	credentialOneRequest := opaqueIngressCredentialRequest(now, "customer-one", opaqueIngressTestRevision("customer-one"), "credential-one")
	invalidRevision := credentialOneRequest
	invalidRevision.Reference.Revision = "v1"
	if _, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, invalidRevision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("non-digest credential revision err=%v, want ErrInvalidState", err)
	}
	nonASCIICredential := credentialOneRequest
	nonASCIICredential.Issuer = "g\u00e2teway.test"
	if _, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, nonASCIICredential); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("non-ASCII credential material err=%v, want ErrInvalidState", err)
	}
	tamperedCredential := credentialOneRequest
	tamperedCredential.OperationRef = "identity/other"
	if _, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, tamperedCredential); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("tampered credential digest err=%v, want rejection", err)
	}
	credentialOne, replay, err := store.PutOpaqueIngressCredentialSnapshot(ctx, credentialOneRequest)
	if err != nil || replay {
		t.Fatalf("put credential one replay=%v err=%v", replay, err)
	}
	if replayed, replay, err := store.PutOpaqueIngressCredentialSnapshot(ctx, credentialOneRequest); err != nil || !replay || replayed.Reference != credentialOne.Reference {
		t.Fatalf("credential replay = %#v replay=%v err=%v", replayed, replay, err)
	}
	conflictingCredential := credentialOneRequest
	conflictingCredential.RequestFingerprint = "different-fingerprint"
	if _, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, conflictingCredential); !errors.Is(err, ErrConflict) {
		t.Fatalf("credential conflicting replay err=%v, want ErrConflict", err)
	}

	credentialTwoRequest := opaqueIngressCredentialRequest(now, "customer-two", opaqueIngressTestRevision("customer-two"), "credential-two")
	credentialTwo, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, credentialTwoRequest)
	if err != nil {
		t.Fatal(err)
	}

	revisionOneRequest := opaqueIngressPublicationRequest(now, "revision-one", "publication-one", release, credentialOne.Reference, credentialTwo.Reference)
	overBoundPublication := revisionOneRequest
	overBoundPublication.OperationID = "publication-over-bound"
	overBoundPublication.RequestFingerprint = "publication-over-bound-fingerprint"
	overBoundPublication.Revision.CredentialRefs = make([]OpaqueIngressCredentialSnapshotRef, 65)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, overBoundPublication); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("over-bound credential refs err=%v, want ErrInvalidState", err)
	}
	invalidReleaseDigest := revisionOneRequest
	invalidReleaseDigest.Revision.Release.BundleDigest = "bundle-v1"
	invalidReleaseDigest.Revision.Digest = OpaqueIngressPublicationRevisionDigest(invalidReleaseDigest.Revision)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, invalidReleaseDigest); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("non-digest Release bundle err=%v, want ErrInvalidState", err)
	}
	tamperedPublication := revisionOneRequest
	tamperedPublication.Revision.HTTP.ExactEscapedPath = "/v2/tampered"
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, tamperedPublication); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("tampered publication digest err=%v, want rejection", err)
	}
	nonASCIIPath := revisionOneRequest
	nonASCIIPath.Revision.HTTP.ExactEscapedPath = "/v2/\uac80\uc99d"
	nonASCIIPath.Revision.Digest = OpaqueIngressPublicationRevisionDigest(nonASCIIPath.Revision)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, nonASCIIPath); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("non-ASCII publication material err=%v, want ErrInvalidState", err)
	}
	mixedReferences := opaqueIngressPublicationRequest(now, "mixed-references", "mixed-references-operation", release, credentialOne.Reference)
	mixedReferences.Revision.References[0].Name = "customerProfile"
	mixedReferences.Revision.Digest = OpaqueIngressPublicationRevisionDigest(mixedReferences.Revision)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, mixedReferences); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("mixed reference names err=%v, want rejection", err)
	}
	revisionOne, replay, err := store.PutOpaqueIngressPublicationRevision(ctx, revisionOneRequest)
	if err != nil || replay {
		t.Fatalf("put publication one replay=%v err=%v", replay, err)
	}
	revisionOne.CredentialRefs[0].ID = "mutated-by-caller"
	replayedRevision, replay, err := store.PutOpaqueIngressPublicationRevision(ctx, revisionOneRequest)
	if err != nil || !replay || replayedRevision.CredentialRefs[0].ID == "mutated-by-caller" {
		t.Fatalf("publication replay/immutability replay=%v err=%v value=%#v", replay, err, replayedRevision)
	}

	activationOneRequest := OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 0, TargetRevision: "revision-one",
		Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
		OperationID: "activation-one", RequestFingerprint: "activation-one-fingerprint", Actor: "operator:test",
	}
	activationOne, replay, err := store.ActivateOpaqueIngressPublication(ctx, activationOneRequest)
	if err != nil || replay || activationOne.Generation != 1 {
		t.Fatalf("activate one = %#v replay=%v err=%v", activationOne, replay, err)
	}
	if replayed, replay, err := store.ActivateOpaqueIngressPublication(ctx, activationOneRequest); err != nil || !replay || replayed.Generation != 1 {
		t.Fatalf("activation replay = %#v replay=%v err=%v", replayed, replay, err)
	}
	changedAuthorization := activationOneRequest
	changedAuthorization.AuthorizedTarget = "other/run"
	if _, _, err := store.ActivateOpaqueIngressPublication(ctx, changedAuthorization); !errors.Is(err, ErrConflict) {
		t.Fatalf("activation authorization replay err=%v, want ErrConflict", err)
	}

	resolved := resolveOpaqueIngressTestRequest(now, credentialOne.Reference, 1)
	projection, err := store.ResolveOpaqueIngressProjection(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Publication.CredentialRefs) != 2 || projection.Credential.Reference.ID != credentialOne.Reference.ID {
		t.Fatalf("resolved N-credential projection = %#v", projection)
	}
	staleGeneration := resolved
	staleGeneration.RouteGeneration = 2
	if _, err := store.ResolveOpaqueIngressProjection(ctx, staleGeneration); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("future generation err=%v, want rejection", err)
	}
	futureClock := resolved
	futureClock.Now = now.Add(-2 * time.Hour)
	if _, err := store.ResolveOpaqueIngressProjection(ctx, futureClock); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("future projection err=%v, want rejection", err)
	}
	expiredClock := resolved
	expiredClock.Now = now.Add(3 * time.Hour)
	if _, err := store.ResolveOpaqueIngressProjection(ctx, expiredClock); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("expired projection err=%v, want rejection", err)
	}

	revisionTwoRequest := opaqueIngressPublicationRequest(now, "revision-two", "publication-two", release, credentialOne.Reference, credentialTwo.Reference)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, revisionTwoRequest); err != nil {
		t.Fatal(err)
	}

	type activationOutcome struct {
		activation OpaqueIngressActivation
		err        error
	}
	outcomes := make(chan activationOutcome, 16)
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			activation, _, err := store.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
				WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
				PublicationRef: "identity-check", ExpectedGeneration: 1, TargetRevision: "revision-two",
				Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
				OperationID: fmt.Sprintf("activation-race-%02d", index), RequestFingerprint: fmt.Sprintf("activation-race-fingerprint-%02d", index), Actor: "operator:test",
			})
			outcomes <- activationOutcome{activation: activation, err: err}
		}(index)
	}
	group.Wait()
	close(outcomes)
	successes := 0
	for outcome := range outcomes {
		if outcome.err == nil {
			successes++
			if outcome.activation.Generation != 2 {
				t.Fatalf("winning race generation = %d", outcome.activation.Generation)
			}
		} else if !errors.Is(outcome.err, ErrConflict) {
			t.Fatalf("race error = %v, want ErrConflict", outcome.err)
		}
	}
	if successes != 1 {
		t.Fatalf("activation race successes = %d, want 1", successes)
	}

	rollback, _, err := store.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 2, TargetRevision: "revision-one",
		Kind: OpaqueIngressActivationKindRollback, AuthorizedTarget: "opaque_app/run",
		OperationID: "rollback-one", RequestFingerprint: "rollback-one-fingerprint", Actor: "operator:test",
	})
	if err != nil || rollback.Generation != 3 || rollback.Revision != "revision-one" {
		t.Fatalf("rollback = %#v err=%v", rollback, err)
	}
	unauthorized := OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 3, TargetRevision: "revision-two",
		Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "other/run",
		OperationID: "unauthorized-activation", RequestFingerprint: "unauthorized-fingerprint", Actor: "service:test",
	}
	if _, _, err := store.ActivateOpaqueIngressPublication(ctx, unauthorized); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("unauthorized activation err=%v, want rejection", err)
	}

	if _, _, err := store.RevokeOpaqueIngressCredentialSnapshot(ctx, OpaqueIngressCredentialRevocationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		Reference: credentialOne.Reference, Reason: "test revoke", OperationID: "revoke-credential-one",
		RequestFingerprint: "revoke-credential-one-fingerprint", Actor: "operator:test",
	}); err != nil {
		t.Fatal(err)
	}
	resolved.RouteGeneration = 3
	if _, err := store.ResolveOpaqueIngressProjection(ctx, resolved); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("revoked credential resolution err=%v, want rejection", err)
	}
	resolvedTwo := resolveOpaqueIngressTestRequest(now, credentialTwo.Reference, 3)
	if _, err := store.ResolveOpaqueIngressProjection(ctx, resolvedTwo); err != nil {
		t.Fatalf("unrevoked credential after peer revoke: %v", err)
	}

	releaseTwo := publishOpaqueIngressTestRelease(t, store, "release-v2", "commit-v2", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", now.Add(time.Second))
	if _, err := store.ResolveOpaqueIngressProjection(ctx, resolvedTwo); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("release drift resolution err=%v, want rejection", err)
	}
	revisionThreeRequest := opaqueIngressPublicationRequest(now, "revision-three", "publication-three", releaseTwo, credentialTwo.Reference)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, revisionThreeRequest); err != nil {
		t.Fatal(err)
	}
	if activated, _, err := store.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 3, TargetRevision: "revision-three",
		Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
		OperationID: "activate-release-two", RequestFingerprint: "activate-release-two-fingerprint", Actor: "operator:test",
	}); err != nil || activated.Generation != 4 {
		t.Fatalf("activate release two = %#v err=%v", activated, err)
	}
	resolvedTwo.RouteGeneration = 4
	if _, err := store.ResolveOpaqueIngressProjection(ctx, resolvedTwo); err != nil {
		t.Fatalf("resolve republished Release: %v", err)
	}

	revokedRoute, _, err := store.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 4, Kind: OpaqueIngressActivationKindRevoke,
		AuthorizedTarget: "opaque_app/run", OperationID: "revoke-route", RequestFingerprint: "revoke-route-fingerprint", Actor: "operator:test",
	})
	if err != nil || revokedRoute.Generation != 5 || revokedRoute.State != OpaqueIngressActivationRevoked {
		t.Fatalf("route revoke = %#v err=%v", revokedRoute, err)
	}
	if _, err := store.ResolveOpaqueIngressProjection(ctx, resolvedTwo); !errors.Is(err, ErrOpaqueIngressProjectionRejected) {
		t.Fatalf("revoked route resolution err=%v, want rejection", err)
	}
	if reactivated, _, err := store.ActivateOpaqueIngressPublication(ctx, OpaqueIngressActivationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", ExpectedGeneration: 5, TargetRevision: "revision-three",
		Kind: OpaqueIngressActivationKindActivate, AuthorizedTarget: "opaque_app/run",
		OperationID: "reactivate-route", RequestFingerprint: "reactivate-route-fingerprint", Actor: "operator:test",
	}); err != nil || reactivated.Generation != 6 {
		t.Fatalf("reactivate route = %#v err=%v", reactivated, err)
	}
	resolvedTwo.RouteGeneration = 6

	oldCredentialRequest := opaqueIngressCredentialRequest(now.Add(-4*time.Hour), "old-customer", opaqueIngressTestRevision("old-customer"), "old-credential")
	oldCredentialRequest.NotAfter = now.Add(-2 * time.Hour)
	oldCredentialRequest.MaxStalenessSeconds = 3600
	oldCandidate := opaqueIngressCredentialSnapshotFromRequest(oldCredentialRequest)
	oldCredentialRequest.Reference.Digest = OpaqueIngressCredentialSnapshotDigest(oldCandidate)
	oldCredential, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, oldCredentialRequest)
	if err != nil {
		t.Fatal(err)
	}
	oldPublicationRequest := opaqueIngressPublicationRequest(now.Add(-4*time.Hour), "old-revision", "old-publication", release, oldCredential.Reference)
	oldPublicationRequest.Revision.NotAfter = now.Add(-2 * time.Hour)
	oldPublicationRequest.Revision.RetainUntil = now.Add(-time.Hour)
	oldPublicationRequest.Revision.MaxStalenessSeconds = 3600
	oldPublicationRequest.Revision.Digest = OpaqueIngressPublicationRevisionDigest(oldPublicationRequest.Revision)
	if _, _, err := store.PutOpaqueIngressPublicationRevision(ctx, oldPublicationRequest); err != nil {
		t.Fatal(err)
	}
	oldRevokedRequest := opaqueIngressCredentialRequest(now.Add(-4*time.Hour), "old-revoked-customer", opaqueIngressTestRevision("old-revoked-customer"), "old-revoked-credential")
	oldRevokedRequest.NotAfter = now.Add(-2 * time.Hour)
	oldRevokedRequest.MaxStalenessSeconds = 3600
	oldRevokedRequest.Reference.Digest = OpaqueIngressCredentialSnapshotDigest(opaqueIngressCredentialSnapshotFromRequest(oldRevokedRequest))
	oldRevoked, _, err := store.PutOpaqueIngressCredentialSnapshot(ctx, oldRevokedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RevokeOpaqueIngressCredentialSnapshot(ctx, OpaqueIngressCredentialRevocationRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		Reference: oldRevoked.Reference, Reason: "retain tombstone", OperationID: "revoke-old-credential",
		RequestFingerprint: "revoke-old-credential-fingerprint", Actor: "operator:test",
	}); err != nil {
		t.Fatal(err)
	}
	retentionRequest := OpaqueIngressRetentionRequest{
		WorkspaceID: contract.DefaultWorkspace, Before: time.Now().UTC().Add(time.Minute), Limit: 10,
		OperationID: "retention-one", RequestFingerprint: "retention-one-fingerprint", Actor: "operator:test",
	}
	pruned, replay, err := store.PruneOpaqueIngressProjectionHistory(ctx, retentionRequest)
	if err != nil || replay || pruned.PublicationRevisions != 1 || pruned.CredentialSnapshots != 1 {
		t.Fatalf("retention = %#v replay=%v err=%v", pruned, replay, err)
	}
	if replayedPrune, replay, err := store.PruneOpaqueIngressProjectionHistory(ctx, retentionRequest); err != nil || !replay || replayedPrune != pruned {
		t.Fatalf("retention replay = %#v replay=%v err=%v, want %#v", replayedPrune, replay, err, pruned)
	}
	retained, replay, err := store.PruneOpaqueIngressProjectionHistory(ctx, OpaqueIngressRetentionRequest{
		WorkspaceID: contract.DefaultWorkspace, Before: now.Add(4 * time.Hour), Limit: 100,
		OperationID: "retention-activated", RequestFingerprint: "retention-activated-fingerprint", Actor: "operator:test",
	})
	if err != nil || replay || retained.PublicationRevisions != 0 || retained.CredentialSnapshots != 0 {
		t.Fatalf("activated history retention = %#v replay=%v err=%v", retained, replay, err)
	}

	audits, err := store.ListOpaqueIngressProjectionAudit(ctx, contract.DefaultWorkspace, "identity-check", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 5 {
		t.Fatalf("audit count = %d, want immutable mutation history", len(audits))
	}
	return resolvedTwo
}

func opaqueIngressCredentialRequest(now time.Time, id, revision, operationID string) OpaqueIngressCredentialSnapshotRequest {
	request := OpaqueIngressCredentialSnapshotRequest{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		Reference:    OpaqueIngressCredentialSnapshotRef{ID: id, Revision: revision},
		OperationRef: "identity/verify",
		References:   []contract.NamedImmutableReferencePin{{Name: "customerProfile", Reference: contract.ImmutableReference{ID: id, Version: revision}}},
		ProjectedAt:  now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), MaxStalenessSeconds: 7200,
		OperationID: operationID, RequestFingerprint: operationID + "-fingerprint", Actor: "operator:test",
	}
	request.Reference.Digest = OpaqueIngressCredentialSnapshotDigest(opaqueIngressCredentialSnapshotFromRequest(request))
	return request
}

func opaqueIngressTestRevision(seed string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(seed)))
}

func opaqueIngressCredentialSnapshotFromRequest(request OpaqueIngressCredentialSnapshotRequest) OpaqueIngressCredentialSnapshot {
	return OpaqueIngressCredentialSnapshot{
		WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
		Reference: request.Reference, OperationRef: request.OperationRef,
		References: request.References, ProjectedAt: request.ProjectedAt, NotAfter: request.NotAfter,
		MaxStalenessSeconds: request.MaxStalenessSeconds,
	}
}

func opaqueIngressPublicationRequest(now time.Time, revision, operationID string, release OpaqueIngressReleasePin, credentials ...OpaqueIngressCredentialSnapshotRef) OpaqueIngressPublicationRevisionRequest {
	value := OpaqueIngressPublicationRevision{
		WorkspaceID: contract.DefaultWorkspace, Issuer: "gateway.test", Audience: "opaque.test",
		PublicationRef: "identity-check", Revision: revision, App: "opaque_app", Action: "run", Release: release,
		HTTP: OpaqueIngressHTTPContract{
			Method: "POST", ExactEscapedPath: "/v2/identity/check", ContentType: "application/json", MaxRequestBodyBytes: 4096,
			ResponsePolicy: contract.HTTPPolicy{ContentTypes: []string{"application/json"}, MaxBodyBytes: 4096},
		},
		OperationRef: "identity/verify", CredentialRefs: credentials,
		References:  []contract.NamedImmutableReferencePin{{Name: "routeSchema", Reference: contract.ImmutableReference{ID: "identity-schema", Version: "v1"}}},
		ProjectedAt: now.Add(-time.Minute), NotAfter: now.Add(2 * time.Hour), MaxStalenessSeconds: 7200,
		RetainUntil: now.Add(3 * time.Hour),
	}
	value.Digest = OpaqueIngressPublicationRevisionDigest(value)
	return OpaqueIngressPublicationRevisionRequest{Revision: value, OperationID: operationID, RequestFingerprint: operationID + "-fingerprint", Actor: "operator:test"}
}

func resolveOpaqueIngressTestRequest(now time.Time, credential OpaqueIngressCredentialSnapshotRef, generation int64) OpaqueIngressResolutionRequest {
	return OpaqueIngressResolutionRequest{
		Issuer: "gateway.test", Audience: "opaque.test", PublicationRef: "identity-check",
		RouteGeneration: generation, CredentialID: credential.ID, CredentialRevision: credential.Revision,
		Method: "POST", ExactEscapedPath: "/v2/identity/check", ContentType: "application/json",
		BodyByteLength: 2, Now: now,
	}
}

func publishOpaqueIngressTestRelease(t *testing.T, store opaqueIngressProjectionTestStore, deploymentID, commit, digest string, at time.Time) OpaqueIngressReleasePin {
	t.Helper()
	deployment := releaseCatalogDeployment(contract.DefaultWorkspace, "opaque-source", "opaque_app", commit)
	deployment.DeploymentID = &deploymentID
	deployment.BundleDigest = digest
	published, err := store.PublishRelease(context.Background(), deployment, at)
	if err != nil {
		t.Fatal(err)
	}
	return OpaqueIngressReleasePin{DeploymentID: deploymentID, Commit: published.Deployment.Commit, BundleDigest: published.Deployment.BundleDigest}
}
