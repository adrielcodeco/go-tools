package gsrueidis_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/rueidis"

	caches "github.com/adrielcodeco/go-tools/gormcache"
	"github.com/adrielcodeco/go-tools/gsrueidis"
)

func newTestCache(t *testing.T, opts ...gsrueidis.Option) (*gsrueidis.RueidisCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	if err != nil {
		t.Fatalf("rueidis.NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return gsrueidis.NewRueidisCache(client, 0, opts...), mr
}

func TestRueidisCache_StoreAndGet(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	q := &caches.Query[any]{RowsAffected: 1}
	if err := cache.Store(ctx, "key1", q); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := cache.Get(ctx, "key1", &caches.Query[any]{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected cached value, got nil")
	}
}

func TestRueidisCache_GetMiss(t *testing.T) {
	cache, _ := newTestCache(t)
	got, err := cache.Get(context.Background(), "nonexistent", &caches.Query[any]{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil on miss, got %v", got)
	}
}

func TestRueidisCache_InvalidateByExplicitTag(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := caches.WithTags(context.Background(), "users")

	q := &caches.Query[any]{RowsAffected: 1}
	if err := cache.Store(ctx, "key-users", q); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify stored.
	if got, _ := cache.Get(context.Background(), "key-users", &caches.Query[any]{}); got == nil {
		t.Fatal("expected key present before invalidation")
	}

	event := &caches.InvalidationEvent{Tags: []string{"users"}}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	// Key must be gone.
	if got, _ := cache.Get(context.Background(), "key-users", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted after invalidation")
	}
}

func TestRueidisCache_InvalidateByEntityTag(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := caches.WithTags(context.Background(), "users:42")

	q := &caches.Query[any]{RowsAffected: 1}
	if err := cache.Store(ctx, "key-user-42", q); err != nil {
		t.Fatalf("Store: %v", err)
	}

	event := &caches.InvalidationEvent{
		Tables:    []string{"users"},
		EntityIDs: []any{42},
	}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cache.Get(context.Background(), "key-user-42", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted via entity tag users:42")
	}
}

func TestRueidisCache_InvalidateEntityTag_MultipleEntities(t *testing.T) {
	cache, _ := newTestCache(t)

	store := func(key, tag string) {
		ctx := caches.WithTags(context.Background(), tag)
		if err := cache.Store(ctx, key, &caches.Query[any]{RowsAffected: 1}); err != nil {
			t.Fatalf("Store %s: %v", key, err)
		}
	}
	store("key-1", "orders:1")
	store("key-2", "orders:2")
	store("key-3", "orders:3")

	event := &caches.InvalidationEvent{
		Tables:    []string{"orders"},
		EntityIDs: []any{1, 2},
	}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cache.Get(context.Background(), "key-1", &caches.Query[any]{}); got != nil {
		t.Error("expected key-1 evicted")
	}
	if got, _ := cache.Get(context.Background(), "key-2", &caches.Query[any]{}); got != nil {
		t.Error("expected key-2 evicted")
	}
	// orders:3 was NOT in the invalidation event — must still be present.
	if got, _ := cache.Get(context.Background(), "key-3", &caches.Query[any]{}); got == nil {
		t.Error("expected key-3 to survive (not invalidated)")
	}
}

func TestRueidisCache_TableFallback_Disabled(t *testing.T) {
	cache, _ := newTestCache(t) // no WithTableFallback
	ctx := caches.WithTags(context.Background(), "products")

	if err := cache.Store(ctx, "key-products", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Invalidate by table with no EntityIDs — should be a no-op without WithTableFallback.
	event := &caches.InvalidationEvent{Tables: []string{"products"}}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cache.Get(context.Background(), "key-products", &caches.Query[any]{}); got == nil {
		t.Fatal("expected key to survive when tableFallback is disabled")
	}
}

func TestRueidisCache_TableFallback_Enabled(t *testing.T) {
	cache, _ := newTestCache(t, gsrueidis.WithTableFallback())
	ctx := caches.WithTags(context.Background(), "products")

	if err := cache.Store(ctx, "key-products", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Invalidate by table with no EntityIDs — must evict with WithTableFallback.
	event := &caches.InvalidationEvent{Tables: []string{"products"}}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cache.Get(context.Background(), "key-products", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted when tableFallback is enabled")
	}
}

func TestRueidisCache_InvalidateDedupTags(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := caches.WithTags(context.Background(), "users:7")

	if err := cache.Store(ctx, "key-user-7", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Both event.Tags and entity expansion resolve to "users:7" — must not double-delete.
	event := &caches.InvalidationEvent{
		Tags:      []string{"users:7"},
		Tables:    []string{"users"},
		EntityIDs: []any{7},
	}
	if err := cache.Invalidate(context.Background(), event); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if got, _ := cache.Get(context.Background(), "key-user-7", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key evicted")
	}
}

func TestRueidisCache_TTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	if err != nil {
		t.Fatalf("rueidis.NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	cache := gsrueidis.NewRueidisCache(client, time.Second)
	ctx := context.Background()

	if err := cache.Store(ctx, "ttl-key", &caches.Query[any]{RowsAffected: 1}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Key must be present before expiry.
	if got, _ := cache.Get(ctx, "ttl-key", &caches.Query[any]{}); got == nil {
		t.Fatal("expected key present before TTL expires")
	}

	mr.FastForward(2 * time.Second)

	if got, _ := cache.Get(ctx, "ttl-key", &caches.Query[any]{}); got != nil {
		t.Fatal("expected key gone after TTL expiry")
	}
}
