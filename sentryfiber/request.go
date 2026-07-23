package sentryfiber

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// defaultFlushTimeout bounds a synchronous per-request flush when
// Config.WaitForDelivery is set.
const defaultFlushTimeout = 2 * time.Second

// requestContext builds the "request" context map attached to the Sentry
// scope. It avoids materializing a *http.Request (Fiber runs on fasthttp)
// and skips the body, which may be large or already consumed.
func requestContext(c *fiber.Ctx) map[string]any {
	headers := make(map[string]string)
	c.Request().Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = string(value)
	})
	return map[string]any{
		"method":       c.Method(),
		"url":          c.OriginalURL(),
		"query_string": string(c.Request().URI().QueryString()),
		"headers":      headers,
	}
}
