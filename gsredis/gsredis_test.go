package gsredis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/gsredis"
)

// fakeRegistrar captures the arguments passed to RegisterCloser so tests can
// assert on them.
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

// newOfflineClient returns a *redis.Client configured to a non-existent
// address. Close() on such a client completes synchronously without error.
func newOfflineClient() redis.UniversalClient {
	return redis.NewClient(&redis.Options{Addr: "localhost:0"})
}

func TestRegisterRegistersCloser(t *testing.T) {
	reg := &fakeRegistrar{}
	client := newOfflineClient()

	gsredis.Register(client, reg, gscore.PhasePostDB, 100*time.Millisecond)

	if reg.name != "gsredis" {
		t.Errorf("name = %q, want %q", reg.name, "gsredis")
	}
	if reg.phase != int(gscore.PhasePostDB) {
		t.Errorf("phase = %d, want %d (PhasePostDB)", reg.phase, int(gscore.PhasePostDB))
	}
	if reg.timeout != 100*time.Millisecond {
		t.Errorf("timeout = %v, want 100ms", reg.timeout)
	}
	if reg.fn == nil {
		t.Fatal("fn must not be nil after Register")
	}

	// Calling the registered fn must invoke Close on the underlying client.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := reg.fn(ctx); err != nil {
		t.Errorf("registered fn returned unexpected error: %v", err)
	}
}

func TestRegisterDefaultTimeout(t *testing.T) {
	reg := &fakeRegistrar{}
	client := newOfflineClient()
	defer client.Close() //nolint:errcheck

	gsredis.Register(client, reg, gscore.PhasePostDB, 0) // zero → DefaultTimeout

	if reg.timeout != gsredis.DefaultTimeout {
		t.Errorf("timeout = %v, want DefaultTimeout (%v)", reg.timeout, gsredis.DefaultTimeout)
	}
}

func TestRegisterNilClientIsNoop(t *testing.T) {
	reg := &fakeRegistrar{}
	gsredis.Register(nil, reg, gscore.PhasePostDB, 0)
	if reg.name != "" {
		t.Errorf("expected no registration for nil client, but name = %q", reg.name)
	}
}

func TestRegisterNilManagerIsNoop(t *testing.T) {
	client := newOfflineClient()
	defer client.Close() //nolint:errcheck
	// Must not panic.
	gsredis.Register(client, nil, 4, 0)
}

// slowUniversalClient wraps redis.UniversalClient but delays Close.
type slowUniversalClient struct {
	redis.UniversalClient
	block time.Duration
}

func (s *slowUniversalClient) Close() error {
	time.Sleep(s.block)
	return s.UniversalClient.Close()
}

func TestRegisterHonorsTimeout(t *testing.T) {
	reg := &fakeRegistrar{}
	inner := newOfflineClient()
	client := &slowUniversalClient{UniversalClient: inner, block: 300 * time.Millisecond}

	gsredis.Register(client, reg, gscore.PhasePostDB, 50*time.Millisecond)

	if reg.fn == nil {
		t.Fatal("fn must not be nil")
	}

	// The manager passes a per-closer deadline context when invoking the fn.
	// Simulate that by passing a 50ms deadline context — matching reg.timeout.
	ctx, cancel := context.WithTimeout(context.Background(), reg.timeout)
	defer cancel()

	start := time.Now()
	err := reg.fn(ctx)
	elapsed := time.Since(start)

	if !errors.Is(err, gsredis.ErrCloseTimedOut) {
		t.Errorf("expected ErrCloseTimedOut, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected fn to return near 50ms, took %v", elapsed)
	}
}
