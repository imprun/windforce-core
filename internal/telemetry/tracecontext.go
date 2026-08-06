package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	Version1              = 1
	maxTraceParentBytes   = 512
	maxTraceStateBytes    = 512
	maxLabelBytes         = 64
	instrumentationName   = "github.com/imprun/windforce-core"
	ReasonContinued       = "continued"
	ReasonNewMissing      = "new_missing"
	ReasonNewInvalid      = "new_invalid"
	ReasonAttemptRecovery = "attempt_recovery"
)

// TraceContextV1 is the bounded, versioned W3C carrier persisted with a Run and
// Job. It is execution metadata, never Application input.
type TraceContextV1 struct {
	Version           int    `json:"version"`
	TraceParent       string `json:"traceparent,omitempty"`
	TraceState        string `json:"tracestate,omitempty"`
	Origin            string `json:"origin,omitempty"`
	PropagationReason string `json:"propagationReason,omitempty"`
}

func (c *TraceContextV1) UnmarshalJSON(data []byte) error {
	type wire TraceContextV1
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value := TraceContextV1(decoded)
	if !value.IsCanonical() {
		*c = TraceContextV1{}
		return nil
	}
	value.Origin = boundedLabel(value.Origin)
	value.PropagationReason = boundedLabel(value.PropagationReason)
	*c = value
	return nil
}

func (c TraceContextV1) IsZero() bool {
	return c.Version == 0 && c.TraceParent == "" && c.TraceState == "" && c.Origin == "" && c.PropagationReason == ""
}

func (c TraceContextV1) IsCanonical() bool {
	if c.Version != Version1 {
		return false
	}
	if len(c.Origin) > maxLabelBytes || len(c.PropagationReason) > maxLabelBytes {
		return false
	}
	if c.TraceParent == "" {
		return c.TraceState == ""
	}
	return c.IsValid()
}

func (c TraceContextV1) IsValid() bool {
	return SpanContext(c).IsValid()
}

// ParseCarrier validates and canonicalizes a W3C carrier without retaining an
// invalid raw value. Missing and invalid carriers are deliberately non-fatal.
func ParseCarrier(traceParent string, traceState string, origin string) (TraceContextV1, string) {
	traceParent = strings.TrimSpace(traceParent)
	traceState = strings.TrimSpace(traceState)
	if traceParent == "" {
		return TraceContextV1{}, ReasonNewMissing
	}
	if len(traceParent) > maxTraceParentBytes || len(traceState) > maxTraceStateBytes {
		return TraceContextV1{}, ReasonNewInvalid
	}
	if traceState != "" {
		if _, err := trace.ParseTraceState(traceState); err != nil {
			return TraceContextV1{}, ReasonNewInvalid
		}
	}
	carrier := propagation.MapCarrier{"traceparent": traceParent}
	if traceState != "" {
		carrier.Set("tracestate", traceState)
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), carrier)
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return TraceContextV1{}, ReasonNewInvalid
	}
	canonical := carrierFromSpanContext(sc)
	return TraceContextV1{
		Version:           Version1,
		TraceParent:       canonical.Get("traceparent"),
		TraceState:        canonical.Get("tracestate"),
		Origin:            boundedLabel(origin),
		PropagationReason: ReasonContinued,
	}, ReasonContinued
}

// SpanContext returns a remote SpanContext suitable for crossing a durable or
// process boundary. Invalid or unknown versions return an invalid context.
func SpanContext(c TraceContextV1) trace.SpanContext {
	if c.Version != Version1 || strings.TrimSpace(c.TraceParent) == "" {
		return trace.SpanContext{}
	}
	parsed, _ := ParseCarrier(c.TraceParent, c.TraceState, c.Origin)
	if parsed.Version != Version1 {
		return trace.SpanContext{}
	}
	carrier := propagation.MapCarrier{"traceparent": parsed.TraceParent}
	if parsed.TraceState != "" {
		carrier.Set("tracestate", parsed.TraceState)
	}
	return trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), carrier))
}

func ContextWithCarrier(ctx context.Context, c TraceContextV1) context.Context {
	sc := SpanContext(c)
	if !sc.IsValid() {
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// FromContext snapshots the current valid span context into a durable carrier.
func FromContext(ctx context.Context, origin string, reason string) TraceContextV1 {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return TraceContextV1{
			Version:           Version1,
			Origin:            boundedLabel(origin),
			PropagationReason: boundedLabel(reason),
		}
	}
	carrier := carrierFromSpanContext(sc)
	return TraceContextV1{
		Version:           Version1,
		TraceParent:       carrier.Get("traceparent"),
		TraceState:        carrier.Get("tracestate"),
		Origin:            boundedLabel(origin),
		PropagationReason: boundedLabel(reason),
	}
}

func TraceID(c TraceContextV1) string {
	sc := SpanContext(c)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

type ingressInfo struct {
	origin string
	reason string
}

type ingressInfoKey struct{}

// IngressContext applies optional-in, valid-out precedence at non-HTTP
// protocol boundaries: valid ambient context, then valid carrier, otherwise a
// context that lets the local SDK create a root while retaining only a bounded
// reason.
func IngressContext(ctx context.Context, traceParent string, traceState string, origin string) context.Context {
	if trace.SpanContextFromContext(ctx).IsValid() {
		return context.WithValue(ctx, ingressInfoKey{}, ingressInfo{origin: "ambient", reason: ReasonContinued})
	}
	carrier, reason := ParseCarrier(traceParent, traceState, origin)
	if carrier.IsValid() {
		ctx = ContextWithCarrier(ctx, carrier)
	}
	return context.WithValue(ctx, ingressInfoKey{}, ingressInfo{origin: boundedLabel(origin), reason: reason})
}

func EnsureIngressContext(ctx context.Context, origin string) context.Context {
	if _, exists := ctx.Value(ingressInfoKey{}).(ingressInfo); exists {
		return ctx
	}
	reason := ReasonNewMissing
	if trace.SpanContextFromContext(ctx).IsValid() {
		reason = ReasonContinued
		origin = "ambient"
	}
	return context.WithValue(ctx, ingressInfoKey{}, ingressInfo{origin: boundedLabel(origin), reason: reason})
}

func CreationContext(ctx context.Context, fallbackOrigin string) TraceContextV1 {
	info, _ := ctx.Value(ingressInfoKey{}).(ingressInfo)
	origin := info.origin
	if origin == "" {
		origin = fallbackOrigin
	}
	reason := info.reason
	if reason == "" {
		reason = ReasonContinued
	}
	return FromContext(ctx, origin, reason)
}

// HTTPHandler validates only W3C Trace Context, discards baggage, and then uses
// standard OTel HTTP instrumentation. Invalid carriers are removed before the
// instrumentation sees them and never enter logs or durable state.
func HTTPHandler(next http.Handler, spanName string) http.Handler {
	traced := otelhttp.NewHandler(next, spanName,
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithSpanOptions(trace.WithAttributes(attribute.String("windforce.component", "http_server"))),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "http"
		traceParents := r.Header.Values("traceparent")
		carrier := TraceContextV1{}
		reason := ReasonNewMissing
		if len(traceParents) == 1 {
			carrier, reason = ParseCarrier(traceParents[0], strings.Join(r.Header.Values("tracestate"), ","), origin)
		} else if len(traceParents) > 1 {
			reason = ReasonNewInvalid
		}
		if ambient := trace.SpanContextFromContext(r.Context()); ambient.IsValid() {
			carrier = FromContext(r.Context(), "ambient", ReasonContinued)
			reason = ReasonContinued
			origin = "ambient"
		}
		clone := r.Clone(context.WithValue(r.Context(), ingressInfoKey{}, ingressInfo{origin: origin, reason: reason}))
		clone.Header = r.Header.Clone()
		clone.Header.Del("baggage")
		clone.Header.Del("traceparent")
		clone.Header.Del("tracestate")
		if carrier.IsValid() {
			clone.Header.Set("traceparent", carrier.TraceParent)
			if carrier.TraceState != "" {
				clone.Header.Set("tracestate", carrier.TraceState)
			}
		}
		traced.ServeHTTP(w, clone)
	})
}

func HTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base,
		otelhttp.WithPropagators(propagation.TraceContext{}),
		otelhttp.WithSpanOptions(trace.WithAttributes(attribute.String("windforce.component", "worker_plane_client"))),
	)
}

// StartAttempt deliberately ignores the polling/claim span. Attempt 1 is a
// child of immutable creation context. Recovery attempts start a new trace and
// link back to creation because the previous attempt context is not persisted.
func StartAttempt(ctx context.Context, creation TraceContextV1, attempt int, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.Tracer(instrumentationName)
	sc := SpanContext(creation)
	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	if attempt <= 1 && sc.IsValid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
	} else {
		opts = append(opts, trace.WithNewRoot())
		if sc.IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
		}
	}
	return tracer.Start(ctx, "windforce.job.attempt", opts...)
}

func carrierFromSpanContext(sc trace.SpanContext) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(trace.ContextWithRemoteSpanContext(context.Background(), sc), carrier)
	return carrier
}

func boundedLabel(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxLabelBytes {
		value = value[:maxLabelBytes]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
