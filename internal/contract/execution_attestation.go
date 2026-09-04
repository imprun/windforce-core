package contract

const (
	// ExecutionAttestationKindV1 is the signed execution attestation document.
	ExecutionAttestationKindV1 = "windforce.execution-attestation/v1"
	// ExecutionAttestationBindingKindV1 is the canonical value the signature
	// covers. It is a separate kind so a verifier cannot be tricked into
	// checking a signature over a differently shaped document.
	ExecutionAttestationBindingKindV1 = "windforce.execution-attestation-binding/v1"
	// ExecutionAttestationAlgorithm is the only signature algorithm this
	// contract defines.
	ExecutionAttestationAlgorithm = "Ed25519"
)

// ExecutionReleasePin is the exact Release an admitted Run is pinned to.
type ExecutionReleasePin struct {
	DeploymentID string `json:"deploymentId,omitempty"`
	Commit       string `json:"commit"`
	BundleDigest string `json:"bundleDigest"`
}

// ExecutionAttestationBinding is the canonical, provider-neutral statement a
// signed execution attestation makes: this Run was admitted against exactly
// these immutable references, for this audience, until this instant.
//
// Field order is the canonical encoding order and References is sorted by name,
// so any implementation that encodes this struct reproduces the same bytes and
// the same digest. Values Core does not interpret — a downstream capability
// service's own identities, versions, or epochs — travel in References as named
// immutable pins supplied by the projection.
type ExecutionAttestationBinding struct {
	Kind            string                       `json:"kind"`
	Audience        string                       `json:"audience"`
	IssuerKeyID     string                       `json:"issuerKeyId"`
	ExpiresAt       string                       `json:"expiresAt"`
	RunRef          string                       `json:"runRef"`
	Workspace       string                       `json:"workspace"`
	App             string                       `json:"app"`
	Action          string                       `json:"action"`
	PublicationRef  string                       `json:"publicationRef"`
	RouteGeneration int64                        `json:"routeGeneration"`
	OperationRef    string                       `json:"operationRef"`
	CredentialRef   ImmutableReference           `json:"credentialRef"`
	Release         ExecutionReleasePin          `json:"release"`
	References      []NamedImmutableReferencePin `json:"references,omitempty"`
}

// ExecutionAttestation is host-private execution metadata. It is never an
// external HTTP header, a public API response, or part of a Run outcome or
// event payload.
type ExecutionAttestation struct {
	Kind          string                      `json:"kind"`
	Binding       ExecutionAttestationBinding `json:"binding"`
	BindingDigest string                      `json:"bindingDigest"`
	Algorithm     string                      `json:"algorithm"`
	Signature     string                      `json:"signature"`
}

func CloneExecutionAttestationBinding(binding ExecutionAttestationBinding) ExecutionAttestationBinding {
	binding.References = cloneNamedImmutableReferencePins(binding.References)
	return binding
}

func CloneExecutionAttestation(attestation *ExecutionAttestation) *ExecutionAttestation {
	if attestation == nil {
		return nil
	}
	clone := *attestation
	clone.Binding = CloneExecutionAttestationBinding(attestation.Binding)
	return &clone
}

func cloneNamedImmutableReferencePins(pins []NamedImmutableReferencePin) []NamedImmutableReferencePin {
	if len(pins) == 0 {
		return nil
	}
	return append([]NamedImmutableReferencePin(nil), pins...)
}
