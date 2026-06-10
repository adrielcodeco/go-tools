package gsrueidis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/rueidis"

	caches "github.com/adrielcodeco/go-tools/gormcache"
)

const tagKeyPrefix = "tag:"

// RueidisCache implements caches.Cacher using a rueidis.Client.
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
type RueidisCache struct {
	client rueidis.Client
	ttl    time.Duration
}

// Option configures a RueidisCache.
type Option func(*RueidisCache)

// WithTableFallback is deprecated and has no effect: whole-table tag
// invalidation is now always enabled. Entries indexed under a plain "<table>"
// tag were previously only evicted when a mutation had no resolvable
// EntityIDs, which silently left stale entries behind on every single-row
// INSERT/UPDATE/DELETE.
//
// Deprecated: table tags are always invalidated; this option is a no-op.
func WithTableFallback() Option {
	return func(r *RueidisCache) {}
}

// NewRueidisCache creates a RueidisCache backed by client with the given TTL.
// ttl=0 disables expiry (keys persist until explicitly invalidated).
func NewRueidisCache(client rueidis.Client, ttl time.Duration, opts ...Option) *RueidisCache {
	r := &RueidisCache{client: client, ttl: ttl}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *RueidisCache) Get(ctx context.Context, key string, q *caches.Query[any]) (*caches.Query[any], error) {
	cmd := r.client.B().Get().Key(key).Build()
	b, err := r.client.Do(ctx, cmd).AsBytes()
	if err != nil {
		if errors.Is(err, rueidis.Nil) {
			return nil, nil
		}
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

func (r *RueidisCache) Store(ctx context.Context, key string, val *caches.Query[any]) error {
	b, err := val.Marshal()
	if err != nil {
		return err
	}

	tags := caches.TagsFromContext(ctx)

	cmds := make([]rueidis.Completed, 0, 1+len(tags)*2)
	if r.ttl > 0 {
		cmds = append(cmds, r.client.B().Set().Key(key).Value(string(b)).Ex(r.ttl).Build())
	} else {
		cmds = append(cmds, r.client.B().Set().Key(key).Value(string(b)).Build())
	}

	for _, tag := range tags {
		cmds = append(cmds, r.client.B().Sadd().Key(tagKeyPrefix+tag).Member(key).Build())
		if r.ttl > 0 {
			cmds = append(cmds, r.client.B().Expire().Key(tagKeyPrefix+tag).Seconds(int64(r.ttl.Seconds())).Build())
		}
	}

	for _, res := range r.client.DoMulti(ctx, cmds...) {
		if err := res.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RueidisCache) Invalidate(ctx context.Context, event *caches.InvalidationEvent) error {
	seen := make(map[string]struct{})
	tags := make([]string, 0, len(event.Tags)+len(event.Tables)*len(event.EntityIDs))

	add := func(t string) {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			tags = append(tags, t)
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

	for _, tag := range tags {
		setKey := tagKeyPrefix + tag
		members, err := r.client.Do(ctx, r.client.B().Smembers().Key(setKey).Build()).AsStrSlice()
		if err != nil {
			return err
		}
		if len(members) == 0 {
			continue
		}

		// Delete each member key and the tag set individually to remain
		// compatible with cluster mode (multi-key DEL across slots panics).
		delCmds := make([]rueidis.Completed, 0, len(members)+1)
		for _, mk := range members {
			delCmds = append(delCmds, r.client.B().Del().Key(mk).Build())
		}
		delCmds = append(delCmds, r.client.B().Del().Key(setKey).Build())
		for _, res := range r.client.DoMulti(ctx, delCmds...) {
			if err := res.Error(); err != nil {
				return err
			}
		}
	}
	return nil
}
