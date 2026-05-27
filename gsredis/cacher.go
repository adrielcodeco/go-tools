package gsredis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	caches "github.com/adrielcodeco/go-tools/gormcache"
)

const tagKeyPrefix = "tag:"

// RedisCacher implements caches.Cacher using a go-redis UniversalClient.
//
// Tag-based invalidation: Store indexes each cache key under its tags via
// SADD tag:<tag> <key>. Invalidate removes all keys for each tag in the
// event and then removes the tag set itself.
//
// Table-based invalidation only works if the caller configures
// caches.Config.TagsFunc to emit table names as tags — the Store path has no
// access to table information, so table→key index entries cannot be built
// automatically. Without TagsFunc, table-based invalidation is a no-op and
// the caller must rely on TTL expiry.
type RedisCacher struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisCacher creates a RedisCacher backed by client with the given TTL.
// ttl=0 disables expiry (keys persist until explicitly invalidated).
func NewRedisCacher(client redis.UniversalClient, ttl time.Duration) *RedisCacher {
	return &RedisCacher{client: client, ttl: ttl}
}

func (r *RedisCacher) Get(ctx context.Context, key string, q *caches.Query[any]) (*caches.Query[any], error) {
	b, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res := &caches.Query[any]{
		Dest:         q.Dest,
		RowsAffected: q.RowsAffected,
	}
	if err := res.Unmarshal(b); err != nil {
		return nil, err
	}
	return res, nil
}

func (r *RedisCacher) Store(ctx context.Context, key string, val *caches.Query[any]) error {
	b, err := val.Marshal()
	if err != nil {
		return err
	}

	tags := caches.TagsFromContext(ctx)
	if len(tags) == 0 {
		// No tags — skip pipeline overhead and use a plain SET.
		return r.client.Set(ctx, key, b, r.ttl).Err()
	}

	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, b, r.ttl)
	for _, tag := range tags {
		pipe.SAdd(ctx, tagKeyPrefix+tag, key)
		if r.ttl > 0 {
			pipe.Expire(ctx, tagKeyPrefix+tag, r.ttl)
		}
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisCacher) Invalidate(ctx context.Context, event *caches.InvalidationEvent) error {
	if len(event.Tags) == 0 {
		return nil
	}

	setKeys := make([]string, len(event.Tags))
	for i, tag := range event.Tags {
		setKeys[i] = tagKeyPrefix + tag
	}

	return r.deleteKeySets(ctx, setKeys)
}

// deleteKeySets fetches members of each set, then deletes all member keys and
// the set keys themselves in a single pipeline per set.
func (r *RedisCacher) deleteKeySets(ctx context.Context, setKeys []string) error {
	for _, setKey := range setKeys {
		keys, err := r.client.SMembers(ctx, setKey).Result()
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			continue
		}
		// Delete all cached keys and the tag set in one pipeline.
		pipe := r.client.Pipeline()
		pipe.Del(ctx, keys...)
		pipe.Del(ctx, setKey)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
