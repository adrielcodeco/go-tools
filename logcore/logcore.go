// Package logcore is the framework-agnostic logging engine behind the
// logfiber (Fiber v2) and logfiberv3 (Fiber v3) adapters.
//
// It builds a zap logger pre-wired for APM:
//
//   - Error/Fatal log calls are auto-emitted as APM error events (via
//     apmcore.WrapZapCore on top of apmzap.Core).
//   - logcore.LogCtx(ctx) returns a logger decorated with trace.id /
//     transaction.id / span.id when ctx has an active APM transaction.
//
// The package keeps a process-global logger (logcore.Log / Logy / LogCtx)
// so handlers can call it without plumbing — and exposes logcore.New +
// SetGlobal for tests and apps that want to inject their own.
//
// It also exports HTTPClientHook(), an adapter that plugs straight into
// httpclient.SetHook(...) and produces the same "outgoing" structured-log
// schema the Fiber incoming middlewares use, so requests are searchable
// in Kibana by req/res/responseTime regardless of direction.
package logcore

import (
	"context"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// Options tunes New. All fields are optional; sensible production
// defaults are applied when zero.
type Options struct {
	// Level is the minimum log level. Default: InfoLevel.
	Level zapcore.Level

	// Encoding selects the zap encoder. "json" (default) or "console".
	Encoding string

	// DisableAPMCore turns off the apmzap.Core wrap. By default the
	// logger's core is wrapped so .Error/.Fatal calls are auto-emitted
	// as APM error events in Kibana → APM → Errors. Set to true to
	// disable (e.g. in tests where you don't want APM noise).
	DisableAPMCore bool

	// Service / Version / Environment are added as permanent fields on
	// every log line — useful as Kibana filters when many services
	// share an index.
	Service     string
	Version     string
	Environment string

	// RedactFields wraps the logger core with NewRedactCore so the values
	// of sensitive fields (Authorization, Cookie, password, token, card
	// data, …) are masked globally before encoding — for every log call,
	// not just the HTTP request/response logs. Defaults to false to stay
	// backwards-compatible; new services should enable it.
	RedactFields bool

	// Redactor overrides the policy used when RedactFields is true. Nil
	// uses DefaultRedactor (the package default key/pattern set).
	Redactor *Redactor

	// Extra is appended to the zap.New options list. Use it for hooks,
	// custom AddCallerSkip, ReplaceCore, etc.
	Extra []zap.Option
}

// Logger wraps *zap.Logger so the package can attach helpers without
// shadowing zap's API.
type Logger struct{ *zap.Logger }

// Sugar returns the sugared variant of the underlying logger.
func (l *Logger) Sugar() *zap.SugaredLogger { return l.Logger.Sugar() }

// New constructs a Logger configured per opts. The returned Logger is
// independent — call SetGlobal to make it the process-wide default.
func New(opts Options) (*Logger, error) {
	cfg := zap.NewProductionConfig()
	if opts.Encoding == "console" {
		cfg = zap.NewDevelopmentConfig()
	}
	cfg.Level = zap.NewAtomicLevelAt(opts.Level)
	if opts.Level == 0 && opts.Encoding != "console" {
		// zap.NewProductionConfig defaults to InfoLevel; keep that on zero.
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	zapOpts := opts.Extra
	if !opts.DisableAPMCore {
		zapOpts = append(zapOpts, zap.WrapCore(func(c zapcore.Core) zapcore.Core {
			return apmcore.WrapZapCore(c)
		}))
	}
	if opts.RedactFields {
		// Applied last so it is the outermost core: fields are masked
		// before reaching the APM core (so APM error events are redacted
		// too) and the encoder.
		red := opts.Redactor
		zapOpts = append(zapOpts, zap.WrapCore(func(c zapcore.Core) zapcore.Core {
			return NewRedactCore(c, red)
		}))
	}

	base, err := cfg.Build(zapOpts...)
	if err != nil {
		return nil, err
	}

	fields := []zap.Field{}
	if opts.Service != "" {
		fields = append(fields, zap.String("service.name", opts.Service))
	}
	if opts.Version != "" {
		fields = append(fields, zap.String("service.version", opts.Version))
	}
	if opts.Environment != "" {
		fields = append(fields, zap.String("service.environment", opts.Environment))
	}
	if len(fields) > 0 {
		base = base.With(fields...)
	}

	return &Logger{Logger: base}, nil
}

// LogCtx returns a child logger decorated with the APM trace fields
// pulled from ctx — useful for any handler/service log call so the
// resulting line jumps back to its trace in Kibana.
func (l *Logger) LogCtx(ctx context.Context) *zap.Logger {
	if ctx == nil {
		return l.Logger
	}
	fields := apmcore.LogCtxFields(ctx)
	if len(fields) == 0 {
		return l.Logger
	}
	return l.Logger.With(fields...)
}

// --- global helpers ---------------------------------------------------

var (
	globalPtr atomic.Pointer[Logger]
	initOnce  sync.Once
)

// SetGlobal replaces the process-wide logger. Passing nil restores the
// lazy default.
func SetGlobal(l *Logger) { globalPtr.Store(l) }

// Log returns the global logger, lazily constructing a production one
// the first time it's called. Safe for concurrent use.
func Log() *zap.Logger { return globalLogger().Logger }

// Logy returns the global sugared logger.
func Logy() *zap.SugaredLogger { return globalLogger().Sugar() }

// LogCtx returns the global logger decorated with APM trace fields.
func LogCtx(ctx context.Context) *zap.Logger { return globalLogger().LogCtx(ctx) }

func globalLogger() *Logger {
	if l := globalPtr.Load(); l != nil {
		return l
	}
	initOnce.Do(func() {
		l, err := New(Options{})
		if err != nil {
			// Should not happen on default config; fall back to bare zap.
			base, _ := zap.NewProduction()
			l = &Logger{Logger: base}
		}
		globalPtr.CompareAndSwap(nil, l)
	})
	return globalPtr.Load()
}
