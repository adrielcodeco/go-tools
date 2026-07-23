package sentryfiberv3_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/adrielcodeco/go-tools/sentryfiberv3"
)

// TestMiddleware_PassesThroughAndRecovers mirrors the sentryfiber v2 test
// for Fiber v3: pass-through on success, recovery into 500 on panic, all
// with Sentry disabled.
func TestMiddleware_PassesThroughAndRecovers(t *testing.T) {
	app := fiber.New()
	app.Use(sentryfiberv3.New())
	app.Get("/ok", func(c fiber.Ctx) error {
		sentryfiberv3.CaptureError(c, nil)
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/panic", func(c fiber.Ctx) error {
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
