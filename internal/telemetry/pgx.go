package telemetry

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type PGXTracer struct {
	tracer trace.Tracer
}

func NewPGXTracer() *PGXTracer {
	return &PGXTracer{tracer: otel.Tracer("agentx/server/postgresql")}
}

func (t *PGXTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	operation := "query"
	if fields := strings.Fields(data.SQL); len(fields) > 0 {
		operation = strings.ToLower(fields[0])
	}
	ctx, span := t.tracer.Start(ctx, "postgresql."+operation, trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(attribute.String("db.system.name", "postgresql"), attribute.String("db.operation.name", operation))
	return ctx
}

func (t *PGXTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, "database operation failed")
	}
	span.End()
}

var _ pgx.QueryTracer = (*PGXTracer)(nil)
