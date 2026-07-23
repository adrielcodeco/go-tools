// Sentry scenarios — examples of wiring the sentrycore + sentryfiber
// packages into a Fiber v2 application for error/crash reporting. These
// are documentation-only snippets (compile-checked by the examples
// module) — no executable main here.
//
// Scope reminder: Sentry here does ERROR reporting only. Tracing and
// metrics stay with apmcore/apmfiber. A captured Sentry error carries the
// active APM trace_id/transaction_id as tags so you can jump from a Sentry
// issue to the matching Kibana trace.
package examples

import (
	"context"
	"net/http"

	"github.com/gofiber/fiber/v2"

	"github.com/adrielcodeco/go-tools/apmcore"
	"github.com/adrielcodeco/go-tools/apmfiber"
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/sentrycore"
	"github.com/adrielcodeco/go-tools/sentryfiber"
	setuppkg "github.com/adrielcodeco/go-tools/setup"
)

// sentryScenarioBootstrap wires Sentry at process start. It runs AFTER
// apmcore.SetupOTelSDK so the APM transaction (and its trace tags) exists
// when Sentry captures an error.
//
// DSN/environment/release are read from SENTRY_DSN / SENTRY_ENVIRONMENT /
// SENTRY_RELEASE when the Options fields are empty. An empty DSN leaves
// Sentry disabled: SetupSentry returns a no-op shutdown and every capture
// is a no-op — the app runs unchanged.
func sentryScenarioBootstrap() (sentrycore.ShutdownFunc, error) {
	shutdown, err := sentrycore.SetupSentry(context.Background(), sentrycore.Options{
		Tags: map[string]string{"service.name": "payments-api"},
	})
	if err != nil {
		return nil, err
	}

	// Compose on top of the APM transport: APM spans + traceparent first,
	// then Sentry breadcrumbs for outgoing requests.
	http.DefaultTransport = sentrycore.WrapHTTPTransport(
		apmcore.WrapHTTPTransport(http.DefaultTransport),
	)
	http.DefaultClient.Transport = http.DefaultTransport

	return shutdown, nil
}

// sentryScenarioFiberApp shows the middleware order: apmfiber.Middleware
// MUST be first (it starts the APM transaction); sentryfiber goes right
// after so its per-request hub picks up the trace tags.
func sentryScenarioFiberApp() *fiber.App {
	app := fiber.New()
	app.Use(apmfiber.Middleware())
	app.Use(sentryfiber.New()) // default redactor; see custom variant below
	app.Post("/charge", sentryExampleHandler)
	return app
}

// sentryExampleHandler illustrates inline error capture. Like
// apmfiber.CaptureError, sentryfiber.CaptureError reports errors that the
// handler maps directly (never reaching Fiber's central ErrorHandler).
// The report carries the request's trace tags automatically.
func sentryExampleHandler(c *fiber.Ctx) error {
	if _, err := doDomainWork(c.Context()); err != nil {
		sentryfiber.CaptureError(c, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.SendStatus(fiber.StatusOK)
}

// sentryScenarioViaSetup is the recommended path: one Builder call wires
// APM, Sentry, and the logger (with the Error+ → Sentry hook) in the
// correct order and returns the flush func + the effective redactor.
func sentryScenarioViaSetup(mgr *gscore.Manager, app *fiber.App) (*setuppkg.Result, error) {
	ctx := context.Background()
	res, err := setuppkg.New().
		WithOTel(ctx).
		WithSentry(ctx, sentrycore.Options{
			Tags: map[string]string{"service.name": "payments-api"},
		}).
		WithLogger(logcore.Options{
			Service:      "payments-api",
			SentryHook:   true, // Error+ logs become Sentry events
			RedactFields: true, // mask secrets in logs AND (shared) in Sentry
		}).
		WithFiberV2(app).
		Build(mgr)
	if err != nil {
		return nil, err
	}

	// Middleware still added by the caller. Pass res.Redactor so the Sentry
	// middleware masks request headers/query with the SAME policy as the
	// logs (see the redactor-sharing scenarios below).
	app.Use(apmfiber.Middleware())
	app.Use(sentryfiber.NewWithConfig(sentryfiber.Config{Redactor: res.Redactor}))

	return res, nil
}

// --- Redactor sharing: what reflects in Sentry and what does NOT ---------
//
// Header/query redaction (Fiber middleware) and log-field redaction use the
// SAME type (logcore.Redactor) but are SEPARATE instances unless connected.
// The three scenarios below map directly to the README table.

// sentryRedactSharedGlobals — extending the GLOBAL defaults reflects
// everywhere automatically (logs, log middleware, Sentry middleware),
// because logcore.DefaultRedactor() reads these slices. ✅ reflects.
func sentryRedactSharedGlobals() {
	logcore.DefaultSensitiveKeys = append(logcore.DefaultSensitiveKeys, "x-tenant-marker")
	logcore.DefaultSensitivePatterns = append(logcore.DefaultSensitivePatterns, "passphrase")

	// Both of these now mask x-tenant-marker / *passphrase* without any
	// further wiring:
	_, _ = logcore.New(logcore.Options{RedactFields: true})
	_ = sentryfiber.New()
}

// sentryRedactSharedCustomInstance — a CUSTOM redactor reflects in Sentry
// ONLY when the SAME instance is handed to the middleware. ✅ reflects.
func sentryRedactSharedCustomInstance() *fiber.App {
	red := logcore.NewRedactor(logcore.RedactorOptions{
		Extra:    []string{"x-tenant-marker"},
		Patterns: append(logcore.DefaultSensitivePatterns, "passphrase"),
	})

	app := fiber.New()
	app.Use(apmfiber.Middleware())
	// Same `red` given to both the logger and the middleware.
	_, _ = logcore.New(logcore.Options{RedactFields: true, Redactor: red})
	app.Use(sentryfiber.NewWithConfig(sentryfiber.Config{Redactor: red}))
	return app
}

// sentryRedactNotSharedPitfall — a custom redactor given ONLY to the
// logger does NOT reach the Sentry middleware, which falls back to the
// default. ❌ does NOT reflect. This is the mistake to avoid.
func sentryRedactNotSharedPitfall() *fiber.App {
	red := logcore.NewRedactor(logcore.RedactorOptions{Extra: []string{"x-tenant-marker"}})

	app := fiber.New()
	app.Use(apmfiber.Middleware())
	_, _ = logcore.New(logcore.Options{RedactFields: true, Redactor: red})
	// BUG: no Redactor passed → middleware uses DefaultRedactor(), so
	// x-tenant-marker is masked in logs but LEAKS into the Sentry event.
	app.Use(sentryfiber.New())
	return app
}
