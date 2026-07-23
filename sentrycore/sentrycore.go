// Package sentrycore is the framework-agnostic Sentry error/crash reporting
// engine behind the sentryfiber (Fiber v2) and sentryfiberv3 (Fiber v3)
// adapters and the logcore zap hook.
//
// Scope: Sentry here does ERROR and CRASH reporting only. Tracing and
// metrics remain the responsibility of the sibling apmcore package
// (Elastic APM + OpenTelemetry). To keep a captured error linkable back
// to its trace in Kibana, CaptureFields/CaptureException attach the
// active APM trace.id / transaction.id as Sentry tags.
//
// It exposes:
//
//   - SetupSentry: initialize the Sentry SDK from Options. An empty DSN
//     yields a silent no-op — every capture becomes a cheap no-op and the
//     application runs unchanged. This is the "does not interfere with the
//     running app" guarantee.
//   - CaptureException / CaptureFields: report an error (optionally with
//     the active APM trace tags) through the current or a fresh hub.
//   - WrapHTTPTransport: wrap an http.RoundTripper so outgoing requests
//     are recorded as Sentry breadcrumbs, giving errors HTTP context.
//   - RegisterWithManager: register the flushing ShutdownFunc as a gscore
//     closer so buffered events are flushed before the process exits.
//
// The package is intentionally Fiber-agnostic. HTTP middleware lives in
// the sentryfiber/sentryfiberv3 adapter packages.
package sentrycore

import (
	"context"
	"errors"
	"os"
	"time"

	sentry "github.com/getsentry/sentry-go"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// ShutdownFunc flushes buffered Sentry events. It is safe to call
// multiple times; subsequent calls are no-ops. It mirrors
// apmcore.ShutdownFunc so both can be registered through the same
// gscore closer machinery.
type ShutdownFunc func(ctx context.Context) error

// Options tunes SetupSentry. All fields are optional; when a field is
// zero the Sentry SDK falls back to its own environment-variable lookup
// (SENTRY_DSN, SENTRY_ENVIRONMENT, SENTRY_RELEASE), matching the
// convention used by the Elastic APM agent in apmcore.
type Options struct {
	// DSN is the Sentry project DSN. Empty (and no SENTRY_DSN in the
	// environment) disables Sentry entirely: SetupSentry returns a no-op
	// ShutdownFunc and every capture is a no-op. Nothing about the
	// running application changes.
	DSN string

	// Environment tags events (e.g. "production", "staging"). Empty falls
	// back to SENTRY_ENVIRONMENT.
	Environment string

	// Release identifies the deployed build (e.g. a version or git sha).
	// Empty falls back to SENTRY_RELEASE.
	Release string

	// ServerName overrides the reported server/host name. Empty lets the
	// SDK resolve it.
	ServerName string

	// SampleRate is the error-event sample rate in [0,1]. Zero is treated
	// as 1.0 (report every error) so a zero-value Options stays useful.
	SampleRate float64

	// Debug turns on the Sentry SDK debug logging.
	Debug bool

	// Tags are attached to every event as global tags. Useful for
	// service.name / service.version so events are filterable alongside
	// the logcore log fields.
	Tags map[string]string

	// FlushTimeout bounds how long the ShutdownFunc waits for buffered
	// events to be sent. Zero defaults to 2s.
	FlushTimeout time.Duration

	// Extra is merged onto the sentry.ClientOptions built from the fields
	// above, letting callers set anything the struct does not expose
	// (BeforeSend, integrations, transport, …). It runs last and wins.
	Extra func(*sentry.ClientOptions)
}

// noop is the ShutdownFunc returned when Sentry is disabled.
func noop(context.Context) error { return nil }

// enabled reports whether the SDK was initialized with a usable DSN.
var enabled bool

// Enabled reports whether SetupSentry initialized a live Sentry client.
// When false, all capture helpers are no-ops.
func Enabled() bool { return enabled }

// SetupSentry initializes the global Sentry client from opts.
//
// If opts.DSN is empty and no SENTRY_DSN is set in the environment,
// Sentry stays disabled: this returns a no-op ShutdownFunc and nil error,
// and Enabled() reports false. This is deliberate so a service can ship
// the wiring unconditionally and turn Sentry on purely via configuration.
//
// The returned ShutdownFunc calls sentry.Flush; register it with
// RegisterWithManager so events are flushed during graceful shutdown.
func SetupSentry(_ context.Context, opts Options) (ShutdownFunc, error) {
	// Resolve the effective DSN the way sentry.Init would: explicit field
	// first, then SENTRY_DSN. Doing it here lets us short-circuit to a
	// no-op (and report Enabled()==false) when no DSN is configured.
	dsn := opts.DSN
	if dsn == "" {
		dsn = os.Getenv("SENTRY_DSN")
	}
	if dsn == "" {
		enabled = false
		return noop, nil
	}

	clientOpts := sentry.ClientOptions{
		Dsn:         dsn,
		Environment: opts.Environment,
		Release:     opts.Release,
		ServerName:  opts.ServerName,
		SampleRate:  opts.SampleRate,
		Debug:       opts.Debug,
	}
	if clientOpts.SampleRate == 0 {
		clientOpts.SampleRate = 1.0
	}
	if opts.Extra != nil {
		opts.Extra(&clientOpts)
	}

	if err := sentry.Init(clientOpts); err != nil {
		return nil, err
	}
	enabled = true

	if len(opts.Tags) > 0 {
		tags := opts.Tags
		sentry.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetTags(tags)
		})
	}

	timeout := opts.FlushTimeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	var once bool
	shutdown := func(context.Context) error {
		if once {
			return nil
		}
		once = true
		if !sentry.Flush(timeout) {
			return errors.New("sentrycore: flush timed out")
		}
		return nil
	}
	return shutdown, nil
}

// CaptureException reports err through the hub bound to ctx (or the
// current hub when ctx carries none), attaching the active APM trace
// tags so the event links back to its Kibana trace. It is a no-op when
// err is nil or Sentry is disabled.
func CaptureException(ctx context.Context, err error) {
	if err == nil || !enabled {
		return
	}
	hub := hubFromContext(ctx)
	if fields := traceTags(ctx); len(fields) > 0 {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetTags(fields)
			hub.CaptureException(err)
		})
		return
	}
	hub.CaptureException(err)
}

// CaptureFields returns the APM trace correlation tags (trace_id,
// transaction_id) for ctx, suitable for attaching to a Sentry scope. It
// is exported so the Fiber adapters can enrich their per-request scope
// without re-deriving the mapping.
func CaptureFields(ctx context.Context) map[string]string { return traceTags(ctx) }

// hubFromContext returns the Sentry hub bound to ctx, or the current hub.
func hubFromContext(ctx context.Context) *sentry.Hub {
	if ctx != nil {
		if h := sentry.GetHubFromContext(ctx); h != nil {
			return h
		}
	}
	return sentry.CurrentHub()
}

// traceTags maps the active APM trace fields onto Sentry tag keys. It
// reuses apmcore.LogCtxFields (the same source used for log↔trace
// correlation) so a Sentry error and its logs share identifiers.
func traceTags(ctx context.Context) map[string]string {
	fields := apmcore.LogCtxFields(ctx)
	if len(fields) == 0 {
		return nil
	}
	tags := make(map[string]string, len(fields))
	for _, f := range fields {
		// apmzap emits string fields keyed "trace.id" / "transaction.id" /
		// "span.id"; normalize to Sentry-friendly snake_case tag keys.
		switch f.Key {
		case "trace.id":
			tags["trace_id"] = f.String
		case "transaction.id":
			tags["transaction_id"] = f.String
		case "span.id":
			tags["span_id"] = f.String
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}
