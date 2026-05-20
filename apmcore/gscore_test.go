package apmcore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// fakeRegistrar captures arguments passed to RegisterCloser so tests can
// assert on them without importing gscore.
type fakeRegistrar struct {
	name    string
	phase   int
	timeout time.Duration
	fn      func(context.Context) error
}

func (f *fakeRegistrar) RegisterCloser(name string, phase int, timeout time.Duration, fn func(context.Context) error) {
	f.name = name
	f.phase = phase
	f.timeout = timeout
	f.fn = fn
}

func (f *fakeRegistrar) RegisterCloserWithPriority(name string, phase int, _ int, timeout time.Duration, fn func(context.Context) error) {
	f.RegisterCloser(name, phase, timeout, fn)
}

// --- RegisterWithManager tests -----------------------------------------------

func TestRegisterWithManager_NilFn(t *testing.T) {
	mgr := &fakeRegistrar{}
	// Must not panic and must not call RegisterCloser.
	apmcore.RegisterWithManager(nil, mgr, 4, 0)
	if mgr.name != "" {
		t.Errorf("expected no registration, but RegisterCloser was called with name %q", mgr.name)
	}
}

func TestRegisterWithManager_NilMgr(t *testing.T) {
	shutdown, err := apmcore.SetupOTelSDK(context.Background())
	if err != nil {
		t.Fatalf("SetupOTelSDK: %v", err)
	}
	// Must not panic when mgr is nil.
	apmcore.RegisterWithManager(shutdown, nil, 4, 0)
	// Clean up the shutdown fn even though it was not registered.
	_ = shutdown(context.Background())
}

func TestRegisterWithManager_Registers(t *testing.T) {
	shutdown, err := apmcore.SetupOTelSDK(context.Background())
	if err != nil {
		t.Fatalf("SetupOTelSDK: %v", err)
	}

	mgr := &fakeRegistrar{}
	apmcore.RegisterWithManager(shutdown, mgr, 4, 0)

	// Name must match the documented constant.
	if mgr.name != "apmcore-otel-shutdown" {
		t.Errorf("name = %q, want %q", mgr.name, "apmcore-otel-shutdown")
	}
	// Timeout must default to 15s when 0 is passed.
	if mgr.timeout != 15*time.Second {
		t.Errorf("timeout = %v, want 15s", mgr.timeout)
	}
	// The registered fn must call through to the shutdown fn without error.
	if mgr.fn == nil {
		t.Fatal("fn was not set on registrar")
	}
	if err := mgr.fn(context.Background()); err != nil {
		t.Errorf("calling registered fn: %v", err)
	}
}

func TestRegisterWithManager_DefaultPhase(t *testing.T) {
	shutdown, err := apmcore.SetupOTelSDK(context.Background())
	if err != nil {
		t.Fatalf("SetupOTelSDK: %v", err)
	}
	defer shutdown(context.Background()) //nolint:errcheck

	mgr := &fakeRegistrar{}
	// Pass phase=0 — must be normalised to PhasePostDB (4).
	apmcore.RegisterWithManager(shutdown, mgr, 0, 0)

	if mgr.phase != 4 {
		t.Errorf("phase = %d, want 4 (PhasePostDB)", mgr.phase)
	}
}

// --- RegisterDBPoolMetricsWithManager tests ----------------------------------

func TestRegisterDBPoolMetricsWithManager_NilDB(t *testing.T) {
	mgr := &fakeRegistrar{}
	// Must not panic.
	apmcore.RegisterDBPoolMetricsWithManager(nil, mgr, 4, 0)
	if mgr.name != "" {
		t.Errorf("expected no registration, but RegisterCloser was called")
	}
}

func TestRegisterDBPoolMetricsWithManager_Registers(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	mgr := &fakeRegistrar{}
	apmcore.RegisterDBPoolMetricsWithManager(sqlDB, mgr, 4, 0)

	if mgr.name != "apmcore-pool-metrics" {
		t.Errorf("name = %q, want %q", mgr.name, "apmcore-pool-metrics")
	}
	if mgr.phase != 4 {
		t.Errorf("phase = %d, want 4", mgr.phase)
	}
	if mgr.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", mgr.timeout)
	}
	if mgr.fn == nil {
		t.Fatal("fn was not set on registrar")
	}
	// Calling the closer must invoke deregister without panicking or erroring.
	if err := mgr.fn(context.Background()); err != nil {
		t.Errorf("closer fn returned error: %v", err)
	}
}
