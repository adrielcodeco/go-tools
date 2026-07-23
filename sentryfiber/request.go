package sentryfiber

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/adrielcodeco/go-tools/sentrycore"
)

// defaultFlushTimeout bounds a synchronous per-request flush when
// Config.WaitForDelivery is set.
const defaultFlushTimeout = 2 * time.Second

// requestContext builds the "request" context map attached to the Sentry
// scope. It avoids materializing a *http.Request (Fiber runs on fasthttp)
// and skips the body, which may be large or already consumed. Sensitive
// header values and query parameters are redacted via sentrycore before
// leaving the process.
func requestContext(c *fiber.Ctx) map[string]any {
	headers := make(map[string]string)
	c.Request().Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = sentrycore.RedactHeader(string(key), string(value))
	})
	return map[string]any{
		"method":       c.Method(),
		"url":          c.OriginalURL(),
		"query_string": sentrycore.RedactQueryString(string(c.Request().URI().QueryString())),
		"headers":      headers,
	}
}
