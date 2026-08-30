package opaquehttp

import "time"

const (
	OpaqueHTTPInvocationKindV1     = "windforce.opaque-http-ingress-request/v1"
	OpaqueHTTPAppInputKindV1       = "windforce.opaque-http-app-input/v1"
	ApplicationWireResponseKindV1  = "windforce.application-wire-response/v1"
	ExecutionOutcomeKindV1         = "windforce.execution-outcome/v1"
	RFC4648Base64Encoding          = "RFC4648-BASE64"
	ExecutionOutcomeCompleted      = "completed"
	ExecutionOutcomePlatformFailed = "platformFailed"
)

type FailureCategory string

const (
	FailureDeadlineExceeded             FailureCategory = "deadlineExceeded"
	FailureCapacityUnavailable          FailureCategory = "capacityUnavailable"
	FailureWorkerLost                   FailureCategory = "workerLost"
	FailureApplicationProtocolViolation FailureCategory = "applicationProtocolViolation"
	FailureInternal                     FailureCategory = "internal"
)

// ImmutableRefV1 identifies one immutable resolver-owned snapshot without
// carrying secret material.
type ImmutableRefV1 struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// TrustedIngressV1 contains only identity and publication assertions already
// authenticated by the private ingress boundary.
type TrustedIngressV1 struct {
	Issuer          string         `json:"issuer"`
	Audience        string         `json:"audience"`
	PublicationRef  string         `json:"publicationRef"`
	RouteGeneration int64          `json:"routeGeneration"`
	CredentialRef   ImmutableRefV1 `json:"credentialRef"`
}

type HTTPMediaV1 struct {
	Method           string `json:"method"`
	ExactEscapedPath string `json:"exactEscapedPath"`
	ContentType      string `json:"contentType"`
}

type BodyBytesV1 struct {
	Encoding   string `json:"encoding"`
	Data       string `json:"data"`
	ByteLength int64  `json:"byteLength"`
	Digest     string `json:"digest"`
}

// OpaqueHTTPInvocationV1 is the strict trusted envelope accepted by the
// opt-in conformance handler. It is not mounted on Core's primary listener.
type OpaqueHTTPInvocationV1 struct {
	Kind           string           `json:"kind"`
	TrustedIngress TrustedIngressV1 `json:"trustedIngress"`
	HTTP           HTTPMediaV1      `json:"http"`
	Body           BodyBytesV1      `json:"body"`
	ReceivedAt     time.Time        `json:"receivedAt"`
	DeadlineAt     time.Time        `json:"deadlineAt"`
}

type OpaqueHTTPAppInputV1 struct {
	Kind string      `json:"kind"`
	HTTP HTTPMediaV1 `json:"http"`
	Body BodyBytesV1 `json:"body"`
}

type ResponseHeaderV1 struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ApplicationWireResponseV1 struct {
	Kind    string             `json:"kind"`
	Status  int                `json:"status"`
	Headers []ResponseHeaderV1 `json:"headers"`
	Body    BodyBytesV1        `json:"body"`
}

type PlatformFailureV1 struct {
	Category  FailureCategory `json:"category"`
	Retryable bool            `json:"retryable"`
}

// ExecutionOutcomeV1 is the stable machine-readable fallback returned only
// when no valid ApplicationWireResponseV1 can be restored.
type ExecutionOutcomeV1 struct {
	Kind     string                     `json:"kind"`
	Outcome  string                     `json:"outcome"`
	Response *ApplicationWireResponseV1 `json:"response,omitempty"`
	Failure  *PlatformFailureV1         `json:"failure,omitempty"`
}
