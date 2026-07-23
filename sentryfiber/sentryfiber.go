// Package sentryfiber is the Fiber v2 adapter for the sentrycore Sentry
// error/crash reporting engine. See the sibling sentryfiberv3 package for
// Fiber v3.
//
// The middleware:
//
//  1. Binds a per-request Sentry hub (a clone of the current hub) onto the
//     request context so concurrent requests don't share scope.
//  2. Enriches the scope with the HTTP request and the active APM trace
//     tags (trace_id / transaction_id) via sentrycore, so a captured error
//     links back to its Kibana trace.
//  3. Recovers panics, reports them to Sentry, and (by default) responds
//     500 instead of crashing the app.
//  4. Captures errors bubbled from handlers to Fiber's ErrorHandler.
//
// It never blocks the request: when Sentry is disabled (empty DSN) the
// middleware still calls c.Next() and every capture is a no-op.
//
// Pitfall: place this middleware AFTER apmfiber.Middleware() so the APM
// transaction already exists on c.Context() when sentrycore reads the
// trace tags.
package sentryfiber

import (
	"github.com/gofiber/fiber/v2"

	sentry "github.com/getsentry/sentry-go"

	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/sentrycore"
)

// Config tunes the middleware.
type Config struct {
	// Repanic re-panics after reporting so an upstream recover middleware
	// (or the Fiber default) can handle it. Default false: the panic is
	// swallowed and a 500 is returned.
	Repanic bool

	// WaitForDelivery blocks the response until the event is delivered.
	// Default false (events are flushed asynchronously / at shutdown).
	WaitForDelivery bool

	// Redactor masks sensitive request headers and query params before they
	// are attached to the Sentry event. Nil uses logcore.DefaultRedactor()
	// so the Sentry payload shares the exact policy applied to the logs.
	//
	// Pass the SAME *logcore.Redactor you gave logcore.Options.Redactor so a
	// custom denylist reflects in both places. setup.Builder does this
	// automatically when WithSentry and WithLogger are both configured.
	Redactor *logcore.Redactor
}

// New returns the middleware with default Config.
func New() fiber.Handler { return NewWithConfig(Config{}) }

// NewWithConfig returns the middleware configured by cfg.
func NewWithConfig(cfg Config) fiber.Handler {
	red := cfg.Redactor
	if red == nil {
		red = logcore.DefaultRedactor()
	}
	return func(c *fiber.Ctx) (err error) {
		// Clone the current hub for this request so scope mutations don't
		// leak across concurrent requests.
		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetContext("request", requestContext(c, red))
		if tags := sentrycore.CaptureFields(c.Context()); len(tags) > 0 {
			hub.Scope().SetTags(tags)
		}

		// Bind the hub onto the user context so handlers and sentrycore
		// capture helpers find it.
		ctx := sentry.SetHubOnContext(c.UserContext(), hub)
		c.SetUserContext(ctx)

		defer func() {
			if v := recover(); v != nil {
				eventID := hub.RecoverWithContext(ctx, v)
				if eventID != nil && cfg.WaitForDelivery {
					hub.Flush(defaultFlushTimeout)
				}
				if cfg.Repanic {
					panic(v)
				}
				// Swallow: respond 500 without crashing the app.
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}()

		err = c.Next()
		if err != nil {
			hub.CaptureException(err)
			if cfg.WaitForDelivery {
				hub.Flush(defaultFlushTimeout)
			}
		}
		return err
	}
}

// CaptureError reports err against the current request's Sentry hub. Use
// it from handlers that map errors inline (without returning them to
// Fiber's ErrorHandler). No-op when err is nil or Sentry is disabled.
func CaptureError(c *fiber.Ctx, err error) {
	if err == nil {
		return
	}
	sentrycore.CaptureException(c.UserContext(), err)
}
