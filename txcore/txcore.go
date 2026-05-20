// Package txcore is the framework-agnostic engine behind the txctx (Fiber v2)
// and txctxv3 (Fiber v3) middlewares. It owns the request-scoped transaction
// holder, the lazy ConnPool wrapper, and the commit/rollback orchestration.
//
// The public packages (txctx, txctxv3) are thin Fiber adapters: they extract a
// context.Context from the framework's request type and delegate everything
// here.
package txcore

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

type ctxKey struct{}

// activeWg tracks holders that have been started but not yet finished
// (committed or rolled back, including compensation callbacks).
// Only active after RegisterWithManager is called.
var (
	activeWg      sync.WaitGroup
	trackingActive atomic.Bool
)

func wgAdd() {
	if trackingActive.Load() {
		activeWg.Add(1)
	}
}
func wgDone() {
	if trackingActive.Load() {
		activeWg.Done()
	}
}

// CloserRegistrar is the subset of gscore.Manager used by RegisterWithManager.
// Accepting an interface instead of the concrete *gscore.Manager avoids a hard
// dependency on gscore from txcore.
type CloserRegistrar interface {
	RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error)
}

// RegisterWithManager registers a PhasePostDrain closer that blocks until all
// active transaction holders (including their compensation callbacks) finish.
// This prevents gscore's PhaseDB from closing the *sql.DB while OnRollback or
// OnCommit callbacks are still issuing queries.
//
// Calling this also activates per-holder WaitGroup tracking: every subsequent
// NewHolder call contributes to the counter, and Commit/Rollback decrements it.
//
// phase must be gscore.PhasePostDrain (value 2).
//
//	txcore.RegisterWithManager(mgr, gscore.PhasePostDrain, 35*time.Second)
func RegisterWithManager(mgr CloserRegistrar, phase int, timeout time.Duration) {
	trackingActive.Store(true)
	mgr.RegisterCloser("txcore-drain", phase, timeout, func(_ context.Context) error {
		activeWg.Wait()
		return nil
	})
}

// Holder is the request-scoped transaction state.
type Holder struct {
	mu              sync.Mutex
	db              *gorm.DB
	tx              *gorm.DB
	started         bool
	committed       bool
	tracked         bool // true when Track() was called; gates wgDone() in Commit/Rollback
	onRollback      []func(*gorm.DB) error
	onCommit        []func(*gorm.DB) error
	compensCtx      time.Duration
	lazy            bool
	onCallbackError func(error)
	reqCtx          context.Context // set on first Begin; used for OnCommit callback DB
}

// NewHolder constructs a Holder. The caller (middleware) is responsible for
// placing it in a context via Inject. When RegisterWithManager has been called,
// the holder automatically registers with the global WaitGroup and Commit /
// Rollback will decrement it after compensation callbacks finish.
func NewHolder(db *gorm.DB, compensCtx time.Duration, lazy bool, onErr func(error)) *Holder {
	h := &Holder{
		db:              db,
		compensCtx:      compensCtx,
		lazy:            lazy,
		onCallbackError: onErr,
	}
	if trackingActive.Load() {
		h.tracked = true
		wgAdd()
	}
	return h
}

// Inject returns a new context carrying h.
func Inject(parent context.Context, h *Holder) context.Context {
	return context.WithValue(parent, ctxKey{}, h)
}

// FromCtx returns the Holder stored in ctx, or nil.
func FromCtx(ctx context.Context) *Holder {
	h, _ := ctx.Value(ctxKey{}).(*Holder)
	return h
}

// MustFromCtx is FromCtx but panics if no holder is present.
func MustFromCtx(ctx context.Context) *Holder {
	h := FromCtx(ctx)
	if h == nil {
		panic("txctx: no transaction holder in context — did you add the txctx middleware?")
	}
	return h
}

// Begin opens the underlying *sql.Tx exactly once. Subsequent calls are
// no-ops.
func (h *Holder) Begin(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.started {
		return
	}
	h.reqCtx = ctx
	h.tx = h.db.WithContext(ctx).Begin()
	h.started = true
}

// Rollback rolls back the transaction (if any) and runs all OnRollback
// callbacks with a fresh context whose deadline is compensCtx.
func (h *Holder) Rollback() {
	h.mu.Lock()
	tx := h.tx
	started := h.started
	committed := h.committed
	callbacks := h.onRollback
	onErr := h.onCallbackError
	compensCtx := h.compensCtx
	tracked := h.tracked
	h.mu.Unlock()

	if started && tx != nil && !committed {
		if err := tx.Rollback().Error; err != nil && onErr != nil {
			onErr(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), compensCtx)
	defer cancel()
	compDB := h.db.WithContext(ctx)
	for _, fn := range callbacks {
		if err := fn(compDB); err != nil && onErr != nil {
			onErr(err)
		}
	}

	if tracked {
		wgDone()
	}
}

// Commit returns (commitErr, postCommitErr). commitErr is set if tx.Commit
// itself failed — caller should treat as rollback. postCommitErr is set if
// an OnCommit callback failed after a successful commit — caller MUST NOT
// rollback (the tx is already durable).
func (h *Holder) Commit() (commitErr error, postCommitErr error) {
	h.mu.Lock()
	tx := h.tx
	started := h.started
	callbacks := h.onCommit
	onErr := h.onCallbackError
	tracked := h.tracked
	h.mu.Unlock()

	bg := h.db.WithContext(context.Background())

	if started && tx != nil {
		if err := tx.Commit().Error; err != nil {
			if tracked {
				wgDone()
			}
			return err, nil
		}
		h.mu.Lock()
		h.committed = true
		h.mu.Unlock()
	}

	for _, fn := range callbacks {
		if err := fn(bg); err != nil {
			if onErr != nil {
				onErr(err)
			}
			if tracked {
				wgDone()
			}
			return nil, err
		}
	}
	if tracked {
		wgDone()
	}
	return nil, nil
}

// DB returns the request-scoped *gorm.DB. With lazy mode, the first write
// transparently opens BEGIN; reads stay outside any transaction.
func (h *Holder) DB(ctx context.Context) *gorm.DB {
	h.mu.Lock()
	started := h.started
	lazy := h.lazy
	h.mu.Unlock()

	if started {
		return h.tx.WithContext(ctx)
	}
	if !lazy {
		return h.db.WithContext(ctx)
	}
	return lazyDB(ctx, h)
}

// Outside returns a *gorm.DB whose context is decoupled from the request
// cancellation but preserves request values.
func (h *Holder) Outside(ctx context.Context) *gorm.DB {
	return h.db.WithContext(context.WithoutCancel(ctx))
}

// AppendOnRollback registers fn to run if the transaction rolls back.
func (h *Holder) AppendOnRollback(fn func(*gorm.DB) error) {
	h.mu.Lock()
	h.onRollback = append(h.onRollback, fn)
	h.mu.Unlock()
}

// AppendOnCommit registers fn to run only if the transaction commits
// successfully.
func (h *Holder) AppendOnCommit(fn func(*gorm.DB) error) {
	h.mu.Lock()
	h.onCommit = append(h.onCommit, fn)
	h.mu.Unlock()
}

// --- test/introspection accessors (used by tests) ---

func (h *Holder) Started() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.started
}

func (h *Holder) Tx() *gorm.DB {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tx
}

func (h *Holder) BaseDB() *gorm.DB {
	return h.db
}

func (h *Holder) OnRollbackLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.onRollback)
}

func (h *Holder) OnCommitLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.onCommit)
}
