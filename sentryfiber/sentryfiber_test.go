package sentryfiber_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/adrielcodeco/go-tools/sentryfiber"
)

// TestMiddleware_PassesThroughAndRecovers verifies the middleware calls
// the handler chain, returns a 200 on success, and recovers a panicking
// handler into a 500 instead of crashing — all with Sentry disabled
// (empty DSN), i.e. it never interferes with the running app.
func TestMiddleware_PassesThroughAndRecovers(t *testing.T) {
	app := fiber.New()
	app.Use(sentryfiber.New())
	app.Get("/ok", func(c *fiber.Ctx) error {
		sentryfiber.CaptureError(c, nil) // no-op path
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/panic", func(c *fiber.Ctx) error {
		panic("boom")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/ok", nil))
	if err != nil {
		t.Fatalf("Test /ok: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("/ok status = %d, want 200", resp.StatusCode)
	}

	resp, err = app.Test(httptest.NewRequest("GET", "/panic", nil))
	if err != nil {
		t.Fatalf("Test /panic: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("/panic status = %d, want 500", resp.StatusCode)
	}
}
