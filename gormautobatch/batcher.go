package autobatch

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrBatcherClosed is returned when an op is submitted to a closed batcher.
var ErrBatcherClosed = errors.New("autobatch: batcher closed")

// pendingOp is one buffered database operation waiting to be executed in a batch.
//
// The dest/model/table fields are a snapshot taken at submit time so the flush
// goroutine never reads from the caller's *gorm.DB.Statement (which the caller
// or other plugins could mutate concurrently).
type pendingOp struct {
	dest    any              // snapshot of Statement.Dest
	model   any              // snapshot of Statement.Model (nil if not set)
	table   string           // snapshot of Statement.Table (empty if not set)
	clauses []clause.Expression // snapshot of Statement.Clauses (e.g. ON CONFLICT)
	caller  *gorm.DB         // caller's DB — receive RowsAffected/Error after flush
	ctx     context.Context
	done    chan struct{} // closed after the batch containing this op has executed
	err     error         // written before done is closed; caller reads after
	rows    int64         // RowsAffected from flush; copied into caller after wait
}

// batcher collects pending operations and flushes them in batches.
type batcher struct {
	mu           sync.Mutex
	pending      []*pendingOp
	timer        *time.Timer
	timerGen     uint64 // monotonic generation; timer callbacks check before clearing
	flushTimeout time.Duration
	maxSize      int
	flushFn      func([]*pendingOp) // executes the batch; sets op.err, closes op.done
	closed       bool
}

func newBatcher(flushTimeout time.Duration, maxSize int, flushFn func([]*pendingOp)) *batcher {
	return &batcher{
		flushTimeout: flushTimeout,
		maxSize:      maxSize,
		flushFn:      flushFn,
	}
}

// submit enqueues op and returns immediately. The caller blocks on op.done
// to wait for the batch result. Returns ErrBatcherClosed if the batcher was
// closed before submit.
func (b *batcher) submit(op *pendingOp) error {
	b.mu.Lock()

	if b.closed {
		b.mu.Unlock()
		op.err = ErrBatcherClosed
		close(op.done)
		return ErrBatcherClosed
	}

	b.pending = append(b.pending, op)

	// Start timer if this is the first op in a batch.
	if len(b.pending) == 1 {
		if b.timer != nil {
			b.timer.Stop()
		}
		b.timerGen++
		gen := b.timerGen
		b.timer = time.AfterFunc(b.flushTimeout, func() { b.timerFlush(gen) })
	}

	// Flush immediately if maxSize reached.
	if len(b.pending) >= b.maxSize {
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		// Bump generation so any in-flight timer callback for this batch is
		// invalidated and cannot clobber a future timer.
		b.timerGen++
		batch := b.drain()
		b.mu.Unlock()
		if len(batch) > 0 {
			b.flushFn(batch)
		}
		return nil
	}

	b.mu.Unlock()
	return nil
}

// timerFlush is invoked by time.AfterFunc when the flush timeout expires.
// gen is the generation that armed the timer; if it no longer matches the
// current generation, a size-trigger or Close already drained, so this
// callback is stale and must not touch state.
func (b *batcher) timerFlush(gen uint64) {
	b.mu.Lock()
	if gen != b.timerGen {
		b.mu.Unlock()
		return
	}
	batch := b.drain()
	b.timer = nil
	b.mu.Unlock()

	if len(batch) > 0 {
		b.flushFn(batch)
	}
}

// drain atomically removes all pending ops and returns them.
// Must be called with b.mu held.
func (b *batcher) drain() []*pendingOp {
	if len(b.pending) == 0 {
		return nil
	}
	batch := b.pending
	b.pending = nil
	return batch
}

// close stops accepting new submits and flushes any pending ops synchronously.
func (b *batcher) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.timerGen++
	batch := b.drain()
	b.mu.Unlock()

	if len(batch) > 0 {
		b.flushFn(batch)
	}
}

// wait blocks until op's batch has been executed or ctx is cancelled.
// On normal completion, copies RowsAffected back into the caller's *gorm.DB.
func wait(ctx context.Context, op *pendingOp) error {
	select {
	case <-op.done:
		if op.caller != nil {
			op.caller.RowsAffected = op.rows
		}
		return op.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newPendingOp captures a snapshot of the caller's Statement at submit time.
// The flush goroutine reads only from the snapshot — never from db.Statement —
// so concurrent mutations on the caller side cannot race.
func newPendingOp(db *gorm.DB) *pendingOp {
	return &pendingOp{
		dest:    db.Statement.Dest,
		model:   db.Statement.Model,
		table:   db.Statement.Table,
		clauses: snapshotClauses(db.Statement),
		caller:  db,
		ctx:     db.Statement.Context,
		done:    make(chan struct{}),
	}
}

// snapshotClauses copies the caller's statement-level clauses (e.g. the
// ON CONFLICT set by db.Clauses(clause.OnConflict{...}), or RETURNING/locking
// clauses) so the flush goroutine can re-apply them. Without this, a batched
// upsert silently degrades into a plain INSERT and races on a unique index
// fail with a duplicate-key error instead of being absorbed by ON CONFLICT.
//
// GORM stores user-supplied clauses keyed by their Name() in Statement.Clauses.
// We re-apply them via db.Clauses(...) on the flush session, which re-keys them
// the same way, so order does not matter.
func snapshotClauses(stmt *gorm.Statement) []clause.Expression {
	if len(stmt.Clauses) == 0 {
		return nil
	}
	out := make([]clause.Expression, 0, len(stmt.Clauses))
	for _, c := range stmt.Clauses {
		if c.Expression != nil {
			out = append(out, c.Expression)
		}
	}
	return out
}
