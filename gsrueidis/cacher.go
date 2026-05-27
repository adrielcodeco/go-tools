package gsrueidis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/rueidis"

	caches "github.com/adrielcodeco/go-tools/gormcache"
)

const tagKeyPrefix = "tag:"

// RueidisCache implements caches.Cacher using a rueidis.Client.
//
// Tag-based invalidation: Store indexes each cache key under its tags via
// SADD tag:<tag> <key>. Invalidate removes all keys for each tag in the
// event and then removes the tag set itself.
//
// Table-based invalidation only works if the caller configures
// caches.Config.TagsFunc to emit table names as tags — the Store path has no
// access to table information. Without TagsFunc, table-based invalidation is
// a no-op and the caller must rely on TTL expiry.
type RueidisCache struct {
	client rueidis.Client
	ttl    time.Duration
}

// NewRueidisCache creates a RueidisCache backed by client with the given TTL.
// ttl=0 disables expiry (keys persist until explicitly invalidated).
func NewRueidisCache(client rueidis.Client, ttl time.Duration) *RueidisCache {
	return &RueidisCache{client: client, ttl: ttl}
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
	for _, tag := range event.Tags {
		setKey := tagKeyPrefix + tag
		members, err := r.client.Do(ctx, r.client.B().Smembers().Key(setKey).Build()).AsStrSlice()
		if err != nil {
			return err
		}
		if len(members) == 0 {
			continue
		}

		// Delete all member keys plus the tag set in a single DEL command.
		allKeys := append(members, setKey)
		delCmd := r.client.B().Del().Key(allKeys[0]).Key(allKeys[1:]...).Build()
		if err := r.client.Do(ctx, delCmd).Error(); err != nil {
			return err
		}
	}
	return nil
}
