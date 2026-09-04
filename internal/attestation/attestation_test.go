package attestation

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

var testNow = time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)

func testIssuer(t *testing.T) *Issuer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issuer, err := NewIssuer("synthetic-issuer-key-1", "synthetic-capability-service", 5*time.Minute, private)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}
	return issuer
}

func testBinding() contract.ExecutionAttestationBinding {
	return contract.ExecutionAttestationBinding{
		RunRef:          "run_0123456789abcdef",
		Workspace:       "synthetic",
		App:             "synthetic_app",
		Action:          "verify",
		PublicationRef:  "synthetic-publication",
		RouteGeneration: 7,
		OperationRef:    "operations/synthetic/v1",
		CredentialRef:   contract.ImmutableReference{ID: "credential/synthetic", Version: "sha256:" + repeat("a", 64)},
		Release: contract.ExecutionReleasePin{
			DeploymentID: "deployment-7",
			Commit:       "0123456789abcdef",
			BundleDigest: "sha256:" + repeat("b", 64),
		},
		References: []contract.NamedImmutableReferencePin{
			{Name: "materialVersion", Reference: contract.ImmutableReference{ID: "material/synthetic", Version: "v4"}},
			{Name: "customerExecutionSnapshot", Reference: contract.ImmutableReference{ID: "snapshot/synthetic", Version: "sha256:" + repeat("c", 64)}},
			{Name: "securityEpoch", Reference: contract.ImmutableReference{ID: "grant/synthetic", Version: "3"}},
		},
	}
}

func repeat(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}

func TestCanonicalBindingSortsReferencesAndIsStable(t *testing.T) {
	t.Parallel()
	issuer := testIssuer(t)
	first, err := issuer.Issue(testBinding(), testNow)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	shuffled := testBinding()
	shuffled.References = []contract.NamedImmutableReferencePin{
		shuffled.References[2], shuffled.References[0], shuffled.References[1],
	}
	second, err := issuer.Issue(shuffled, testNow)
	if err != nil {
		t.Fatalf("issue shuffled: %v", err)
	}
	if first.BindingDigest != second.BindingDigest || first.Signature != second.Signature {
		t.Fatal("reference order changed the canonical binding")
	}
	names := make([]string, 0, len(first.Binding.References))
	for _, pin := range first.Binding.References {
		names = append(names, pin.Name)
	}
	if names[0] != "customerExecutionSnapshot" || names[1] != "materialVersion" || names[2] != "securityEpoch" {
		t.Fatalf("references are not name-sorted: %v", names)
	}
}

func TestIssuerOwnsAudienceKeyIdAndExpiry(t *testing.T) {
	t.Parallel()
	issuer := testIssuer(t)
	claimed := testBinding()
	claimed.Audience = "another-service"
	claimed.IssuerKeyID = "another-key"
	claimed.ExpiresAt = testNow.Add(100 * time.Hour).Format(time.RFC3339)

	minted, err := issuer.Issue(claimed, testNow)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if minted.Binding.Audience != "synthetic-capability-service" || minted.Binding.IssuerKeyID != "synthetic-issuer-key-1" {
		t.Fatalf("a caller widened the audience or key: %+v", minted.Binding)
	}
	expiresAt, err := time.Parse(time.RFC3339, minted.Binding.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}
	if !expiresAt.Equal(testNow.Add(5 * time.Minute)) {
		t.Fatalf("expiry = %s, want the issuer lifetime", expiresAt)
	}
	if minted.Kind != contract.ExecutionAttestationKindV1 || minted.Binding.Kind != contract.ExecutionAttestationBindingKindV1 ||
		minted.Algorithm != contract.ExecutionAttestationAlgorithm {
		t.Fatalf("unexpected document identity: %+v", minted)
	}
}

func TestVerifyAcceptsOnlyTheExactBinding(t *testing.T) {
	t.Parallel()
	issuer := testIssuer(t)
	minted, err := issuer.Issue(testBinding(), testNow)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	expect := Expectation{Audience: issuer.Audience(), Binding: testBinding()}
	if err := Verify(minted, issuer.PublicKey(), testNow.Add(time.Minute), expect); err != nil {
		t.Fatalf("verify: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*contract.ExecutionAttestationBinding)
	}{
		{name: "run", mutate: func(b *contract.ExecutionAttestationBinding) { b.RunRef = "run_ffffffffffffffff" }},
		{name: "workspace", mutate: func(b *contract.ExecutionAttestationBinding) { b.Workspace = "other" }},
		{name: "action", mutate: func(b *contract.ExecutionAttestationBinding) { b.Action = "other" }},
		{name: "publication", mutate: func(b *contract.ExecutionAttestationBinding) { b.PublicationRef = "other" }},
		{name: "route generation", mutate: func(b *contract.ExecutionAttestationBinding) { b.RouteGeneration = 8 }},
		{name: "operation", mutate: func(b *contract.ExecutionAttestationBinding) { b.OperationRef = "operations/other/v1" }},
		{name: "credential version", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.CredentialRef.Version = "sha256:" + repeat("d", 64)
		}},
		{name: "release commit", mutate: func(b *contract.ExecutionAttestationBinding) { b.Release.Commit = "fedcba9876543210" }},
		{name: "bundle digest", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.Release.BundleDigest = "sha256:" + repeat("e", 64)
		}},
		// The epoch and the material version reach the attestation as named
		// pins, so a mutated pin version is exactly how those rejections happen.
		{name: "security epoch pin", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.References[2].Reference.Version = "4"
		}},
		{name: "material version pin", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.References[0].Reference.Version = "v5"
		}},
		{name: "snapshot pin", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.References[1].Reference.ID = "snapshot/other"
		}},
	} {
		t.Run("verifier expects another "+test.name, func(t *testing.T) {
			expected := testBinding()
			test.mutate(&expected)
			if err := Verify(minted, issuer.PublicKey(), testNow, Expectation{Audience: issuer.Audience(), Binding: expected}); !errors.Is(err, ErrVerification) {
				t.Fatalf("error = %v, want a verification failure", err)
			}
		})
		t.Run("attestation carries another "+test.name, func(t *testing.T) {
			tampered := minted
			tampered.Binding = contract.CloneExecutionAttestationBinding(minted.Binding)
			test.mutate(&tampered.Binding)
			if err := Verify(tampered, issuer.PublicKey(), testNow, expect); !errors.Is(err, ErrVerification) {
				t.Fatalf("error = %v, want a verification failure", err)
			}
		})
	}
}

func TestVerifyRejectsWrongIssuerAudienceAndExpiry(t *testing.T) {
	t.Parallel()
	issuer := testIssuer(t)
	minted, err := issuer.Issue(testBinding(), testNow)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	expect := Expectation{Audience: issuer.Audience(), Binding: testBinding()}
	other := testIssuer(t)

	for _, test := range []struct {
		name  string
		check func() error
	}{
		{
			name:  "another issuer key",
			check: func() error { return Verify(minted, other.PublicKey(), testNow, expect) },
		},
		{
			name:  "no trusted key",
			check: func() error { return Verify(minted, nil, testNow, expect) },
		},
		{
			name: "another audience",
			check: func() error {
				return Verify(minted, issuer.PublicKey(), testNow, Expectation{Audience: "other", Binding: testBinding()})
			},
		},
		{
			name: "no expected audience",
			check: func() error {
				return Verify(minted, issuer.PublicKey(), testNow, Expectation{Binding: testBinding()})
			},
		},
		{
			name:  "expired",
			check: func() error { return Verify(minted, issuer.PublicKey(), testNow.Add(6*time.Minute), expect) },
		},
		{
			name: "replayed bytes with a tampered signature",
			check: func() error {
				tampered := minted
				tampered.Signature = other.mustSign(t, minted.Binding)
				return Verify(tampered, issuer.PublicKey(), testNow, expect)
			},
		},
		{
			name: "binding digest that does not cover the binding",
			check: func() error {
				tampered := minted
				tampered.BindingDigest = "sha256:" + repeat("f", 64)
				return Verify(tampered, issuer.PublicKey(), testNow, expect)
			},
		},
		{
			name: "another document kind",
			check: func() error {
				tampered := minted
				tampered.Kind = "windforce.execution-attestation/v2"
				return Verify(tampered, issuer.PublicKey(), testNow, expect)
			},
		},
		{
			name: "another algorithm",
			check: func() error {
				tampered := minted
				tampered.Algorithm = "HMAC-SHA256"
				return Verify(tampered, issuer.PublicKey(), testNow, expect)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.check(); !errors.Is(err, ErrVerification) {
				t.Fatalf("error = %v, want a verification failure", err)
			}
		})
	}
}

func (i *Issuer) mustSign(t *testing.T, binding contract.ExecutionAttestationBinding) string {
	t.Helper()
	minted, err := i.Issue(binding, testNow)
	if err != nil {
		t.Fatalf("sign with another issuer: %v", err)
	}
	return minted.Signature
}

func TestValidateBindingRejectsIncompleteStatements(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*contract.ExecutionAttestationBinding)
	}{
		{name: "no run", mutate: func(b *contract.ExecutionAttestationBinding) { b.RunRef = "" }},
		{name: "no operation", mutate: func(b *contract.ExecutionAttestationBinding) { b.OperationRef = "" }},
		{name: "no credential version", mutate: func(b *contract.ExecutionAttestationBinding) { b.CredentialRef.Version = "" }},
		{name: "no bundle digest", mutate: func(b *contract.ExecutionAttestationBinding) { b.Release.BundleDigest = "" }},
		{name: "zero route generation", mutate: func(b *contract.ExecutionAttestationBinding) { b.RouteGeneration = 0 }},
		{name: "padded value", mutate: func(b *contract.ExecutionAttestationBinding) { b.Workspace = " synthetic " }},
		{name: "unparseable expiry", mutate: func(b *contract.ExecutionAttestationBinding) { b.ExpiresAt = "soon" }},
		{name: "duplicate pin", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.References = append(b.References, b.References[0])
		}},
		{name: "empty pin version", mutate: func(b *contract.ExecutionAttestationBinding) {
			b.References[0].Reference.Version = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			binding := testBinding()
			binding.Kind = contract.ExecutionAttestationBindingKindV1
			binding.Audience = "synthetic-capability-service"
			binding.IssuerKeyID = "synthetic-issuer-key-1"
			binding.ExpiresAt = testNow.Format(time.RFC3339)
			test.mutate(&binding)
			if err := ValidateBinding(binding); !errors.Is(err, ErrInvalidBinding) {
				t.Fatalf("error = %v, want an invalid binding", err)
			}
		})
	}
}

func TestIssuerConstructionFailsClosed(t *testing.T) {
	t.Parallel()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, test := range []struct {
		name     string
		keyID    string
		audience string
		ttl      time.Duration
		key      ed25519.PrivateKey
	}{
		{name: "no key id", audience: "service", ttl: time.Minute, key: private},
		{name: "no audience", keyID: "key", ttl: time.Minute, key: private},
		{name: "no key", keyID: "key", audience: "service", ttl: time.Minute},
		{name: "lifetime above the bound", keyID: "key", audience: "service", ttl: MaxTTL + time.Second, key: private},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewIssuer(test.keyID, test.audience, test.ttl, test.key); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want unavailable", err)
			}
		})
	}
	issuer, err := NewIssuer("key", "service", 0, private)
	if err != nil || issuer.ttl != DefaultTTL {
		t.Fatalf("default lifetime = %v, error = %v", issuer.ttl, err)
	}
}

func TestLoadIssuerReadsAPKCS8Key(t *testing.T) {
	t.Parallel()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	directory := t.TempDir()
	keyFile := filepath.Join(directory, "issuer.key")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	issuer, err := LoadIssuer(keyFile, "key", "service", time.Minute)
	if err != nil {
		t.Fatalf("load issuer: %v", err)
	}
	if !issuer.PublicKey().Equal(private.Public()) {
		t.Fatal("loaded issuer holds another key")
	}

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "no reference", path: ""},
		{name: "missing file", path: filepath.Join(directory, "absent.key")},
		{name: "a directory", path: directory},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadIssuer(test.path, "key", "service", time.Minute); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want unavailable", err)
			}
		})
	}

	notAKey := filepath.Join(directory, "other.key")
	if err := os.WriteFile(notAKey, []byte("-----BEGIN PRIVATE KEY-----\nnot-a-key\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if _, err := LoadIssuer(notAKey, "key", "service", time.Minute); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	if _, err := LoadIssuer(keyFile, "key", "service", time.Minute); err != nil {
		t.Fatalf("a usable key must still load: %v", err)
	}
	var absent *Issuer
	if _, err := absent.Issue(testBinding(), testNow); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
}

func TestContractFixtureMatchesTheCanonicalDigest(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "execution-attestation", "v1", "execution-attestation-binding.example.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Binding contract.ExecutionAttestationBinding `json:"binding"`
		Digest  string                               `json:"bindingDigest"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	digest, err := BindingDigest(fixture.Binding)
	if err != nil {
		t.Fatalf("digest fixture: %v", err)
	}
	if digest != fixture.Digest {
		t.Fatalf("fixture digest = %s, computed %s", fixture.Digest, digest)
	}
}
