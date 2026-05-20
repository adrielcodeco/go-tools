package apmcore

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// BatchSpanEmitter returns a SpanEmitter compatible with
// gormautobatch.Config.SpanEmitter. It emits a background OTel span for
// each batch flush so batched writes are visible in APM even though they
// belong to no specific request.
func BatchSpanEmitter() func(table string, ops int, elapsed time.Duration) {
	tracer := otel.Tracer("gormautobatch")
	return func(table string, ops int, elapsed time.Duration) {
		// Use context.Background() — this is a background operation.
		ctx := context.Background()
		_, span := tracer.Start(ctx, "autobatch.flush",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithTimestamp(time.Now().Add(-elapsed)),
		)
		span.SetAttributes(
			attribute.String("db.table", table),
			attribute.Int("db.batch.ops", ops),
		)
		span.End(trace.WithTimestamp(time.Now()))
	}
}
