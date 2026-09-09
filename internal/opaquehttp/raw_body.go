package opaquehttp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
)

// TransportMode is selected at startup, never by sniffing a request body.
type TransportMode string

const (
	TransportEnvelopeV1 TransportMode = "envelope-v1"
	TransportRawBodyV1  TransportMode = "raw-body-v1"
	RawContextHeader                  = "X-Windforce-Opaque-Context"
	RawContextKindV1                  = "windforce.opaque-http-ingress-context/v1"
	MaxRawContextBytes                = 16 << 10
)

func hasOpaqueContextHeader(headers http.Header) bool {
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-windforce-opaque-") {
			return true
		}
	}
	return false
}

// Header names are case-insensitive even for in-process callers that construct
// http.Header without Header.Set. Empty or repeated fields remain present.
func headerValues(headers http.Header, name string) []string {
	var values []string
	for key, entries := range headers {
		if strings.EqualFold(key, name) {
			if len(entries) == 0 {
				values = append(values, "")
			}
			values = append(values, entries...)
		}
	}
	return values
}

func decodeRawContext(headers http.Header, maxEnvelopeBytes, maxBodyBytes int64) (OpaqueHTTPInvocationV1, error) {
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-windforce-opaque-") && !strings.EqualFold(name, RawContextHeader) {
			return OpaqueHTTPInvocationV1{}, errors.New("unknown opaque context header")
		}
	}
	values := headerValues(headers, RawContextHeader)
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > MaxRawContextBytes {
		return OpaqueHTTPInvocationV1{}, errors.New("one bounded opaque context header is required")
	}
	raw := []byte(values[0])
	for _, character := range raw {
		if character < 0x20 || character > 0x7e {
			return OpaqueHTTPInvocationV1{}, errors.New("opaque context header must be printable ASCII JSON")
		}
	}
	if err := rejectDuplicateJSONMembers(raw); err != nil {
		return OpaqueHTTPInvocationV1{}, err
	}
	fields, err := exactObject(raw, "kind", "trustedIngress", "http", "receivedAt", "deadlineAt")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, err
	}
	var kind string
	if err := json.Unmarshal(fields["kind"], &kind); err != nil || kind != RawContextKindV1 {
		return OpaqueHTTPInvocationV1{}, errors.New("unsupported opaque context kind")
	}
	// Reuse the full nested envelope validator, including null/type/unknown-field,
	// canonical path and credential-reference checks. Only bounded metadata is
	// re-encoded here; the raw request bytes never pass through a JSON decoder.
	fields["kind"], _ = json.Marshal(OpaqueHTTPInvocationKindV1)
	fields["body"], _ = json.Marshal(bodyBytes(nil))
	envelope, err := json.Marshal(fields)
	if err != nil {
		return OpaqueHTTPInvocationV1{}, err
	}
	invocation, _, err := decodeInvocation(bytes.NewReader(envelope), maxEnvelopeBytes, maxBodyBytes)
	return invocation, err
}

func bodyBytes(raw []byte) BodyBytesV1 {
	digest := sha256.Sum256(raw)
	return BodyBytesV1{
		Encoding:   RFC4648Base64Encoding,
		Data:       base64.StdEncoding.EncodeToString(raw),
		ByteLength: int64(len(raw)),
		Digest:     "sha256:" + hex.EncodeToString(digest[:]),
	}
}

func (h *Handler) serveRawBody(w http.ResponseWriter, request *http.Request) {
	contentTypes := headerValues(request.Header, "Content-Type")
	if len(contentTypes) != 1 {
		h.writePlatformFailure(w, http.StatusUnsupportedMediaType, FailureApplicationProtocolViolation, false)
		return
	}
	mediaType, params, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || !strings.EqualFold(mediaType, "application/octet-stream") || len(params) != 0 {
		h.writePlatformFailure(w, http.StatusUnsupportedMediaType, FailureApplicationProtocolViolation, false)
		return
	}
	if len(headerValues(request.Header, "Content-Encoding")) != 0 || hasTrailers(request) {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		return
	}
	invocation, err := decodeRawContext(request.Header, h.maxEnvelopeBytes, h.limits.MaxRequestBytes)
	if err != nil {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		return
	}
	if err := h.validateDeadline(invocation, time.Now().UTC()); err != nil {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureDeadlineExceeded, false)
		return
	}
	if request.Body == nil || request.ContentLength > h.limits.MaxRequestBytes {
		h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		return
	}
	// A context deadline alone cannot interrupt a blocked Body.Read. The actual
	// isolated server (including telemetry wrappers) must support read deadlines.
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(invocation.DeadlineAt); err != nil {
		h.writePlatformFailure(w, http.StatusInternalServerError, FailureInternal, false)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, h.limits.MaxRequestBytes+1))
	if err != nil {
		// Do not give net/http an unbounded drain after an incomplete read.
		_ = controller.SetReadDeadline(time.Now())
		if os.IsTimeout(err) || !time.Now().Before(invocation.DeadlineAt) {
			h.writePlatformFailure(w, http.StatusGatewayTimeout, FailureDeadlineExceeded, false)
		} else {
			h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		}
		return
	}
	if int64(len(raw)) > h.limits.MaxRequestBytes || hasTrailers(request) {
		_ = controller.SetReadDeadline(time.Now())
		h.writePlatformFailure(w, http.StatusBadRequest, FailureApplicationProtocolViolation, false)
		return
	}
	_ = controller.SetReadDeadline(time.Time{})
	invocation.Body = bodyBytes(raw)
	h.serveInvocation(w, request, invocation)
}

func hasTrailers(request *http.Request) bool {
	return len(request.Trailer) != 0 || len(headerValues(request.Header, "Trailer")) != 0
}
