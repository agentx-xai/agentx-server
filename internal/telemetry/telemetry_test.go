package telemetry_test

import (
	"agentx/server/internal/repo/file"
	"agentx/server/internal/telemetry"
	"agentx/server/internal/usecase/registry"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestConfigureWithoutEndpointIsDisabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := telemetry.Configure(context.Background(), "agentx-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStorageRegistryAndPostgresSpans(t *testing.T) {
	recorder, restore := spanRecorder(t)
	defer restore()
	store := telemetry.TraceArtifactStore(file.NewArtifactStore(t.TempDir()))
	service := registry.New(store, file.NewPackageRepo(t.TempDir()))
	if _, err := service.Publish(context.Background(), "demo", "1.0.0", bytes.NewBufferString("artifact")); err != nil {
		t.Fatal(err)
	}
	pgxTracer := telemetry.NewPGXTracer()
	ctx := pgxTracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	pgxTracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("database unavailable")})

	spans := recorder.Ended()
	wanted := map[string]bool{"artifact.put": false, "registry.prepare": false, "postgresql.select": false}
	for _, span := range spans {
		if _, ok := wanted[span.Name()]; ok {
			wanted[span.Name()] = true
		}
		if span.Name() == "postgresql.select" && span.Status().Code.String() != "Error" {
			t.Fatalf("database error span has status %s", span.Status().Code)
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("span %q missing from %#v", name, spanNames(spans))
		}
	}
}

func spanRecorder(t *testing.T) (*tracetest.SpanRecorder, func()) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	return recorder, func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
	}
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

var _ trace.TracerProvider
