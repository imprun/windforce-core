package secretbackend

import (
	"context"
	"strings"
	"testing"

	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

type staticKeys struct {
	key     string
	version int32
}

func (s staticKeys) GetWorkspaceKeyVersioned(context.Context, string) (string, int32, error) {
	return s.key, s.version, nil
}

func TestDatabaseUsesWrappedWorkspaceDEK(t *testing.T) {
	wrapped, version, err := wfcrypto.NewWrappedDEK("instance-secret")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(staticKeys{key: wrapped, version: version}, "instance-secret", "")
	reference := Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "api/token"}

	stored, err := backend.Store(context.Background(), reference, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if stored == "secret-value" {
		t.Fatal("database backend stored plaintext")
	}
	if len(stored) < len(boundCiphertextPrefix) || stored[:len(boundCiphertextPrefix)] != boundCiphertextPrefix {
		t.Fatalf("stored value does not use bound ciphertext format: %q", stored)
	}
	resolved, err := backend.Resolve(context.Background(), reference, stored)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "secret-value" {
		t.Fatalf("resolved = %q, want secret-value", resolved)
	}
}

func TestDatabaseBindsCiphertextToWorkspaceKindAndPath(t *testing.T) {
	wrapped, version, err := wfcrypto.NewWrappedDEK("instance-secret")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(staticKeys{key: wrapped, version: version}, "instance-secret", "")
	original := Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "api/token"}
	stored, err := backend.Store(context.Background(), original, "secret-value")
	if err != nil {
		t.Fatal(err)
	}

	for _, changed := range []Reference{
		{WorkspaceID: "ws-b", Kind: "variable", Path: "api/token"},
		{WorkspaceID: "ws-a", Kind: "resource", Path: "api/token"},
		{WorkspaceID: "ws-a", Kind: "variable", Path: "api/other"},
	} {
		if _, err := backend.Resolve(context.Background(), changed, stored); err == nil {
			t.Fatalf("ciphertext resolved for changed reference %#v", changed)
		}
	}
}

func TestRuntimeCandidateEnvelopePreservesBoundReference(t *testing.T) {
	wrapped, version, err := wfcrypto.NewWrappedDEK("instance-secret")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(staticKeys{key: wrapped, version: version}, "instance-secret", "")
	base := Reference{WorkspaceID: "ws-a", Kind: "variable-app", Path: "shop/sessions/playwright"}
	candidateID := "0123456789abcdef0123456789abcdef"
	candidate := base
	candidate.Path += "/" + candidateID
	stored, err := backend.Store(context.Background(), candidate, "session-secret")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealRuntimeCandidate(candidateID, stored)
	if err != nil {
		t.Fatal(err)
	}
	openedReference, openedStored, err := OpenRuntimeCandidate(base, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if openedReference != candidate || openedStored != stored {
		t.Fatalf("opened reference=%#v stored=%q", openedReference, openedStored)
	}
	plaintext, err := backend.Resolve(context.Background(), openedReference, openedStored)
	if err != nil || plaintext != "session-secret" {
		t.Fatalf("resolved=%q err=%v", plaintext, err)
	}
	if _, _, err := OpenRuntimeCandidate(base, runtimeCandidatePrefix+"invalid:payload"); err == nil {
		t.Fatal("malformed runtime candidate envelope was accepted")
	}
}

func TestRuntimeCandidateEnvelopeFitsTwoMiBStorageBound(t *testing.T) {
	backend := NewDatabase(nil, "instance-secret", "")
	reference := Reference{
		WorkspaceID: "ws-a",
		Kind:        "variable-app",
		Path:        "shop/sessions/playwright/0123456789abcdef0123456789abcdef",
	}
	stored, err := backend.Store(context.Background(), reference, strings.Repeat("x", 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealRuntimeCandidate("0123456789abcdef0123456789abcdef", stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) > 2<<20 {
		t.Fatalf("sealed maximum Secret = %d bytes, exceeds storage bound", len(sealed))
	}
}

func TestDatabaseResolvesLegacyUnboundCiphertext(t *testing.T) {
	wrapped, version, err := wfcrypto.NewWrappedDEK("instance-secret")
	if err != nil {
		t.Fatal(err)
	}
	dek, err := wfcrypto.ResolveDEK(wrapped, version, []string{wfcrypto.DeriveKEK("instance-secret")})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := wfcrypto.Encrypt(dek, "legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(staticKeys{key: wrapped, version: version}, "instance-secret", "")
	resolved, err := backend.Resolve(
		context.Background(),
		Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "legacy"},
		legacy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "legacy-value" {
		t.Fatalf("resolved = %q, want legacy-value", resolved)
	}
}

func TestDatabaseResolvesLegacyPreviousSecret(t *testing.T) {
	legacy, err := wfcrypto.Encrypt(wfcrypto.DeriveWorkspaceKey("old-secret", "ws-a"), "legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(nil, "new-secret", "old-secret")

	resolved, err := backend.Resolve(
		context.Background(),
		Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "legacy"},
		legacy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "legacy-value" {
		t.Fatalf("resolved = %q, want legacy-value", resolved)
	}
}

func TestDatabaseResolvesLegacyPreviousSecretWhenKeyProviderHasNoWorkspaceKey(t *testing.T) {
	reference := Reference{WorkspaceID: "ws-a", Kind: "variable", Path: "legacy"}
	legacy, err := wfcrypto.Encrypt(wfcrypto.DeriveWorkspaceKey("old-secret", "ws-a"), "legacy-value")
	if err != nil {
		t.Fatal(err)
	}
	backend := NewDatabase(staticKeys{}, "new-secret", "old-secret")

	resolved, err := backend.Resolve(context.Background(), reference, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "legacy-value" {
		t.Fatalf("resolved = %q, want legacy-value", resolved)
	}
}
