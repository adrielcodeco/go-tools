package gsrueidis_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/gsrueidis"
)

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
