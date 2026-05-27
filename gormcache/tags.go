package caches

import "context"

type tagsKeyType struct{}
type invalidateTagsKeyType struct{}

// WithTags associates cache tags with the context.
// Used internally by the caching system to tag cache entries.
func WithTags(ctx context.Context, tags ...string) context.Context {
	return context.WithValue(ctx, tagsKeyType{}, tags)
}

// WithInvalidateTags sets tags on the context to indicate which cache entries
// should be invalidated during a mutation. Use this in your application code
// before performing CREATE/UPDATE/DELETE operations.
func WithInvalidateTags(ctx context.Context, tags ...string) context.Context {
	return context.WithValue(ctx, invalidateTagsKeyType{}, tags)
}

// TagsFromContext returns the cache tags stored on ctx by WithTags.
// Cacher implementations use this in Store to build tag→key index entries.
func TagsFromContext(ctx context.Context) []string {
	tags, _ := ctx.Value(tagsKeyType{}).([]string)
	return tags
}

func invalidateTagsFromContext(ctx context.Context) []string {
	tags, _ := ctx.Value(invalidateTagsKeyType{}).([]string)
	return tags
}
