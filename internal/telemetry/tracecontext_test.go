package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const sampledTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestParseCarrierCanonicalizesAndRejectsInvalidWithoutEcho(t *testing.T) {
	got, reason := ParseCarrier(sampledTraceParent, "vendor=value", "http")
	if reason != ReasonContinued || !got.IsValid() || got.TraceParent != sampledTraceParent {
		t.Fatalf("valid carrier = %#v, %q", got, reason)
	}
	invalid := strings.Repeat("secret", 200)
	got, reason = ParseCarrier(invalid, "", "http")
	if reason != ReasonNewInvalid || got.TraceParent != "" || got.TraceState != "" {
		t.Fatalf("invalid carrier leaked or was accepted: %#v, %q", got, reason)
	}
	got, reason = ParseCarrier(sampledTraceParent, strings.Repeat("a", maxTraceStateBytes+1), "http")
	if reason != ReasonNewInvalid || got.TraceParent != "" || got.TraceState != "" {
		t.Fatalf("oversized tracestate leaked or was accepted: %#v, %q", got, reason)
	}
}

func TestParseCarrierPreservesSampledFlag(t *testing.T) {
	for _, flag := range []string{"00", "01"} {
		carrier, reason := ParseCarrier("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-"+flag, "", "http")
		if reason != ReasonContinued || !carrier.IsValid() || !strings.HasSuffix(carrier.TraceParent, "-"+flag) {
			t.Fatalf("flag %s carrier = %#v reason=%s", flag, carrier, reason)
		}
	}
}

func TestHTTPHandlerContinuesValidAndRootsMissing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	seen := make(chan oteltrace.SpanContext, 2)
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- oteltrace.SpanContextFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}), "test")

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("traceparent", sampledTraceParent)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	continued := <-seen
	if continued.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("continued trace id = %s", continued.TraceID())
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))
	rooted := <-seen
	if !rooted.IsValid() || rooted.TraceID() == continued.TraceID() {
		t.Fatalf("missing carrier did not start a distinct root: %v", rooted)
	}
}

func TestHTTPHandlerMalformedAndOversizedCarriersRootWithoutRejecting(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})

	seen := make(chan TraceContextV1, 2)
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- CreationContext(r.Context(), "http")
		w.WriteHeader(http.StatusNoContent)
	}), "test.invalid")
	for _, raw := range []string{"not-a-trace", strings.Repeat("a", maxTraceParentBytes+1)} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set("traceparent", raw)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("invalid carrier status = %d", response.Code)
		}
		created := <-seen
		if !created.IsValid() || created.PropagationReason != ReasonNewInvalid || strings.Contains(created.TraceParent, raw) {
			t.Fatalf("invalid carrier handling = %#v", created)
		}
	}
}

func TestHTTPHandlerPrefersValidAmbientContext(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	ambient, _ := ParseCarrier(sampledTraceParent, "", "ambient")
	header, _ := ParseCarrier("00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01", "", "http")
	seen := make(chan oteltrace.SpanContext, 1)
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- oteltrace.SpanContextFromContext(r.Context())
	}), "test.ambient")
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ContextWithCarrier(context.Background(), ambient))
	request.Header.Set("traceparent", header.TraceParent)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got := (<-seen).TraceID().String(); got != TraceID(ambient) {
		t.Fatalf("ambient precedence trace id = %s, want %s", got, TraceID(ambient))
	}
}

func TestHTTPHandlerForwardsValidCarrierWithNoopProvider(t *testing.T) {
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	seen := make(chan TraceContextV1, 1)
	handler := HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- CreationContext(r.Context(), "http")
	}), "test.noop")
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("traceparent", sampledTraceParent)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if got := <-seen; TraceID(got) != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("noop provider lost valid inbound carrier: %#v", got)
	}
}

func TestStartAttemptUsesCreationParentThenRecoveryLink(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	})
	creation, _ := ParseCarrier(sampledTraceParent, "", "http")

	_, first := StartAttempt(context.Background(), creation, 1)
	first.End()
	_, recovered := StartAttempt(context.Background(), creation, 2)
	recovered.End()
	legacyContext, legacy := StartAttempt(context.Background(), TraceContextV1{}, 1)
	legacySpanContext := oteltrace.SpanContextFromContext(legacyContext)
	legacy.End()

	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("ended spans = %d", len(spans))
	}
	if spans[0].Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("attempt 1 parent = %s", spans[0].Parent().SpanID())
	}
	if spans[1].Parent().IsValid() || len(spans[1].Links()) != 1 || spans[1].Links()[0].SpanContext.TraceID() != spans[0].SpanContext().TraceID() {
		t.Fatalf("recovery span parent/links = %v / %#v", spans[1].Parent(), spans[1].Links())
	}
	if !legacySpanContext.IsValid() || spans[2].Parent().IsValid() || len(spans[2].Links()) != 0 {
		t.Fatalf("legacy attempt did not start an independent root: %#v", spans[2])
	}
}
