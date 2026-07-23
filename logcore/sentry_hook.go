package logcore

import (
	sentry "github.com/getsentry/sentry-go"
	"go.uber.org/zap/zapcore"

	"github.com/adrielcodeco/go-tools/sentrycore"
)

// sentryCore is a zapcore.Core decorator that forwards log entries at or
// above a threshold level (default Error) to Sentry as events. It is the
// logcore counterpart to apmcore.WrapZapCore (which forwards to Elastic
// APM): with both wrapped, an Error log produces an APM error doc AND a
// Sentry event, cross-linked by the trace tags sentrycore attaches.
//
// It is deliberately non-blocking: sentry.CaptureEvent buffers and the
// SDK's background worker delivers asynchronously, so logging never waits
// on the network. When Sentry is disabled (empty DSN) the whole core is a
// pass-through — sentrycore.Enabled() gates every capture.
type sentryCore struct {
	zapcore.Core
	minLevel zapcore.Level
}

// NewSentryCore wraps c so entries at level >= minLevel are also sent to
// Sentry. A zero minLevel is treated as ErrorLevel. Plug it in via
// zap.WrapCore, or let Options.SentryHook do it for you.
func NewSentryCore(c zapcore.Core, minLevel zapcore.Level) zapcore.Core {
	if minLevel == 0 {
		minLevel = zapcore.ErrorLevel
	}
	return &sentryCore{Core: c, minLevel: minLevel}
}

func (c *sentryCore) With(fields []zapcore.Field) zapcore.Core {
	return &sentryCore{Core: c.Core.With(fields), minLevel: c.minLevel}
}

func (c *sentryCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	// Register this core so Write runs for entries at/above minLevel,
	// while still delegating the underlying enablement to the wrapped core.
	if c.Core.Enabled(ent.Level) {
		ce = ce.AddCore(ent, c)
	}
	return ce
}

func (c *sentryCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Forward first so the log line is emitted even if Sentry capture
	// panics for any reason; the underlying core write is the source of
	// truth for the log pipeline.
	err := c.Core.Write(ent, fields)

	if sentrycore.Enabled() && ent.Level >= c.minLevel {
		c.capture(ent, fields)
	}
	return err
}

// capture builds a Sentry event from the zap entry and its fields. The
// fields reaching here are already redacted when Options.RedactFields is
// set, because NewSentryCore is wrapped INSIDE the redact core.
func (c *sentryCore) capture(ent zapcore.Entry, fields []zapcore.Field) {
	event := sentry.NewEvent()
	event.Level = zapToSentryLevel(ent.Level)
	event.Message = ent.Message
	event.Logger = ent.LoggerName

	// Encode the structured fields into a flat map for the "log" context.
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	if len(enc.Fields) > 0 {
		event.Contexts["log"] = sentry.Context(enc.Fields)
		// Promote the trace correlation fields to tags so a Sentry event
		// filters alongside the same identifiers apmcore logs.
		for _, key := range []string{"trace.id", "transaction.id", "span.id"} {
			if v, ok := enc.Fields[key].(string); ok && v != "" {
				if event.Tags == nil {
					event.Tags = map[string]string{}
				}
				event.Tags[sentryTagKey(key)] = v
			}
		}
	}

	// If a field carries an error value, attach it as the exception so
	// the Sentry issue groups by error type rather than by message.
	if e := errorFromFields(fields); e != nil {
		sentry.CaptureException(e)
		return
	}
	sentry.CaptureEvent(event)
}

func zapToSentryLevel(l zapcore.Level) sentry.Level {
	switch {
	case l >= zapcore.FatalLevel:
		return sentry.LevelFatal
	case l >= zapcore.ErrorLevel:
		return sentry.LevelError
	case l >= zapcore.WarnLevel:
		return sentry.LevelWarning
	case l >= zapcore.InfoLevel:
		return sentry.LevelInfo
	default:
		return sentry.LevelDebug
	}
}

func sentryTagKey(zapKey string) string {
	switch zapKey {
	case "trace.id":
		return "trace_id"
	case "transaction.id":
		return "transaction_id"
	case "span.id":
		return "span_id"
	default:
		return zapKey
	}
}

// errorFromFields returns the first error carried by a zap.Error field
// (key "error"), or nil. zap stores it as a field with an error Interface.
func errorFromFields(fields []zapcore.Field) error {
	for _, f := range fields {
		if f.Key == "error" && f.Type == zapcore.ErrorType {
			if e, ok := f.Interface.(error); ok {
				return e
			}
		}
	}
	return nil
}
