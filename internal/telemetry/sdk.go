package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

type SDK struct {
	provider *sdktrace.TracerProvider
	warning  error
}

func (s *SDK) Warning() error {
	if s == nil {
		return nil
	}
	return s.warning
}

func (s *SDK) Shutdown(ctx context.Context) error {
	if s == nil || s.provider == nil {
		return nil
	}
	return s.provider.Shutdown(ctx)
}

// InitSDK installs W3C propagation in every role. The SDK always creates local
// trace IDs unless OTEL_SDK_DISABLED=true, while exporting only when standard
// OTel exporter configuration is explicitly present. Exporter configuration
// errors degrade to an in-process provider and never affect execution.
func InitSDK(ctx context.Context, role string, version string) *SDK {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if disabledByEnvironment() {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		return &SDK{}
	}

	serviceName := defaultServiceName(role)
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
		semconv.ServiceNamespace("windforce"),
		attribute.String("windforce.component", boundedLabel(role)),
	}
	if strings.TrimSpace(version) != "" {
		attrs = append(attrs, semconv.ServiceVersion(strings.TrimSpace(version)))
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
	)
	if res == nil {
		res = resource.Empty()
	}
	sdk := &SDK{warning: err}
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(samplerFromEnvironment()),
	}
	if exporterConfigured() {
		exporter, exportErr := newSpanExporter(ctx)
		if exportErr != nil {
			sdk.warning = errors.Join(sdk.warning, fmt.Errorf("trace exporter: %w", exportErr))
		} else if exporter != nil {
			options = append(options, sdktrace.WithBatcher(exporter))
		}
	}
	sdk.provider = sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(sdk.provider)
	return sdk
}

func newSpanExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	selector := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	if selector == "" {
		selector = "otlp"
	}
	switch selector {
	case "none":
		return nil, nil
	case "console":
		return stdouttrace.New()
	case "otlp":
		protocol := strings.ToLower(strings.TrimSpace(firstNonEmpty(
			os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"),
			os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
		)))
		switch protocol {
		case "", "http/protobuf":
			return otlptracehttp.New(ctx)
		case "grpc":
			return otlptracegrpc.New(ctx)
		default:
			return nil, fmt.Errorf("unsupported OTLP traces protocol %q", protocol)
		}
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q", selector)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultServiceName(role string) string {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "server":
		return "windforce-core-server"
	case "worker":
		return "windforce-core-worker"
	case "standalone":
		return "windforce-core-standalone"
	default:
		return "windforce-core"
	}
}

func disabledByEnvironment() bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")))
	return err == nil && value
}

func exporterConfigured() bool {
	for _, key := range []string{
		"OTEL_TRACES_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func samplerFromEnvironment() sdktrace.Sampler {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	argument := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	ratio := 1.0
	if argument != "" {
		if parsed, err := strconv.ParseFloat(argument, 64); err == nil {
			ratio = parsed
		}
	}
	switch name {
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio)
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	case "always_on":
		return sdktrace.AlwaysSample()
	case "", "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}
