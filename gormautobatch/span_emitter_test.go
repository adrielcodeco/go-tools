package autobatch_test

import (
	"sync"
	"testing"
	"time"

	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
)

// TestSpanEmitter_CalledAfterFlush verifies that SpanEmitter is invoked after a
// batch flush with the correct table name and a positive op count.
func TestSpanEmitter_CalledAfterFlush(t *testing.T) {
	db := openBatchDB(t)

	type emission struct {
		table   string
		ops     int
		elapsed time.Duration
	}

	var mu sync.Mutex
	var emissions []emission

	plugin := autobatch.New(autobatch.Config{
		LatencyThreshold: durPtr(0), // always batch
		FlushTimeout:     20 * time.Millisecond,
		MaxBatchSize:     50,
		SpanEmitter: func(table string, ops int, elapsed time.Duration) {
			mu.Lock()
			emissions = append(emissions, emission{table, ops, elapsed})
			mu.Unlock()
		},
	})
	if err := db.Use(plugin); err != nil {
		t.Fatalf("db.Use: %v", err)
	}

	// Submit three concurrent creates so they land in the same batch.
	const n = 3
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := db.Create(&Product{Name: "span-" + string(rune('a'+idx)), Price: float64(idx)}).Error; err != nil {
				t.Errorf("create %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Wait for the async flush timer to fire.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(emissions) == 0 {
		t.Fatal("SpanEmitter was not called after batch flush")
	}

	total := 0
	for _, e := range emissions {
		if e.table != "products" {
			t.Errorf("expected table %q, got %q", "products", e.table)
		}
		if e.ops <= 0 {
			t.Errorf("expected ops > 0, got %d", e.ops)
		}
		if e.elapsed <= 0 {
			t.Errorf("expected elapsed > 0, got %v", e.elapsed)
		}
		total += e.ops
	}
	if total < n {
		t.Errorf("expected at least %d total ops across emissions, got %d", n, total)
	}
}
