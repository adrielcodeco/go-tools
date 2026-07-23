package sentryfiber

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"github.com/adrielcodeco/go-tools/logcore"
)

// TestRequestContext_RedactsSecrets verifies that requestContext masks
// sensitive headers and query params using the shared logcore redactor,
// so credentials never reach the Sentry event.
func TestRequestContext_RedactsSecrets(t *testing.T) {
	app := fiber.New()
	defer app.ReleaseCtx(newCtx(app, func(c *fiber.Ctx) {
		c.Request().SetRequestURI("/pay?access_token=SECRET&page=2")
		c.Request().Header.Set("Authorization", "Bearer super-secret")
		c.Request().Header.Set("X-Origin", "test")

		ctxMap := requestContext(c, logcore.DefaultRedactor())

		headers, _ := ctxMap["headers"].(map[string]string)
		if got := headers["Authorization"]; strings.Contains(got, "super-secret") {
			t.Errorf("Authorization leaked: %q", got)
		}
		if headers["X-Origin"] != "test" {
			t.Errorf("non-sensitive header altered: %q", headers["X-Origin"])
		}

		qs, _ := ctxMap["query_string"].(string)
		if strings.Contains(qs, "SECRET") {
			t.Errorf("access_token leaked in query_string: %q", qs)
		}
		if !strings.Contains(qs, "page=2") {
			t.Errorf("non-sensitive query param dropped: %q", qs)
		}
	}))
}

// TestRequestContext_CustomRedactor verifies that a custom redactor passed
// to the middleware masks a header that the default policy would let
// through — proving Config.Redactor actually reaches the request context.
func TestRequestContext_CustomRedactor(t *testing.T) {
	// "x-tenant-marker" is not caught by the default keys or patterns.
	custom := logcore.NewRedactor(logcore.RedactorOptions{
		Extra: []string{"x-tenant-marker"},
	})
	app := fiber.New()
	defer app.ReleaseCtx(newCtx(app, func(c *fiber.Ctx) {
		c.Request().Header.Set("X-Tenant-Marker", "leak-me")

		def := requestContext(c, logcore.DefaultRedactor())["headers"].(map[string]string)
		if def["X-Tenant-Marker"] != "leak-me" {
			t.Fatalf("precondition: default redactor unexpectedly masked X-Tenant-Marker")
		}

		got := requestContext(c, custom)["headers"].(map[string]string)
		if strings.Contains(got["X-Tenant-Marker"], "leak-me") {
			t.Errorf("custom redactor did not mask X-Tenant-Marker: %q", got["X-Tenant-Marker"])
		}
	}))
}

// newCtx acquires a Fiber context, runs fn against it, and returns it for
// release. It gives the internal test a real *fiber.Ctx backed by a
// fasthttp request context.
func newCtx(app *fiber.App, fn func(*fiber.Ctx)) *fiber.Ctx {
	fctx := &fasthttp.RequestCtx{}
	c := app.AcquireCtx(fctx)
	fn(c)
	return c
}
