package gsredis_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	caches "github.com/adrielcodeco/go-tools/gormcache"
	"github.com/adrielcodeco/go-tools/gsredis"
)

func newTestCacher(t *testing.T) *gsredis.RedisCacher {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{mr.Addr()}})
	t.Cleanup(func() { _ = client.Close() })
	return gsredis.NewRedisCacher(client, 0)
}

func TestRedisCacher_StoreAndGet(t *testing.T) {
	cacher := newTestCacher(t)
	ctx := context.Background()

	if err := cacher.Store(ctx, "key1", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := cacher.Get(ctx, "key1", &caches.Query[any]{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected cached value, got nil")
	}
}

func TestRedisCacher_InvalidateByExplicitTag(t *testing.T) {
	cacher := newTestCacher(t)
	ctx := caches.WithTags(context.Background(), "users")

	if err := cacher.Store(ctx, "key-users", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	event := &caches.InvalidationEvent{Tags: []string{"users"}}
	if err := cacher.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cacher.Get(context.Background(), "key-users", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted after invalidation")
	}
}

func TestRedisCacher_InvalidateByTableTag_WithEntityIDs(t *testing.T) {
	// Regression: a single-row INSERT (EntityIDs present) must still evict
	// entries indexed under the plain table tag — previously only event.Tags
	// were resolved, leaving table-tagged SELECTs stale until TTL.
	cacher := newTestCacher(t)
	ctx := caches.WithTags(context.Background(), "products")

	if err := cacher.Store(ctx, "key-products", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	event := &caches.InvalidationEvent{
		Tables:    []string{"products"},
		EntityIDs: []any{42},
	}
	if err := cacher.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cacher.Get(context.Background(), "key-products", &caches.Query[any]{}); got != nil {
		t.Fatal("expected table-tagged key evicted even when EntityIDs are present")
	}
}

func TestRedisCacher_InvalidateByEntityTag(t *testing.T) {
	cacher := newTestCacher(t)
	ctx := caches.WithTags(context.Background(), "users:42")

	if err := cacher.Store(ctx, "key-user-42", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	event := &caches.InvalidationEvent{
		Tables:    []string{"users"},
		EntityIDs: []any{42},
	}
	if err := cacher.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cacher.Get(context.Background(), "key-user-42", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted via entity tag users:42")
	}
}

func TestRedisCacher_InvalidateUnrelatedTagSurvives(t *testing.T) {
	cacher := newTestCacher(t)
	ctx := caches.WithTags(context.Background(), "orders")

	if err := cacher.Store(ctx, "key-orders", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	event := &caches.InvalidationEvent{Tables: []string{"products"}, EntityIDs: []any{1}}
	if err := cacher.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cacher.Get(context.Background(), "key-orders", &caches.Query[any]{}); got == nil {
		t.Fatal("expected unrelated key to survive invalidation")
	}
}

func TestRedisCacher_InvalidateEmptyEventIsNoop(t *testing.T) {
	cacher := newTestCacher(t)
	if err := cacher.Invalidate(context.Background(), &caches.InvalidationEvent{}); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
}
