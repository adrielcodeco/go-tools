package apmcore

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	caches "github.com/adrielcodeco/go-tools/gormcache"
)

// InstrumentCacher wraps inner with OTel spans for every cache operation.
// Spans are emitted on the tracer "gormcache" and capture hit/miss for Get,
// tag counts for Store, and affected tables for Invalidate.
// Errors are recorded and the span status is set to Error so that APM tools
// can surface them via error-rate metrics.
func InstrumentCacher(inner caches.Cacher) caches.Cacher {
	return &instrumentedCacher{
		inner:  inner,
		tracer: otel.Tracer("gormcache"),
	}
}

type instrumentedCacher struct {
	inner  caches.Cacher
	tracer trace.Tracer
}

func (c *instrumentedCacher) Get(ctx context.Context, key string, q *caches.Query[any]) (*caches.Query[any], error) {
	_, span := c.tracer.Start(ctx, "gormcache.get", trace.WithSpanKind(trace.SpanKindInternal))
	res, err := c.inner.Get(ctx, key, q)
	span.SetAttributes(
		attribute.Bool("cache.hit", res != nil && err == nil),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	return res, err
}

func (c *instrumentedCacher) Store(ctx context.Context, key string, val *caches.Query[any]) error {
	tags := caches.TagsFromContext(ctx)
	_, span := c.tracer.Start(ctx, "gormcache.store", trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(attribute.Int("cache.tags", len(tags)))
	err := c.inner.Store(ctx, key, val)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	return err
}

func (c *instrumentedCacher) Invalidate(ctx context.Context, event *caches.InvalidationEvent) error {
	_, span := c.tracer.Start(ctx, "gormcache.invalidate", trace.WithSpanKind(trace.SpanKindInternal))
	span.SetAttributes(
		attribute.String("db.tables", strings.Join(event.Tables, ",")),
		attribute.Int("cache.tags", len(event.Tags)),
	)
	err := c.inner.Invalidate(ctx, event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	return err
}
