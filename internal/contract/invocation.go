package contract

// ImmutableReference identifies one versioned control-plane object.
type ImmutableReference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// NamedImmutableReferencePin preserves the role and immutable identity of one
// provider-neutral invocation dependency.
type NamedImmutableReferencePin struct {
	Name      string             `json:"name"`
	Reference ImmutableReference `json:"reference"`
}

// InvocationPins are the immutable provider-neutral control-plane identities
// resolved for one admitted invocation. They are Job metadata and are not part
// of the App input.
type InvocationPins struct {
	PublicationRef  string                       `json:"publicationRef"`
	RouteGeneration int64                        `json:"routeGeneration"`
	OperationRef    string                       `json:"operationRef"`
	CredentialRef   ImmutableReference           `json:"credentialRef"`
	References      []NamedImmutableReferencePin `json:"references,omitempty"`
}

// HTTPPolicy is the resolved publication-specific response boundary
// that Core must enforce after an App completes.
type HTTPPolicy struct {
	ContentTypes            []string `json:"contentTypes"`
	MaxBodyBytes            int64    `json:"maxBodyBytes"`
	AllowMissingContentType bool     `json:"allowMissingContentType,omitempty"`
}

func CloneInvocationPins(pins InvocationPins) InvocationPins {
	pins.References = append([]NamedImmutableReferencePin(nil), pins.References...)
	return pins
}

func CloneHTTPPolicy(policy HTTPPolicy) HTTPPolicy {
	policy.ContentTypes = append([]string(nil), policy.ContentTypes...)
	return policy
}
