package logcore

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// gscoreLoggerIface is a local mirror of gscore.Logger. Any value that
// satisfies this interface also satisfies the real gscore.Logger at the call
// site via Go structural typing — no import of gscore is required here.
type gscoreLoggerIface interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// gscoreLogger wraps a *zap.SugaredLogger and satisfies gscoreLoggerIface
// (and therefore gscore.Logger).
type gscoreLogger struct {
	sugar *zap.SugaredLogger
}

func (g *gscoreLogger) Info(msg string, kv ...any) {
	g.sugar.Infow(msg, kv...)
}

func (g *gscoreLogger) Warn(msg string, kv ...any) {
	g.sugar.Warnw(msg, kv...)
}

func (g *gscoreLogger) Error(msg string, kv ...any) {
	g.sugar.Errorw(msg, kv...)
}

// GSCoreLogger returns a gscore.Logger-compatible value backed by l. If l is
// nil the global logger is used. The returned value satisfies the real
// gscore.Logger interface via structural typing — no import of gscore is
// needed inside logcore.
func GSCoreLogger(l *Logger) gscoreLoggerIface {
	if l == nil {
		l = globalLogger()
	}
	return &gscoreLogger{sugar: l.Logger.Sugar()}
}

// GSCoreGlobalLogger is a convenience wrapper that returns GSCoreLogger(nil),
// i.e. a gscore.Logger backed by the global logger.
func GSCoreGlobalLogger() gscoreLoggerIface {
	return GSCoreLogger(nil)
}

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
