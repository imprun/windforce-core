package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestInitSDKCreatesRootsWithoutExporterAndHonorsDisable(t *testing.T) {
	for _, key := range []string{
		"OTEL_SDK_DISABLED",
		"OTEL_TRACES_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	sdk := InitSDK(context.Background(), "worker", "test")
	ctx, span := otel.Tracer(instrumentationName).Start(context.Background(), "root")
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("SDK without exporter did not create a trace ID")
	}
	span.End()
	if err := sdk.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OTEL_SDK_DISABLED", "true")
	disabled := InitSDK(context.Background(), "worker", "test")
	ctx, span = otel.Tracer(instrumentationName).Start(context.Background(), "disabled")
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("OTEL_SDK_DISABLED still created a trace ID")
	}
	span.End()
	if err := disabled.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInitSDKExporterConfigurationFailureIsNonFatal(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "")
	t.Setenv("OTEL_TRACES_EXPORTER", "unsupported")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	previous := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	sdk := InitSDK(context.Background(), "server", "test")
	if sdk.Warning() == nil {
		t.Fatal("invalid exporter configuration did not produce a warning")
	}
	ctx, span := otel.Tracer(instrumentationName).Start(context.Background(), "still-running")
	if !trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("exporter configuration failure disabled in-process tracing")
	}
	span.End()
	if err := sdk.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultServiceNamesAreRoleSpecific(t *testing.T) {
	for role, want := range map[string]string{
		"server":     "windforce-core-server",
		"worker":     "windforce-core-worker",
		"standalone": "windforce-core-standalone",
	} {
		if got := defaultServiceName(role); got != want {
			t.Fatalf("defaultServiceName(%q) = %q, want %q", role, got, want)
		}
	}
}
