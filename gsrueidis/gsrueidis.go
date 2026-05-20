// Package gsrueidis is the Rueidis adapter for the gscore graceful-shutdown
// engine.
//
// rueidis.Client.Close() is a synchronous, ctx-unaware call that BLOCKS
// until all pending pipelined commands and PubSub subscribers finish.
// In a misbehaving setup (stuck subscriber, broken connection that
// hasn't observed TCP close yet) it can hang indefinitely — there is no
// rueidis equivalent of *sql.DB.Close() with a context. This adapter
// runs Close() in a goroutine bounded by the closer's timeout so the
// shutdown sequence cannot stall on a wedged Redis client.
//
// Recommended placement is PhasePostDB: any txctx.OnCommit callback that
// publishes to Redis after a DB commit must have completed by then, so
// it's safe to tear the client down.
package gsrueidis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/rueidis"

	"github.com/adrielcodeco/go-tools/gscore"
)

// CloserRegistrar is the subset of gscore.Manager used by gsrueidis helpers.
// It is a type alias for gscore.CloserRegistrar; *gscore.Manager satisfies it directly.
type CloserRegistrar = gscore.CloserRegistrar

// Closer is the subset of rueidis.Client that gsrueidis needs.
// Provided as an interface so tests (and any custom wrappers) can
// supply a fake without spinning up a real Redis.
type Closer interface {
	Close()
}

// DefaultTimeout is used by Register when timeout=0 is passed.
const DefaultTimeout = 5 * time.Second

// ErrCloseTimedOut is returned from the registered closer if Close()
// did not return within the per-closer timeout. The manager logs it but
// continues with the rest of the shutdown sequence.
var ErrCloseTimedOut = errors.New("gsrueidis: client.Close() timed out")

// Register attaches client to mgr so that during phase the manager will
// call client.Close() bounded by timeout. timeout=0 falls back to
// DefaultTimeout (not Config.HookTimeout — Redis hangs deserve a
// dedicated default).
//
// name is used in log lines; pass something distinguishable when you
// have multiple Redis clients (e.g. "redis-cache", "redis-rate-limit").
// Empty name → auto-filled by gscore.
//
// Returns nothing — registration is idempotent only at the gscore level.
// Calling Register twice with the same client will close it twice; the
// second call is a no-op inside rueidis but emits a misleading error.
func Register(mgr CloserRegistrar, name string, client Closer, phase gscore.Phase, timeout time.Duration) {
	if client == nil {
		return
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	mgr.RegisterCloser(name, int(phase), timeout, makeCloseFn(client))
}

// makeCloseFn wraps client.Close() into a ctx-aware closer. Close runs
// in a goroutine; the function returns either when Close completes or
// when ctx expires. If ctx expires first the goroutine is leaked — by
// design, since calling Close again or aborting it is not supported by
// rueidis. The closer's per-call timeout exists precisely to bound how
// long the *shutdown sequence* waits, not the Close call itself.
func makeCloseFn(client Closer) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		done := make(chan struct{})
		go func() {
			client.Close()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ErrCloseTimedOut
		}
	}
}

// RegisterRueidis is a typed convenience wrapper for callers who don't
// want to type-assert to the Closer interface. Functionally identical
// to Register.
func RegisterRueidis(mgr CloserRegistrar, name string, client rueidis.Client, phase gscore.Phase, timeout time.Duration) {
	Register(mgr, name, client, phase, timeout)
}
