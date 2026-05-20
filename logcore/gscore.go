package logcore

import (
	"context"
	"time"
)

// CloserRegistrar is the subset of gscore.Manager used by logcore helpers.
// *gscore.Manager satisfies this interface directly.
type CloserRegistrar interface {
	RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error)
}

// RegisterWithManager registers logger.Sync() as a closer on mgr so the zap
// OS write buffer is flushed before the process exits. Must be called after
// all other closers so log lines from shutdown itself are not lost.
//
// phase must be gscore.PhasePostDB (value 4). Pass 0 to use PhasePostDB.
// timeout=0 defaults to 5s.
func (l *Logger) RegisterWithManager(mgr CloserRegistrar, phase int, timeout time.Duration) {
	if mgr == nil {
		return
	}
	if phase == 0 {
		phase = 4 // PhasePostDB
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	mgr.RegisterCloser("logcore-sync", phase, timeout, func(_ context.Context) error {
		return l.Logger.Sync()
	})
}

// RegisterGlobalWithManager is RegisterWithManager applied to the global logger.
func RegisterGlobalWithManager(mgr CloserRegistrar, phase int, timeout time.Duration) {
	globalLogger().RegisterWithManager(mgr, phase, timeout)
}
