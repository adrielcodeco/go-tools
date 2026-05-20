package apmcore_test

import (
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// TestBatchSpanEmitter_NonNilAndNoPanic is a smoke test: BatchSpanEmitter must
// return a non-nil function, and calling that function must not panic.
func TestBatchSpanEmitter_NonNilAndNoPanic(t *testing.T) {
	emitter := apmcore.BatchSpanEmitter()
	if emitter == nil {
		t.Fatal("BatchSpanEmitter() returned nil")
	}
	// Calling with zero values must not panic.
	emitter("users", 5, 10*time.Millisecond)
	emitter("", 0, 0)
}
