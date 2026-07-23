package sentrycore

import (
	"time"

	"github.com/adrielcodeco/go-tools/gscore"
)

// CloserRegistrar is the subset of gscore.Manager used by sentrycore
// helpers. It is a type alias for gscore.CloserRegistrar; *gscore.Manager
// satisfies it directly.
type CloserRegistrar = gscore.CloserRegistrar

// RegisterWithManager registers the Sentry flush ShutdownFunc as a
// PhasePostDB closer so buffered events are flushed before the process
// exits. Call it immediately after SetupSentry.
//
// A nil fn (e.g. the no-op returned when Sentry is disabled) is still
// registered but does nothing, keeping the closer list stable across
// enabled/disabled configurations.
//
// phase must be gscore.PhasePostDB (value 4). Pass 0 to use PhasePostDB.
// timeout=0 defaults to 5s.
//
//	shutdown, err := sentrycore.SetupSentry(ctx, opts)
//	sentrycore.RegisterWithManager(shutdown, mgr, gscore.PhasePostDB, 0)
func RegisterWithManager(fn ShutdownFunc, mgr CloserRegistrar, phase int, timeout time.Duration) {
	if fn == nil || mgr == nil {
		return
	}
	if phase == 0 {
		phase = 4 // PhasePostDB
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	mgr.RegisterCloser("sentrycore-flush", phase, timeout, fn)
}
