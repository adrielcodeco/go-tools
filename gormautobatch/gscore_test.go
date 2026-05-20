package autobatch_test

import (
	"context"
	"errors"
	"testing"
	"time"

	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
)

// fakeRegistrar captures the arguments passed to RegisterCloser for inspection.
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

func TestRegisterWithManager_NilPlugin(t *testing.T) {
	reg := &fakeRegistrar{}
	// Must not panic when plugin is nil.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RegisterWithManager panicked with nil plugin: %v", r)
		}
	}()
	autobatch.RegisterWithManager(nil, reg, 2, 0)
}

func TestRegisterWithManager_NilMgr(t *testing.T) {
	plugin := autobatch.New(autobatch.Config{
		LatencyThreshold: durPtr(0),
		FlushTimeout:     50 * time.Millisecond,
	})
	// Must not panic when mgr is nil.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RegisterWithManager panicked with nil mgr: %v", r)
		}
	}()
	autobatch.RegisterWithManager(plugin, nil, 2, 0)
}

func TestRegisterWithManager_Registers(t *testing.T) {
	db := openBatchDB(t)

	plugin := autobatch.New(autobatch.Config{
		LatencyThreshold: durPtr(0),
		FlushTimeout:     50 * time.Millisecond,
	})
	if err := db.Use(plugin); err != nil {
		t.Fatalf("db.Use: %v", err)
	}

	reg := &fakeRegistrar{}
	autobatch.RegisterWithManager(plugin, reg, 2, 0)

	if reg.name != "autobatch" {
		t.Errorf("name = %q, want %q", reg.name, "autobatch")
	}
	if reg.phase != 2 {
		t.Errorf("phase = %d, want 2", reg.phase)
	}
	if reg.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s (default when 0 passed)", reg.timeout)
	}
	if reg.fn == nil {
		t.Fatal("registered fn must not be nil")
	}

	// Calling the closer fn must call plugin.Close().
	if err := reg.fn(context.Background()); err != nil {
		t.Fatalf("closer fn returned error: %v", err)
	}

	// After Close, new DB ops should return ErrBatcherClosed.
	err := db.Create(&Product{Name: "after-close", Price: 1.0}).Error
	if !errors.Is(err, autobatch.ErrBatcherClosed) {
		t.Fatalf("expected ErrBatcherClosed after closer fn ran, got %v", err)
	}
}
