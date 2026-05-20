package gsrueidis_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/gsrueidis"
)

// fakeRegistrar implements gsrueidis.CloserRegistrar without using *gscore.Manager.
type fakeRegistrar struct {
	name    string
	phase   int
	timeout time.Duration
	fn      func(ctx context.Context) error
}

func (r *fakeRegistrar) RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error) {
	r.name = name
	r.phase = phase
	r.timeout = timeout
	r.fn = fn
}

func (r *fakeRegistrar) RegisterCloserWithPriority(name string, phase int, _ int, timeout time.Duration, fn func(ctx context.Context) error) {
	r.RegisterCloser(name, phase, timeout, fn)
}

type fakeClient struct {
	called atomic.Int32
	block  time.Duration
}

func (f *fakeClient) Close() {
	f.called.Add(1)
	if f.block > 0 {
		time.Sleep(f.block)
	}
}

func TestRegisterClosesNormally(t *testing.T) {
	m := gscore.New(gscore.Config{HookTimeout: time.Second, ForceKillAfter: 5 * time.Second})
	c := &fakeClient{}
	gsrueidis.Register(m, "redis", c, gscore.PhasePostDB, 100*time.Millisecond)

	m.Trigger()
	if err := m.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if c.called.Load() != 1 {
		t.Fatalf("expected Close called once, got %d", c.called.Load())
	}
}

func TestRegisterHonorsTimeout(t *testing.T) {
	var hookErr error
	m := gscore.New(gscore.Config{
		HookTimeout:    time.Second,
		ForceKillAfter: 5 * time.Second,
		OnHookError:    func(_ string, _ gscore.Phase, e error) { hookErr = e },
	})
	c := &fakeClient{block: 500 * time.Millisecond}
	gsrueidis.Register(m, "slow-redis", c, gscore.PhasePostDB, 50*time.Millisecond)

	start := time.Now()
	m.Trigger()
	_ = m.Wait()
	elapsed := time.Since(start)

	if elapsed > 300*time.Millisecond {
		t.Errorf("expected shutdown to abort the slow close near 50ms, took %v", elapsed)
	}
	if !errors.Is(hookErr, gsrueidis.ErrCloseTimedOut) {
		t.Errorf("expected ErrCloseTimedOut, got %v", hookErr)
	}
}

func TestRegisterNilClientIsNoop(t *testing.T) {
	m := gscore.New(gscore.Config{HookTimeout: time.Second, ForceKillAfter: time.Second})
	gsrueidis.Register(m, "nil", nil, gscore.PhasePostDB, 0) // no panic, no registration
	m.Trigger()
	if err := m.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestRegisterZeroTimeoutFallsBackToDefault(t *testing.T) {
	m := gscore.New(gscore.Config{HookTimeout: time.Second, ForceKillAfter: 10 * time.Second})
	c := &fakeClient{}
	gsrueidis.Register(m, "redis", c, gscore.PhasePostDB, 0)
	m.Trigger()
	_ = m.Wait()
	if c.called.Load() != 1 {
		t.Fatalf("expected close to be invoked once")
	}
}

// TestRegister_UsesInterface verifies that Register works with any CloserRegistrar,
// not just *gscore.Manager. This tests the LC-3 interface change.
func TestRegister_UsesInterface(t *testing.T) {
	reg := &fakeRegistrar{}
	c := &fakeClient{}

	gsrueidis.Register(reg, "test-redis", c, gscore.PhasePostDB, 100*time.Millisecond)

	if reg.name != "test-redis" {
		t.Errorf("expected name %q, got %q", "test-redis", reg.name)
	}
	if reg.phase != int(gscore.PhasePostDB) {
		t.Errorf("expected phase %d, got %d", int(gscore.PhasePostDB), reg.phase)
	}
	if reg.timeout != 100*time.Millisecond {
		t.Errorf("expected timeout 100ms, got %v", reg.timeout)
	}
	if reg.fn == nil {
		t.Fatal("expected closer fn to be registered")
	}

	// Invoke the closer and verify client.Close() is called.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := reg.fn(ctx); err != nil {
		t.Fatalf("closer fn returned error: %v", err)
	}
	if c.called.Load() != 1 {
		t.Errorf("expected Close called once, got %d", c.called.Load())
	}
}
