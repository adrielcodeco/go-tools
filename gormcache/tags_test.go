package caches

import (
	"context"
	"reflect"
	"testing"
)

func TestWithTags(t *testing.T) {
	ctx := context.Background()
	ctx = WithTags(ctx, "users", "roles")
	tags := TagsFromContext(ctx)
	expected := []string{"users", "roles"}
	if !reflect.DeepEqual(tags, expected) {
		t.Errorf("TagsFromContext() = %v, want %v", tags, expected)
	}
}

func TestWithTags_Empty(t *testing.T) {
	ctx := context.Background()
	tags := TagsFromContext(ctx)
	if tags != nil {
		t.Errorf("TagsFromContext() on empty context = %v, want nil", tags)
	}
}

func TestWithInvalidateTags(t *testing.T) {
	ctx := context.Background()
	ctx = WithInvalidateTags(ctx, "users", "posts")
	tags := invalidateTagsFromContext(ctx)
	expected := []string{"users", "posts"}
	if !reflect.DeepEqual(tags, expected) {
		t.Errorf("invalidateTagsFromContext() = %v, want %v", tags, expected)
	}
}

func TestWithInvalidateTags_Empty(t *testing.T) {
	ctx := context.Background()
	tags := invalidateTagsFromContext(ctx)
	if tags != nil {
		t.Errorf("invalidateTagsFromContext() on empty context = %v, want nil", tags)
	}
}

func TestTagsAndInvalidateTagsIndependent(t *testing.T) {
	ctx := context.Background()
	ctx = WithTags(ctx, "tag1")
	ctx = WithInvalidateTags(ctx, "tag2")

	tags := TagsFromContext(ctx)
	invTags := invalidateTagsFromContext(ctx)

	if !reflect.DeepEqual(tags, []string{"tag1"}) {
		t.Errorf("TagsFromContext() = %v, want [tag1]", tags)
	}
	if !reflect.DeepEqual(invTags, []string{"tag2"}) {
		t.Errorf("invalidateTagsFromContext() = %v, want [tag2]", invTags)
	}
}
