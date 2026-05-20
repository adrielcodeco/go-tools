// Package logfiberv3 is the Fiber v3 adapter for the logcore structured
// logger. See the sibling logfiber package for Fiber v2.
package logfiberv3

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"

	"github.com/adrielcodeco/go-tools/logcore"
)

// Config tunes the middleware. All fields are optional.
type Config struct {
	// SkipPaths lists request paths that bypass logging entirely.
	// Default: ["/live", "/ready", "/health"].
	SkipPaths []string

	// SkipFunc, when non-nil, is called for every request that was not
	// already skipped by SkipPaths. Returning true suppresses the log
	// entry. Use this for pattern- or prefix-based skip rules:
	//
	//   cfg.SkipFunc = func(path string) bool {
	//       return strings.HasPrefix(path, "/internal/")
	//   }
	SkipFunc func(path string) bool

	// Logger overrides the global logcore logger. Nil → use the global.
	Logger *logcore.Logger
}

func (c Config) withDefaults() Config {
	if c.SkipPaths == nil {
		c.SkipPaths = []string{"/live", "/ready", "/health"}
	}
	return c
}

// Middleware returns a Fiber v3 handler that emits one structured log
// per request after the handler chain runs. Trace fields (trace.id,
// transaction.id) are attached when apmfiberv3.Middleware ran first.
func Middleware(cfg Config) fiber.Handler {
	cfg = cfg.withDefaults()
	skip := make(map[string]struct{}, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skip[p] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		path := c.Path()
		if _, ok := skip[path]; ok {
			return c.Next()
		}
		if cfg.SkipFunc != nil && cfg.SkipFunc(path) {
			return c.Next()
		}

		start := time.Now()
		hErr := c.Next()
		responseTime := time.Since(start)

		// In Fiber v3 the request ctx is c.RequestCtx() for APM lookup
		// (where apmfasthttp stores the transaction). Use it for trace
		// fields; if apmfiberv3 didn't run it falls back to nothing.
		var logger *zap.Logger
		if cfg.Logger != nil {
			logger = cfg.Logger.LogCtx(c.RequestCtx())
		} else {
			logger = logcore.LogCtx(c.RequestCtx())
		}

		status := getResStatusCode(c)
		msg := fmt.Sprintf("→ incoming → [%s] %s - %s", c.Method(), c.Path(), status)

		incoming := logcore.Incoming{
			Req: &logcore.Req{
				Params:      getReqParams(c),
				QueryString: getReqQueryString(c),
				Headers:     getReqHeaders(c),
				Body:        getReqBody(c),
			},
			Res: &logcore.Res{
				Headers:    getResHeaders(c),
				Body:       getResBody(c),
				StatusCode: status,
			},
			ResponseTime: responseTime.String(),
		}
		if hErr != nil {
			msg := hErr.Error()
			incoming.Error = &msg
		}
		logger.Info(msg, zap.Any("incoming", incoming))
		return hErr
	}
}

func getReqParams(c fiber.Ctx) any {
	route := c.Route()
	if route == nil || len(route.Params) == 0 {
		return nil
	}
	out := make(map[string]string, len(route.Params))
	for _, v := range route.Params {
		out[v] = c.Params(v)
	}
	return out
}

func getReqQueryString(c fiber.Ctx) any {
	qs := c.Request().URI().QueryArgs()
	if qs == nil || qs.String() == "" {
		return nil
	}
	out := make(map[string]string)
	qs.VisitAll(func(k, v []byte) {
		out[string(k)] = string(v)
	})
	return out
}

func getReqHeaders(c fiber.Ctx) any {
	return logcore.FlattenHeaders(c.GetReqHeaders())
}

func getReqBody(c fiber.Ctx) any {
	out := make(map[string]any)
	if err := c.Bind().Body(&out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func getResHeaders(c fiber.Ctx) any {
	headers := c.GetRespHeaders()
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		switch len(v) {
		case 0:
		case 1:
			out[k] = v[0]
		default:
			out[k] = strings.Join(v, ",")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func getResBody(c fiber.Ctx) any {
	return logcore.DecodeJSONBody(c.Response().Body())
}

func getResStatusCode(c fiber.Ctx) string {
	code := c.Response().StatusCode()
	if code == 0 {
		return "Ø"
	}
	return strconv.Itoa(code)
}
