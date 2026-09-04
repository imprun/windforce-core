// Package attestation mints and verifies the signed execution attestation a
// private downstream capability service can check after Windforce Admission
// accepted an invocation.
//
// The engine only binds references it already pinned. It never resolves,
// opens, or interprets a secret, a material, or a downstream policy, and it
// carries no vocabulary from any particular downstream product.
package attestation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	// MaxTTL bounds how long a minted attestation stays valid. An attestation
	// authorizes one downstream capability exchange for one admitted Run, so a
	// long life adds replay surface without adding capability.
	MaxTTL = 15 * time.Minute
	// DefaultTTL is used when a deployment configures no explicit lifetime.
	DefaultTTL = 5 * time.Minute

	maxKeyFileBytes = 1 << 16
)

var (
	// ErrUnavailable is returned when no usable issuer is configured. It is not
	// an invocation failure: a deployment that mints no attestation simply
	// cannot use a downstream capability service.
	ErrUnavailable = errors.New("execution attestation issuer is unavailable")
	// ErrInvalidBinding rejects a binding that is not a complete, canonical
	// statement.
	ErrInvalidBinding = errors.New("execution attestation binding is invalid")
	// ErrVerification rejects an attestation that does not prove what the
	// verifier requires.
	ErrVerification = errors.New("execution attestation verification failed")
)

// Issuer signs execution attestations with one Ed25519 key for one audience.
type Issuer struct {
	keyID    string
	audience string
	ttl      time.Duration
	key      ed25519.PrivateKey
}

// NewIssuer builds an issuer from a decoded Ed25519 private key.
func NewIssuer(keyID string, audience string, ttl time.Duration, key ed25519.PrivateKey) (*Issuer, error) {
	keyID = strings.TrimSpace(keyID)
	audience = strings.TrimSpace(audience)
	if keyID == "" || audience == "" || len(key) != ed25519.PrivateKeySize {
		return nil, ErrUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		return nil, fmt.Errorf("%w: lifetime above %s", ErrUnavailable, MaxTTL)
	}
	return &Issuer{keyID: keyID, audience: audience, ttl: ttl, key: key}, nil
}

// LoadIssuer reads a PEM-encoded PKCS#8 Ed25519 private key from a file
// reference. The key value itself never appears in configuration, logs, or
// errors.
func LoadIssuer(keyFile string, keyID string, audience string, ttl time.Duration) (*Issuer, error) {
	if strings.TrimSpace(keyFile) == "" {
		return nil, ErrUnavailable
	}
	info, err := os.Stat(keyFile)
	if err != nil || info.IsDir() || info.Size() > maxKeyFileBytes {
		return nil, ErrUnavailable
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, ErrUnavailable
	}
	key, err := parseEd25519PrivateKey(raw)
	if err != nil {
		return nil, err
	}
	return NewIssuer(keyID, audience, ttl, key)
}

func parseEd25519PrivateKey(raw []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, ErrUnavailable
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrUnavailable
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, ErrUnavailable
	}
	return key, nil
}

// Audience is the downstream capability audience this issuer mints for.
func (i *Issuer) Audience() string {
	if i == nil {
		return ""
	}
	return i.audience
}

// KeyID identifies the signing key so a verifier can select a trusted public
// key without trusting the attestation to name itself.
func (i *Issuer) KeyID() string {
	if i == nil {
		return ""
	}
	return i.keyID
}

// PublicKey exposes the verification key of this issuer.
func (i *Issuer) PublicKey() ed25519.PublicKey {
	if i == nil {
		return nil
	}
	public, _ := i.key.Public().(ed25519.PublicKey)
	return public
}

// Issue mints an attestation over the given binding. The issuer owns audience,
// key id, and expiry: a caller cannot widen any of them.
func (i *Issuer) Issue(binding contract.ExecutionAttestationBinding, now time.Time) (contract.ExecutionAttestation, error) {
	if i == nil || len(i.key) != ed25519.PrivateKeySize {
		return contract.ExecutionAttestation{}, ErrUnavailable
	}
	binding.Kind = contract.ExecutionAttestationBindingKindV1
	binding.Audience = i.audience
	binding.IssuerKeyID = i.keyID
	binding.ExpiresAt = now.UTC().Add(i.ttl).Format(time.RFC3339)
	binding = Normalize(binding)
	canonical, err := CanonicalBinding(binding)
	if err != nil {
		return contract.ExecutionAttestation{}, err
	}
	digest := sha256.Sum256(canonical)
	return contract.ExecutionAttestation{
		Kind:          contract.ExecutionAttestationKindV1,
		Binding:       binding,
		BindingDigest: "sha256:" + hex.EncodeToString(digest[:]),
		Algorithm:     contract.ExecutionAttestationAlgorithm,
		Signature:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(i.key, canonical)),
	}, nil
}

// Normalize sorts the reference pins so the canonical encoding does not depend
// on the order a projection happened to store them in.
func Normalize(binding contract.ExecutionAttestationBinding) contract.ExecutionAttestationBinding {
	binding = contract.CloneExecutionAttestationBinding(binding)
	sort.SliceStable(binding.References, func(left, right int) bool {
		return binding.References[left].Name < binding.References[right].Name
	})
	return binding
}

// CanonicalBinding returns the exact bytes a signature covers: the binding
// encoded as JSON in declaration order with sorted reference pins.
func CanonicalBinding(binding contract.ExecutionAttestationBinding) ([]byte, error) {
	if err := ValidateBinding(binding); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(Normalize(binding))
	if err != nil {
		return nil, ErrInvalidBinding
	}
	return canonical, nil
}

// BindingDigest is the SHA-256 of the canonical binding bytes, prefixed with
// the hash name. A verifier may log or compare it without holding the bytes.
func BindingDigest(binding contract.ExecutionAttestationBinding) (string, error) {
	canonical, err := CanonicalBinding(binding)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ValidateBinding rejects an incomplete statement before it can be signed or
// trusted. Every field a verifier compares must be present.
func ValidateBinding(binding contract.ExecutionAttestationBinding) error {
	if binding.Kind != contract.ExecutionAttestationBindingKindV1 {
		return fmt.Errorf("%w: unexpected kind", ErrInvalidBinding)
	}
	for name, value := range map[string]string{
		"audience":       binding.Audience,
		"issuerKeyId":    binding.IssuerKeyID,
		"expiresAt":      binding.ExpiresAt,
		"runRef":         binding.RunRef,
		"workspace":      binding.Workspace,
		"app":            binding.App,
		"action":         binding.Action,
		"publicationRef": binding.PublicationRef,
		"operationRef":   binding.OperationRef,
		"credentialId":   binding.CredentialRef.ID,
		"credentialVer":  binding.CredentialRef.Version,
		"commit":         binding.Release.Commit,
		"bundleDigest":   binding.Release.BundleDigest,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s", ErrInvalidBinding, name)
		}
	}
	if binding.RouteGeneration < 1 {
		return fmt.Errorf("%w: routeGeneration", ErrInvalidBinding)
	}
	if _, err := time.Parse(time.RFC3339, binding.ExpiresAt); err != nil {
		return fmt.Errorf("%w: expiresAt", ErrInvalidBinding)
	}
	seen := make(map[string]struct{}, len(binding.References))
	for _, pin := range binding.References {
		if strings.TrimSpace(pin.Name) == "" || strings.TrimSpace(pin.Reference.ID) == "" || strings.TrimSpace(pin.Reference.Version) == "" {
			return fmt.Errorf("%w: reference pin", ErrInvalidBinding)
		}
		if _, duplicate := seen[pin.Name]; duplicate {
			return fmt.Errorf("%w: duplicate reference pin %s", ErrInvalidBinding, pin.Name)
		}
		seen[pin.Name] = struct{}{}
	}
	return nil
}

// Expectation is what a downstream verifier already knows from its own policy.
// Verification is exact equality: the verifier never learns a value from the
// attestation, it only confirms the one it holds.
type Expectation struct {
	Audience string
	Binding  contract.ExecutionAttestationBinding
}

// Verify proves that an attestation was signed by the given trusted key, is
// unexpired, and binds exactly the references the verifier expects. It is the
// reference implementation of the contract; a downstream service may implement
// the same checks in its own language.
func Verify(attestation contract.ExecutionAttestation, trusted ed25519.PublicKey, now time.Time, expect Expectation) error {
	if attestation.Kind != contract.ExecutionAttestationKindV1 {
		return fmt.Errorf("%w: unexpected document kind", ErrVerification)
	}
	if attestation.Algorithm != contract.ExecutionAttestationAlgorithm {
		return fmt.Errorf("%w: unexpected algorithm", ErrVerification)
	}
	if len(trusted) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: no trusted issuer key", ErrVerification)
	}
	canonical, err := CanonicalBinding(attestation.Binding)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrVerification, err)
	}
	digest := sha256.Sum256(canonical)
	if attestation.BindingDigest != "sha256:"+hex.EncodeToString(digest[:]) {
		return fmt.Errorf("%w: binding digest does not cover the binding", ErrVerification)
	}
	signature, err := base64.RawURLEncoding.DecodeString(attestation.Signature)
	if err != nil || !ed25519.Verify(trusted, canonical, signature) {
		return fmt.Errorf("%w: signature", ErrVerification)
	}
	expiresAt, err := time.Parse(time.RFC3339, attestation.Binding.ExpiresAt)
	if err != nil || !now.UTC().Before(expiresAt) {
		return fmt.Errorf("%w: expired", ErrVerification)
	}
	if strings.TrimSpace(expect.Audience) == "" || attestation.Binding.Audience != expect.Audience {
		return fmt.Errorf("%w: audience", ErrVerification)
	}
	expected := Normalize(expect.Binding)
	expected.Kind = contract.ExecutionAttestationBindingKindV1
	expected.Audience = attestation.Binding.Audience
	expected.IssuerKeyID = attestation.Binding.IssuerKeyID
	expected.ExpiresAt = attestation.Binding.ExpiresAt
	expectedCanonical, err := CanonicalBinding(expected)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrVerification, err)
	}
	if string(expectedCanonical) != string(canonical) {
		return fmt.Errorf("%w: binding does not match the verifier policy", ErrVerification)
	}
	return nil
}
