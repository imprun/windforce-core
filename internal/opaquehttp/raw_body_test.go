package opaquehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRawContextPublishedSchemaAndFixture(t *testing.T) {
	t.Parallel()
	const base = "https://raw.githubusercontent.com/imprun/windforce-core/main/contracts/opaque-http/v1/"
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"opaque-http-invocation.schema.json", "opaque-http-ingress-context.schema.json"} {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contractFixture(t, name)))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource(base+name, document); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile(base + "opaque-http-ingress-context.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	raw := contractFixture(t, "opaque-http-ingress-context.example.json")
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	headers := http.Header{RawContextHeader: []string{compact.String()}}
	invocation, err := decodeRawContext(headers, envelopeOverheadBytes, 1024)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := decodeInvocation(bytes.NewReader(contractFixture(t, "opaque-http-invocation.example.json")), envelopeOverheadBytes, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.TrustedIngress != want.TrustedIngress || invocation.HTTP != want.HTTP || !invocation.ReceivedAt.Equal(want.ReceivedAt) || !invocation.DeadlineAt.Equal(want.DeadlineAt) {
		t.Fatal("published raw/envelope fixtures describe different deliveries")
	}
	for _, invalid := range []string{
		strings.Replace(compact.String(), `"deliveryId":`, `"missingDeliveryId":`, 1),
		strings.Replace(compact.String(), `"credentialRef":`, `"missingCredentialRef":`, 1),
		`{"body":{},` + compact.String()[1:],
	} {
		value, err := jsonschema.UnmarshalJSON(strings.NewReader(invalid))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(value); err == nil {
			t.Fatal("published schema accepted invalid context")
		}
		headers.Set(RawContextHeader, invalid)
		if _, err := decodeRawContext(headers, envelopeOverheadBytes, 1024); err == nil {
			t.Fatal("runtime accepted invalid context")
		}
	}
}

// A schema stored as part of a Release must not decode to literal U+0000:
// PostgreSQL JSONB refuses it, even when the surrounding JSON is valid.
func TestWireResponseSchemaControlPatternIsJSONBSafe(t *testing.T) {
	t.Parallel()
	const schemaURL = "https://raw.githubusercontent.com/imprun/windforce-core/main/contracts/opaque-http/v1/application-wire-response.schema.json"
	raw := contractFixture(t, "application-wire-response.schema.json")
	var definition struct {
		Properties struct {
			Headers struct {
				Items struct {
					Properties struct{ Value struct{ Pattern string } }
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &definition); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(definition.Properties.Headers.Items.Properties.Value.Pattern, 0) {
		t.Fatal("schema contains a literal NUL after JSON decoding")
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURL, document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"application/octet-stream", "bad\x00value", "bad\x1fvalue", "bad\x7fvalue"} {
		response := ApplicationWireResponseV1{Kind: ApplicationWireResponseKindV1, Status: 200, Headers: []ResponseHeaderV1{{Name: "content-type", Value: header}}, Body: bodyBytes(nil)}
		encoded, _ := json.Marshal(response)
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		valid := schema.Validate(instance) == nil
		if valid != (header == "application/octet-stream") {
			t.Fatal("JSONB-safe regex changed header control-character rejection")
		}
	}
}

// The real TCP/telemetry read-deadline behavior is covered in the CLI tests.
// This recorder makes deadline sequencing observable in parser unit tests.
type rawDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (r *rawDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.deadlines = append(r.deadlines, deadline)
	return nil
}

func rawContextForTest(t *testing.T, invocation OpaqueHTTPInvocationV1) string {
	t.Helper()
	raw, err := json.Marshal(invocation)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "body")
	fields["kind"], _ = json.Marshal(RawContextKindV1)
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func rawRequestForTest(t *testing.T, raw []byte, mutate func(*OpaqueHTTPInvocationV1)) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, IngressPath, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set(RawContextHeader, rawContextForTest(t, invocationValue(t, mutate)))
	return request
}

func rawHandlerForTest(t *testing.T, resolver Resolver, admission Admission) *Handler {
	t.Helper()
	limits := testLimits(2 * time.Second)
	limits.Transport = TransportRawBodyV1
	handler, err := NewHandler(resolver, admission, limits)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestRawTransportPreservesExactAppInputAndAdmissionIdentity(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string][]byte{
		"empty":               {},
		"binary":              {0, 1, 0xff, 0x80, 0x0d, 0x0a},
		"multibyte":           []byte("원문 bytes 그대로"),
		"JSON remains opaque": []byte(` { "kind": "windforce.opaque-http-ingress-request/v1", "secret": 1 } `),
	} {
		t.Run(name, func(t *testing.T) {
			var requests []execution.CreateRunRequest
			var resolutions []ResolutionRequest
			admission := &admissionFake{create: func(_ context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
				requests = append(requests, request)
				return execution.Admission{}, &execution.Fault{Kind: execution.FaultUnavailable}
			}}
			resolver := resolverFunc(func(ctx context.Context, request ResolutionRequest) (ResolvedAdmission, error) {
				if _, exposed := ctx.Deadline(); exposed {
					t.Fatal("raw context deadline leaked into Resolver")
				}
				resolutions = append(resolutions, request)
				return validResolvedAdmission(), nil
			})
			invocation := invocationValue(t, nil)
			invocation.Body = bodyBytes(raw)
			envelope, _ := json.Marshal(invocation)
			envelopeRequest := httptest.NewRequest(http.MethodPost, IngressPath, bytes.NewReader(envelope))
			envelopeRequest.Header.Set("Content-Type", "application/json")
			mustHandler(t, resolver, admission, 2*time.Second).ServeHTTP(httptest.NewRecorder(), envelopeRequest)
			rawRequest := rawRequestForTest(t, raw, nil)
			rawRequest.Header.Set(RawContextHeader, rawContextForTest(t, invocation))
			rawRequest.Header.Set("Authorization", "synthetic-not-forwarded")
			rawRequest.ContentLength = -1 // streaming length is checked from observed bytes
			response := &rawDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			rawHandlerForTest(t, resolver, admission).ServeHTTP(response, rawRequest)
			if len(requests) != 2 || !reflect.DeepEqual(requests[0], requests[1]) {
				t.Fatalf("transport changed the Admission request: count=%d", len(requests))
			}
			if len(resolutions) != 2 || !reflect.DeepEqual(resolutions[0], resolutions[1]) || resolutions[1].BodyByteLength != int64(len(raw)) {
				t.Fatal("transport changed the body-blind ResolutionRequest")
			}
			if len(response.deadlines) != 2 || !response.deadlines[0].Equal(invocation.DeadlineAt) || !response.deadlines[1].IsZero() {
				t.Fatal("raw read deadline was not installed then cleared")
			}
		})
	}
}

type trackedRawBody struct {
	reader io.Reader
	reads  int
	bytes  int
	onEOF  func()
}

func (b *trackedRawBody) Read(p []byte) (int, error) {
	b.reads++
	n, err := b.reader.Read(p)
	b.bytes += n
	if err == io.EOF && b.onEOF != nil {
		b.onEOF()
	}
	return n, err
}

func (*trackedRawBody) Close() error { return nil }

func TestRawTransportRejectsInvalidContextBeforeReadingBody(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"missing context", func(r *http.Request) { r.Header.Del(RawContextHeader) }, 400},
		{"empty context", func(r *http.Request) { r.Header.Set(RawContextHeader, "") }, 400},
		{"repeated context", func(r *http.Request) { r.Header.Add(RawContextHeader, r.Header.Get(RawContextHeader)) }, 400},
		{"mixed-case duplicate", func(r *http.Request) {
			r.Header[strings.ToLower(RawContextHeader)] = []string{r.Header.Get(RawContextHeader)}
		}, 400},
		{"oversize", func(r *http.Request) { r.Header.Set(RawContextHeader, strings.Repeat(" ", MaxRawContextBytes+1)) }, 400},
		{"non-ASCII", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.ReplaceAll(r.Header.Get(RawContextHeader), "synthetic-delivery-0001", "한글"))
		}, 400},
		{"control character", func(r *http.Request) { r.Header.Set(RawContextHeader, "\t"+r.Header.Get(RawContextHeader)) }, 400},
		{"unknown reserved header", func(r *http.Request) { r.Header.Set("X-Windforce-Opaque-Target", "ignored-must-not-admit") }, 400},
		{"wrong kind", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.ReplaceAll(r.Header.Get(RawContextHeader), RawContextKindV1, OpaqueHTTPInvocationKindV1))
		}, 400},
		{"trailing JSON", func(r *http.Request) { r.Header.Set(RawContextHeader, r.Header.Get(RawContextHeader)+`{}`) }, 400},
		{"duplicate nested field", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.Replace(r.Header.Get(RawContextHeader), `"issuer":`, `"issuer":"other","issuer":`, 1))
		}, 400},
		{"unknown nested field", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.Replace(r.Header.Get(RawContextHeader), `"issuer":`, `"principal":"forged","issuer":`, 1))
		}, 400},
		{"unknown root body", func(r *http.Request) {
			r.Header.Set(RawContextHeader, `{"body":null,`+r.Header.Get(RawContextHeader)[1:])
		}, 400},
		{"null string", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.Replace(r.Header.Get(RawContextHeader), `"method":"POST"`, `"method":null`, 1))
		}, 400},
		{"noncanonical path", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.ReplaceAll(r.Header.Get(RawContextHeader), "/synthetic/bytes", "/synthetic/../bytes"))
		}, 400},
		{"query path", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.ReplaceAll(r.Header.Get(RawContextHeader), "/synthetic/bytes", "/synthetic/bytes?query=1"))
		}, 400},
		{"absent credential", func(r *http.Request) {
			r.Header.Set(RawContextHeader, strings.Replace(r.Header.Get(RawContextHeader), `"credentialRef":`, `"missingCredentialRef":`, 1))
		}, 400},
		{"compressed", func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 400},
		{"identity encoding", func(r *http.Request) { r.Header.Set("Content-Encoding", "identity") }, 400},
		{"empty encoding", func(r *http.Request) { r.Header["Content-Encoding"] = nil }, 400},
		{"declared trailer", func(r *http.Request) { r.Trailer = http.Header{"X-Synthetic": nil} }, 400},
		{"trailer header", func(r *http.Request) { r.Header.Set("Trailer", "X-Synthetic") }, 400},
		{"method", func(r *http.Request) { r.Method = http.MethodPut }, 405},
		{"missing media type", func(r *http.Request) { r.Header.Del("Content-Type") }, 415},
		{"wrong media type", func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }, 415},
		{"media parameter", func(r *http.Request) { r.Header.Set("Content-Type", "application/octet-stream; charset=utf-8") }, 415},
		{"duplicate media", func(r *http.Request) { r.Header.Add("Content-Type", "application/octet-stream") }, 415},
		{"over declared length", func(r *http.Request) { r.ContentLength = MaxWireBodyBytes + 1 }, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackedRawBody{reader: strings.NewReader("not-read")}
			request := rawRequestForTest(t, nil, nil)
			request.Body = body
			test.mutate(request)
			resolver := resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
				t.Fatal("invalid raw metadata reached Resolver")
				return ResolvedAdmission{}, nil
			})
			response := &rawDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			rawHandlerForTest(t, resolver, &admissionFake{}).ServeHTTP(response, request)
			if response.Code != test.status || body.reads != 0 {
				t.Fatalf("status=%d want=%d reads=%d", response.Code, test.status, body.reads)
			}
			assertPlatformFailureCategory(t, response.Body.Bytes(), FailureApplicationProtocolViolation)
			if strings.Contains(response.Body.String(), "synthetic") || strings.Contains(response.Body.String(), "not-read") {
				t.Fatal("failure response leaked context or body")
			}
		})
	}
}

func TestRawContextMaximumBoundAndEscapedUnicode(t *testing.T) {
	t.Parallel()
	request := rawRequestForTest(t, nil, nil)
	contextValue := request.Header.Get(RawContextHeader)
	request.Header.Set(RawContextHeader, contextValue+strings.Repeat(" ", MaxRawContextBytes-len(contextValue)))
	if _, err := decodeRawContext(request.Header, envelopeOverheadBytes, 1); err != nil {
		t.Fatalf("exact metadata bound rejected: %v", err)
	}
	request.Header.Set(RawContextHeader, strings.Replace(contextValue, "synthetic-delivery-0001", `\uD55C\uAE00`, 1))
	invocation, err := decodeRawContext(request.Header, envelopeOverheadBytes, 1)
	if err != nil || invocation.TrustedIngress.DeliveryID != "한글" {
		t.Fatalf("ASCII escaped JSON changed existing string semantics: %v", err)
	}
}

func TestRawTransportChecksStreamBoundAndTrailersBeforeResolution(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		data     string
		limit    int64
		trailer  bool
		admitted bool
	}{
		{"at limit", "1234", 4, false, true},
		{"over limit", "123456789", 4, false, false},
		{"late trailer", "1234", 4, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := rawRequestForTest(t, nil, nil)
			body := &trackedRawBody{reader: strings.NewReader(test.data)}
			if test.trailer {
				body.onEOF = func() { request.Trailer = http.Header{"X-Synthetic": {"late"}} }
			}
			request.Body, request.ContentLength = body, -1
			called := false
			resolver := resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
				called = true
				return ResolvedAdmission{}, &ResolutionFailure{Category: FailureCapacityUnavailable}
			})
			handler := rawHandlerForTest(t, resolver, &admissionFake{})
			handler.limits.MaxRequestBytes = test.limit
			response := &rawDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			handler.ServeHTTP(response, request)
			if called != test.admitted || int64(body.bytes) > test.limit+1 {
				t.Fatalf("called=%v read=%d", called, body.bytes)
			}
			if !test.admitted && response.Code != 400 {
				t.Fatalf("status=%d", response.Code)
			}
			if !test.admitted && response.deadlines[len(response.deadlines)-1].IsZero() {
				t.Fatal("incomplete read lost its drain bound")
			}
		})
	}
}

func TestRawTransportFailsClosedWithoutReadDeadlineSupport(t *testing.T) {
	t.Parallel()
	request := rawRequestForTest(t, nil, nil)
	body := &trackedRawBody{reader: strings.NewReader("not-read")}
	request.Body = body
	response := httptest.NewRecorder()
	rawHandlerForTest(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		t.Fatal("unsupported read deadline reached Resolver")
		return ResolvedAdmission{}, nil
	}), &admissionFake{}).ServeHTTP(response, request)
	if response.Code != 500 || body.reads != 0 {
		t.Fatalf("status=%d reads=%d", response.Code, body.reads)
	}
}

type errorRawReader struct{}

func (errorRawReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRawTransportRejectsReadFailureAndExpiredContext(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		request := rawRequestForTest(t, nil, func(invocation *OpaqueHTTPInvocationV1) {
			if expired {
				invocation.DeadlineAt = time.Now().Add(-time.Second)
			}
		})
		body := &trackedRawBody{reader: errorRawReader{}}
		request.Body = body
		response := &rawDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		rawHandlerForTest(t, resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
			t.Fatal("invalid delivery reached Resolver")
			return ResolvedAdmission{}, nil
		}), &admissionFake{}).ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("status=%d", response.Code)
		}
		if expired && body.reads != 0 {
			t.Fatal("expired context read the body")
		}
	}
}

func TestEnvelopeModeRejectsRawContextAndUnknownTransport(t *testing.T) {
	t.Parallel()
	resolver := resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
		return ResolvedAdmission{}, errors.New("must not resolve")
	})
	limits := testLimits(2 * time.Second)
	limits.Transport = "unknown"
	if _, err := NewHandler(resolver, &admissionFake{}, limits); err == nil {
		t.Fatal("unknown transport accepted")
	}
	request := httptest.NewRequest(http.MethodPost, IngressPath, bytes.NewReader(invocationEnvelope(t, nil)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(RawContextHeader, rawContextForTest(t, invocationValue(t, nil)))
	response := httptest.NewRecorder()
	mustHandler(t, resolver, &admissionFake{}, 2*time.Second).ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("envelope mode ignored context: status=%d", response.Code)
	}
}

func TestRawTransportAcceptsTheWireMaximumBody(t *testing.T) {
	raw := bytes.Repeat([]byte{0xff}, int(MaxWireBodyBytes))
	request := rawRequestForTest(t, raw, nil)
	var observed int64
	handler := rawHandlerForTest(t, resolverFunc(func(_ context.Context, request ResolutionRequest) (ResolvedAdmission, error) {
		observed = request.BodyByteLength
		return ResolvedAdmission{}, &ResolutionFailure{Category: FailureCapacityUnavailable}
	}), &admissionFake{})
	handler.limits.MaxRequestBytes = MaxWireBodyBytes
	response := &rawDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(response, request)
	if observed != MaxWireBodyBytes || response.Code != http.StatusServiceUnavailable {
		t.Fatalf("maximum body did not reach body-blind Resolver: observed=%d status=%d", observed, response.Code)
	}
}
