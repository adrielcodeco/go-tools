package sentryfiber

import (
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/adrielcodeco/go-tools/logcore"
)

// defaultFlushTimeout bounds a synchronous per-request flush when
// Config.WaitForDelivery is set.
const defaultFlushTimeout = 2 * time.Second

// requestContext builds the "request" context map attached to the Sentry
// scope. It avoids materializing a *http.Request (Fiber runs on fasthttp)
// and skips the body, which may be large or already consumed. Sensitive
// header values and query parameters are masked via red before leaving the
// process.
func requestContext(c *fiber.Ctx, red *logcore.Redactor) map[string]any {
	headers := make(map[string]string)
	c.Request().Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = string(value)
	})
	return map[string]any{
		"method":       c.Method(),
		"url":          c.OriginalURL(),
		"query_string": redactQueryString(red, string(c.Request().URI().QueryString())),
		"headers":      red.Value(headers),
	}
}

// redactQueryString parses raw and masks the values of sensitive query
// parameters using red, preserving parameter names. An unparseable query is
// dropped (returns "") rather than risk leaking it.
func redactQueryString(red *logcore.Redactor, raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	// Route each param through the redactor by key so the same policy that
	// masks headers/log fields applies to query values too.
	for k, vs := range values {
		masked, ok := red.Value(map[string]string{k: firstOrEmpty(vs)}).(map[string]string)
		if ok && masked[k] != firstOrEmpty(vs) {
			for i := range vs {
				vs[i] = masked[k]
			}
		}
	}
	return values.Encode()
}

func firstOrEmpty(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}
