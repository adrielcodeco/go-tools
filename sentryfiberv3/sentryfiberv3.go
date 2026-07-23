// Package sentryfiberv3 is the Fiber v3 adapter for the sentrycore Sentry
// error/crash reporting engine. See the sibling sentryfiber package for
// Fiber v2.
//
// It mirrors sentryfiber: a per-request Sentry hub is bound to the
// request context, enriched with the HTTP request and the active APM
// trace tags (read via c.RequestCtx(), matching apmfiberv3), panics are
// recovered and reported, and errors bubbled from handlers are captured.
//
// It never blocks the request: when Sentry is disabled (empty DSN) the
// middleware still calls c.Next() and every capture is a no-op.
//
// Pitfall: place this middleware AFTER apmfiberv3.Middleware() so the APM
// transaction already exists on c.RequestCtx() when sentrycore reads the
// trace tags.
package sentryfiberv3

import (
	"time"

	sentry "github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"

	"github.com/adrielcodeco/go-tools/sentrycore"
)

const defaultFlushTimeout = 2 * time.Second

// Config tunes the middleware. See sentryfiber.Config for field meanings.
type Config struct {
	Repanic         bool
	WaitForDelivery bool
}

// New returns the middleware with default Config.
func New() fiber.Handler { return NewWithConfig(Config{}) }

// NewWithConfig returns the middleware configured by cfg.
func NewWithConfig(cfg Config) fiber.Handler {
	return func(c fiber.Ctx) (err error) {
		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetContext("request", requestContext(c))
		// APM stores its transaction on c.RequestCtx() in Fiber v3.
		if tags := sentrycore.CaptureFields(c.RequestCtx()); len(tags) > 0 {
			hub.Scope().SetTags(tags)
		}

		ctx := sentry.SetHubOnContext(c.Context(), hub)
		c.SetContext(ctx)

		defer func() {
			if v := recover(); v != nil {
				eventID := hub.RecoverWithContext(ctx, v)
				if eventID != nil && cfg.WaitForDelivery {
					hub.Flush(defaultFlushTimeout)
				}
				if cfg.Repanic {
					panic(v)
				}
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

// CaptureError reports err against the current request's Sentry hub.
// No-op when err is nil or Sentry is disabled.
func CaptureError(c fiber.Ctx, err error) {
	if err == nil {
		return
	}
	sentrycore.CaptureException(c.Context(), err)
}

// requestContext builds the "request" context map attached to the Sentry
// scope. It avoids materializing a *http.Request (Fiber runs on fasthttp)
// and skips the body.
func requestContext(c fiber.Ctx) map[string]any {
	headers := make(map[string]string)
	c.RequestCtx().Request.Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = string(value)
	})
	return map[string]any{
		"method":       c.Method(),
		"url":          c.OriginalURL(),
		"query_string": string(c.RequestCtx().URI().QueryString()),
		"headers":      headers,
	}
}
