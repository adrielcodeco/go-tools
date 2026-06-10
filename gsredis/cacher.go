package gsredis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	caches "github.com/adrielcodeco/go-tools/gormcache"
)

const tagKeyPrefix = "tag:"

// RedisCacher implements caches.Cacher using a go-redis UniversalClient.
//
// Tag-based invalidation: Store indexes each cache key under its tags via
// SADD tag:<tag> <key>. Invalidate resolves tags to evict from three sources:
//
//  1. event.Tags — explicit tags set via WithInvalidateTags on the context.
//  2. table tags — "<table>" for each table in event.Tables, always. This
//     matches entries tagged with the plain table name (the common TagsFunc
//     setup) so any mutation on a table evicts its cached SELECTs.
//  3. entity tags — "<table>:<id>" for each combination of event.Tables x
//     event.EntityIDs, for entries tagged at entity granularity via WithTags.
//
// Tag→key index entries are only built when the caller configures
// caches.Config.TagsFunc (or uses caches.WithTags at query sites) — the Store
// path has no access to table information on its own. Without tags on Store,
// invalidation is a no-op and the caller must rely on TTL expiry.
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
	seen := make(map[string]struct{})
	setKeys := make([]string, 0, len(event.Tags)+len(event.Tables)*(1+len(event.EntityIDs)))

	add := func(t string) {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			setKeys = append(setKeys, tagKeyPrefix+t)
		}
	}

	for _, t := range event.Tags {
		add(t)
	}

	for _, table := range event.Tables {
		add(table)
		for _, id := range event.EntityIDs {
			add(fmt.Sprintf("%s:%v", table, id))
		}
	}

	if len(setKeys) == 0 {
		return nil
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
