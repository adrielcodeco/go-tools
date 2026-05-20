// Package gsredis is the go-redis UniversalClient adapter for the gscore
// graceful-shutdown engine.
//
// redis.UniversalClient.Close() releases the underlying connection pool.
// This adapter registers Close() as a ctx-aware closer on any
// gscore-compatible manager, bounding the call with a per-closer timeout so
// the shutdown sequence cannot stall on a slow client.
//
// Recommended placement is PhasePostDB (value 4): any txctx.OnCommit callback
// that writes to Redis after a DB commit must have completed by then, so it is
// safe to tear the client down.
package gsredis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/adrielcodeco/go-tools/apmcore"
	"github.com/adrielcodeco/go-tools/gscore"
)

// CloserRegistrar is the subset of gscore.Manager used by gsredis helpers.
// It is a type alias for gscore.CloserRegistrar; *gscore.Manager satisfies it directly.
type CloserRegistrar = gscore.CloserRegistrar

// DefaultTimeout is used by Register when timeout=0 is passed.
const DefaultTimeout = 5 * time.Second

// ErrCloseTimedOut is returned from the registered closer if Close()
// did not return within the per-closer timeout. The manager logs it but
// continues with the rest of the shutdown sequence.
var ErrCloseTimedOut = errors.New("gsredis: client.Close() timed out")

// Register attaches client to mgr so that during phase the manager will call
// client.Close() bounded by timeout. timeout=0 falls back to DefaultTimeout.
//
// name is used in log lines; pass something distinguishable when you have
// multiple Redis clients (e.g. "redis-cache", "redis-rate-limit").
// Empty name is auto-filled by the manager.
//
// Calling Register twice with the same client will close it twice; go-redis
// returns an error on the second call, which the manager logs.
func Register(client redis.UniversalClient, mgr CloserRegistrar, phase gscore.Phase, timeout time.Duration) {
	if client == nil || mgr == nil {
		return
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	mgr.RegisterCloser("gsredis", int(phase), timeout, makeCloseFn(client))
}

// InstrumentAndRegister instruments client with OpenTelemetry tracing and
// metrics via apmcore, then registers its Close for graceful shutdown.
// Use this instead of Register when apmcore.SetupOTelSDK has been called.
//
// Returns the error from apmcore.InstrumentRedis if instrumentation fails;
// the caller may choose to ignore it since tracing is best-effort.
func InstrumentAndRegister(client redis.UniversalClient, mgr CloserRegistrar, phase gscore.Phase, timeout time.Duration) error {
	if err := apmcore.InstrumentRedis(client); err != nil {
		return err
	}
	Register(client, mgr, phase, timeout)
	return nil
}

// makeCloseFn wraps client.Close() into a ctx-aware closer. Close runs in a
// goroutine; the function returns when Close completes or when ctx expires.
// If ctx expires first, the goroutine is leaked — by design, since there is no
// way to abort a blocking Close call in go-redis. The per-call timeout exists
// to bound how long the shutdown sequence waits, not the Close call itself.
func makeCloseFn(client interface{ Close() error }) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() {
			done <- client.Close()
		}()
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ErrCloseTimedOut
		}
	}
}
