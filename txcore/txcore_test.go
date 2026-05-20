package txcore

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type item struct {
	ID   uint   `gorm:"primarykey"`
	Name string `gorm:"not null"`
}

func (item) TableName() string { return "items" }

var testDBCounter atomic.Int64

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	id := testDBCounter.Add(1)
	dsn := fmt.Sprintf("file:txcore%d?mode=memory&cache=shared", id)
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db, err := gorm.Open(sqlite.Dialector{DSN: dsn, Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func injectHolder(db *gorm.DB) (context.Context, *Holder) {
	h := NewHolder(db, 5*time.Second, true, nil)
	return Inject(context.Background(), h), h
}

func TestFromCtxNil(t *testing.T) {
	if FromCtx(context.Background()) != nil {
		t.Error("expected nil holder for plain context")
	}
}

func TestMustFromCtxPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustFromCtx should panic when no holder")
		}
	}()
	MustFromCtx(context.Background())
}

func TestHolderBeginIdempotent(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	h.Begin(ctx)
	first := h.Tx()
	h.Begin(ctx)
	second := h.Tx()
	if first != second {
		t.Error("Begin must be idempotent")
	}
}

func TestHolderCommitNoTx(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)

	var fired int32
	h.AppendOnCommit(func(_ *gorm.DB) error {
		atomic.StoreInt32(&fired, 1)
		return nil
	})
	commitErr, postErr := h.Commit()
	if commitErr != nil || postErr != nil {
		t.Fatalf("unexpected: %v / %v", commitErr, postErr)
	}
	if atomic.LoadInt32(&fired) == 0 {
		t.Error("OnCommit must fire even with no tx")
	}
}

func TestHolderRollbackNoTx(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)

	var fired int32
	h.AppendOnRollback(func(_ *gorm.DB) error {
		atomic.StoreInt32(&fired, 1)
		return nil
	})
	h.Rollback()
	if atomic.LoadInt32(&fired) == 0 {
		t.Error("OnRollback must fire even with no tx")
	}
}

func TestOutsideIndependentOfTx(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	out := h.Outside(ctx)
	if err := out.Create(&item{Name: "outside-unit"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got item
	if err := db.Where("name = ?", "outside-unit").First(&got).Error; err != nil {
		t.Errorf("record should be visible: %v", err)
	}
}

func TestOnCallbackErrorCapturesRollback(t *testing.T) {
	db := setupTestDB(t)
	var captured error
	h := NewHolder(db, 1*time.Second, true, func(err error) { captured = err })

	h.AppendOnRollback(func(_ *gorm.DB) error {
		return fmt.Errorf("compensation boom")
	})
	h.Rollback()
	if captured == nil {
		t.Error("OnCallbackError should have captured the OnRollback error")
	}
}

func TestDBLazyReturnsBaseWhenNotStarted(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	got := h.DB(ctx)
	if got == nil {
		t.Fatal("DB returned nil")
	}
}

func TestDBEagerReturnsBaseWhenSomehowNotStarted(t *testing.T) {
	db := setupTestDB(t)
	h := NewHolder(db, 1*time.Second, false, nil)
	ctx := Inject(context.Background(), h)
	got := h.DB(ctx)
	if got == nil {
		t.Fatal("DB returned nil")
	}
}

func TestConfigWithDefaults(t *testing.T) {
	cfg := Config{}.WithDefaults()
	if cfg.Timeout != 30*time.Second {
		t.Errorf("default Timeout: want 30s, got %v", cfg.Timeout)
	}
	if cfg.CompensationCtx != 5*time.Second {
		t.Errorf("default CompensationCtx: want 5s, got %v", cfg.CompensationCtx)
	}
	if cfg.LazyTx == nil || !*cfg.LazyTx {
		t.Errorf("default LazyTx should be true, got %v", cfg.LazyTx)
	}
}

func TestConfigPreservesExplicit(t *testing.T) {
	cfg := Config{
		Timeout:         10 * time.Second,
		CompensationCtx: 2 * time.Second,
		LazyTx:          BoolPtr(false),
	}.WithDefaults()
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout not preserved")
	}
	if cfg.CompensationCtx != 2*time.Second {
		t.Errorf("CompensationCtx not preserved")
	}
	if cfg.LazyTx == nil || *cfg.LazyTx {
		t.Errorf("LazyTx should stay false")
	}
}

func TestIsWriteHeuristic(t *testing.T) {
	cases := []struct {
		q       string
		isWrite bool
	}{
		{"SELECT * FROM t", false},
		{"  select * from t", false},
		{"INSERT INTO t VALUES (1)", true},
		{"UPDATE t SET x=1", true},
		{"DELETE FROM t", true},
		{"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x", true},
		{"", true},
	}
	for _, c := range cases {
		if got := isWrite(c.q); got != c.isWrite {
			t.Errorf("isWrite(%q) = %v, want %v", c.q, got, c.isWrite)
		}
	}
}

func TestLazyPoolDirect(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	pool := &lazyPool{h: h, base: db.ConnPool}

	row := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM items")
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("QueryRowContext: %v", err)
	}

	stmt, err := pool.PrepareContext(ctx, "INSERT INTO items(name) VALUES (?)")
	if err != nil {
		t.Fatalf("PrepareContext: %v", err)
	}
	_ = stmt.Close()

	if _, err := pool.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "exec-row"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if !h.Started() {
		t.Error("writes through lazyPool should have started the tx")
	}

	rows, err := pool.QueryContext(ctx, "SELECT name FROM items WHERE name = ?", "exec-row")
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	_ = rows.Close()
}

func TestAppendCounters(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)

	h.AppendOnRollback(func(_ *gorm.DB) error { return nil })
	h.AppendOnRollback(func(_ *gorm.DB) error { return nil })
	if h.OnRollbackLen() != 2 {
		t.Errorf("want 2 rollback, got %d", h.OnRollbackLen())
	}
	h.AppendOnCommit(func(_ *gorm.DB) error { return nil })
	h.AppendOnCommit(func(_ *gorm.DB) error { return nil })
	h.AppendOnCommit(func(_ *gorm.DB) error { return nil })
	if h.OnCommitLen() != 3 {
		t.Errorf("want 3 commit, got %d", h.OnCommitLen())
	}
}

// --- lazyPool / TxCommitter tests ---

func TestLazyPoolImplementsTxCommitter(t *testing.T) {
	// compile-time check: *lazyPool must satisfy gorm.TxCommitter
	var _ gorm.TxCommitter = (*lazyPool)(nil)
}

func TestLazyPoolCommit_BeforeStart(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)
	pool := &lazyPool{h: h, base: db.ConnPool}

	if err := pool.Commit(); err != nil {
		t.Fatalf("Commit before Begin should return nil, got: %v", err)
	}
}

func TestLazyPoolRollback_BeforeStart(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)
	pool := &lazyPool{h: h, base: db.ConnPool}

	if err := pool.Rollback(); err != nil {
		t.Fatalf("Rollback before Begin should return nil, got: %v", err)
	}
}

func TestLazyPoolTxCommitter_AfterBegin(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	pool := &lazyPool{h: h, base: db.ConnPool}

	// trigger a write so Begin is called lazily
	if _, err := pool.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "txcommitter-row"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if !h.Started() {
		t.Fatal("expected lazy tx to have started after write")
	}

	// Commit through the TxCommitter interface must not error
	if err := pool.Commit(); err != nil {
		t.Fatalf("Commit after Begin: %v", err)
	}
}

func TestLazyPoolRollback_AfterBegin(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	pool := &lazyPool{h: h, base: db.ConnPool}

	if _, err := pool.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "rollback-row"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if !h.Started() {
		t.Fatal("expected lazy tx to have started after write")
	}

	// Rollback through the TxCommitter interface must not error
	if err := pool.Rollback(); err != nil {
		t.Fatalf("Rollback after Begin: %v", err)
	}
}

func TestLazyPoolSatisfiesTxCommitter_RuntimeAssert(t *testing.T) {
	db := setupTestDB(t)
	ctx, h := injectHolder(db)

	pool := &lazyPool{h: h, base: db.ConnPool}

	// trigger a write so pool.h.tx is non-nil
	if _, err := pool.ExecContext(ctx, "INSERT INTO items(name) VALUES (?)", "iface-check"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}

	// At runtime the interface must also be satisfied (redundant with compile-time,
	// but documents the intent for isTransaction()-style checks in gormautobatch).
	if _, ok := any(pool).(gorm.TxCommitter); !ok {
		t.Error("*lazyPool must satisfy gorm.TxCommitter at runtime")
	}
}

func TestBaseDB(t *testing.T) {
	db := setupTestDB(t)
	_, h := injectHolder(db)
	if h.BaseDB() != db {
		t.Error("BaseDB should return the original *gorm.DB")
	}
}

// --- RegisterWithManager / tracking / wgAdd / wgDone ---

// fakeRegistrar implements CloserRegistrar and captures the registered closer.
type fakeRegistrar struct {
	name     string
	phase    int
	priority int
	timeout  time.Duration
	fn       func(ctx context.Context) error
}

func (r *fakeRegistrar) RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error) {
	r.name = name
	r.phase = phase
	r.timeout = timeout
	r.fn = fn
}

func (r *fakeRegistrar) RegisterCloserWithPriority(name string, phase int, priority int, timeout time.Duration, fn func(ctx context.Context) error) {
	r.name = name
	r.phase = phase
	r.priority = priority
	r.timeout = timeout
	r.fn = fn
}

// resetTracking restores global tracking state after a test that activates it.
func resetTracking(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		trackingActive.Store(false)
		// Drain any leftover WaitGroup count so other tests don't deadlock.
		// We do this by draining via Done() for any count > 0 using a channel trick:
		// actually the only safe way is to ensure tests that call wgAdd() also call wgDone().
	})
}

func TestRegisterWithManager_SetsTrackingActive(t *testing.T) {
	resetTracking(t)

	reg := &fakeRegistrar{}
	RegisterWithManager(reg, 2, 35*time.Second)

	if !trackingActive.Load() {
		t.Error("trackingActive should be true after RegisterWithManager")
	}
	if reg.name != "txcore-drain" {
		t.Errorf("expected closer name 'txcore-drain', got %q", reg.name)
	}
	if reg.phase != 2 {
		t.Errorf("expected phase 2, got %d", reg.phase)
	}
	if reg.timeout != 35*time.Second {
		t.Errorf("expected timeout 35s, got %v", reg.timeout)
	}
}

func TestRegisterWithManager_CloserDrainsWaitGroup(t *testing.T) {
	resetTracking(t)

	reg := &fakeRegistrar{}
	RegisterWithManager(reg, 2, 35*time.Second)

	if reg.fn == nil {
		t.Fatal("RegisterCloser fn must not be nil")
	}

	// Create a holder (wgAdd inside NewHolder when trackingActive), then let
	// Rollback call wgDone. The closer must then return promptly.
	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, true, nil)

	if !h.tracked {
		t.Error("holder should be tracked when trackingActive is true")
	}

	// Run the closer in a goroutine — it will block until wgDone.
	done := make(chan error, 1)
	go func() {
		done <- reg.fn(context.Background())
	}()

	// Give the goroutine a moment to block on Wait.
	time.Sleep(10 * time.Millisecond)

	select {
	case <-done:
		t.Error("closer should still be blocking — wgDone not called yet")
	default:
	}

	// Rollback decrements the WaitGroup.
	h.Rollback()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("closer fn should return nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("closer fn did not return within 2 seconds after wgDone")
	}
}

func TestNewHolder_TrackingBranch(t *testing.T) {
	resetTracking(t)
	trackingActive.Store(true)

	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, true, nil)

	if !h.tracked {
		t.Error("holder.tracked should be true when trackingActive is set")
	}

	// Must call wgDone to balance the wgAdd inside NewHolder.
	h.Rollback()
}

func TestMustFromCtx_Success(t *testing.T) {
	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, true, nil)
	ctx := Inject(context.Background(), h)

	got := MustFromCtx(ctx)
	if got != h {
		t.Error("MustFromCtx should return the same holder that was injected")
	}
}

func TestCommit_ErrorPath_WgDone(t *testing.T) {
	resetTracking(t)
	trackingActive.Store(true)

	reg := &fakeRegistrar{}
	RegisterWithManager(reg, 2, 35*time.Second)

	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, false, nil) // eager — Begin opens a real tx
	ctx := Inject(context.Background(), h)
	h.Begin(ctx)

	// Force the tx into an error state so Commit returns an error.
	// Rolling back before Commit makes the subsequent Commit fail.
	h.mu.Lock()
	_ = h.tx.Rollback()
	h.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- reg.fn(context.Background())
	}()

	time.Sleep(10 * time.Millisecond)

	commitErr, postErr := h.Commit()
	// commitErr should be non-nil (tx was already rolled back)
	// but regardless of the exact error, wgDone must have been called.
	_ = commitErr
	_ = postErr

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("closer fn should return nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("closer fn did not return — wgDone was not called on commit error path")
	}
}

func TestCommit_PostCommitError_WgDone(t *testing.T) {
	resetTracking(t)
	trackingActive.Store(true)

	reg := &fakeRegistrar{}
	RegisterWithManager(reg, 2, 35*time.Second)

	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, true, nil)

	// Register an OnCommit callback that returns an error.
	h.AppendOnCommit(func(_ *gorm.DB) error {
		return fmt.Errorf("post-commit boom")
	})

	done := make(chan error, 1)
	go func() {
		done <- reg.fn(context.Background())
	}()

	time.Sleep(10 * time.Millisecond)

	commitErr, postErr := h.Commit()
	if commitErr != nil {
		t.Logf("commitErr: %v (no tx was open, expected nil)", commitErr)
	}
	if postErr == nil {
		t.Error("expected postCommitErr to be set")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("closer fn should return nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("closer fn did not return — wgDone not called on post-commit error path")
	}
}

func TestRollback_WgDone_Tracked(t *testing.T) {
	resetTracking(t)
	trackingActive.Store(true)

	reg := &fakeRegistrar{}
	RegisterWithManager(reg, 2, 35*time.Second)

	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, true, nil)

	done := make(chan error, 1)
	go func() {
		done <- reg.fn(context.Background())
	}()

	time.Sleep(10 * time.Millisecond)

	h.Rollback()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("closer fn should return nil, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("closer fn did not return — wgDone not called in Rollback tracked path")
	}
}

// TestOnCommitCallbackReceivesReqCtx verifies that the *gorm.DB passed to an
// OnCommit callback carries the context that was passed to Begin, not
// context.Background().
func TestOnCommitCallbackReceivesReqCtx(t *testing.T) {
	db := setupTestDB(t)

	type ctxMarkerKey struct{}
	markerVal := "request-ctx-marker"
	reqCtx := context.WithValue(context.Background(), ctxMarkerKey{}, markerVal)

	h := NewHolder(db, 5*time.Second, false, nil)
	h.Begin(reqCtx)

	var gotCtx context.Context
	h.AppendOnCommit(func(callbackDB *gorm.DB) error {
		gotCtx = callbackDB.Statement.Context
		return nil
	})

	commitErr, postErr := h.Commit()
	if commitErr != nil {
		t.Fatalf("commitErr: %v", commitErr)
	}
	if postErr != nil {
		t.Fatalf("postErr: %v", postErr)
	}

	if gotCtx == nil {
		t.Fatal("OnCommit callback did not receive a context")
	}
	if got := gotCtx.Value(ctxMarkerKey{}); got != markerVal {
		t.Errorf("OnCommit DB context does not carry the request context: got %v, want %q", got, markerVal)
	}
}

// TestOnRollbackCallbackReceivesReqCtx verifies that the *gorm.DB passed to an
// OnRollback callback carries context values from the original request context
// (via context.WithoutCancel), not context.Background().
func TestOnRollbackCallbackReceivesReqCtx(t *testing.T) {
	db := setupTestDB(t)

	type ctxMarkerKey struct{}
	markerVal := "rollback-req-ctx-marker"
	reqCtx := context.WithValue(context.Background(), ctxMarkerKey{}, markerVal)

	h := NewHolder(db, 5*time.Second, false, nil)
	h.Begin(reqCtx)

	var gotCtx context.Context
	h.AppendOnRollback(func(callbackDB *gorm.DB) error {
		gotCtx = callbackDB.Statement.Context
		return nil
	})

	h.Rollback()

	if gotCtx == nil {
		t.Fatal("OnRollback callback did not receive a context")
	}
	if got := gotCtx.Value(ctxMarkerKey{}); got != markerVal {
		t.Errorf("OnRollback DB context does not carry the request context: got %v, want %q", got, markerVal)
	}
}

// TestOnRollbackCallbackFallsBackToBackground verifies that when Begin was never
// called (reqCtx is nil), OnRollback callbacks still receive a valid (background) context.
func TestOnRollbackCallbackFallsBackToBackground(t *testing.T) {
	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, false, nil)

	var gotCtx context.Context
	h.AppendOnRollback(func(callbackDB *gorm.DB) error {
		gotCtx = callbackDB.Statement.Context
		return nil
	})

	h.Rollback()

	if gotCtx == nil {
		t.Fatal("OnRollback callback did not receive a context")
	}
}

// TestOnCommitCallbackFallsBackToBackground verifies that when Begin was never
// called, OnCommit callbacks still receive a valid (background) context.
func TestOnCommitCallbackFallsBackToBackground(t *testing.T) {
	db := setupTestDB(t)
	h := NewHolder(db, 5*time.Second, false, nil)

	var gotCtx context.Context
	h.AppendOnCommit(func(callbackDB *gorm.DB) error {
		gotCtx = callbackDB.Statement.Context
		return nil
	})

	commitErr, postErr := h.Commit()
	if commitErr != nil {
		t.Fatalf("commitErr: %v", commitErr)
	}
	if postErr != nil {
		t.Fatalf("postErr: %v", postErr)
	}

	if gotCtx == nil {
		t.Fatal("OnCommit callback did not receive a context")
	}
}
