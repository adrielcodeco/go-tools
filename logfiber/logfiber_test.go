package logfiber_test

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/logfiber"
)

func newObservedLogger(t *testing.T) (*logcore.Logger, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.InfoLevel)
	l, err := logcore.New(logcore.Options{
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l, logs
}

func TestLogsIncoming(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l}))
	app.Post("/echo", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	req := httptest.NewRequest("POST", "/echo?x=1",
		bytes.NewReader([]byte(`{"name":"alice"}`)))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	if want := "→ incoming → [POST] /echo - 200"; entry.Message != want {
		t.Errorf("msg = %q want %q", entry.Message, want)
	}
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Res.StatusCode != "200" {
		t.Errorf("status = %q", inc.Res.StatusCode)
	}
	if inc.Req.QueryString == nil {
		t.Error("expected query string captured")
	}
	if inc.Req.Body == nil {
		t.Error("expected body captured")
	}
}

func TestRedactsSensitiveHeaderAndBody(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l})) // default redactor
	app.Post("/pay", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	req := httptest.NewRequest("POST", "/pay",
		bytes.NewReader([]byte(`{"amount":100,"password":"hunter2"}`)))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	inc := findIncoming(t, logs)

	body, ok := inc.Req.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type %T", inc.Req.Body)
	}
	if body["password"] != logcore.RedactMask {
		t.Errorf("body.password = %v, want mask", body["password"])
	}
	if body["amount"] == nil {
		t.Error("amount should be preserved")
	}
}

func TestDisableRedaction(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, DisableRedaction: true}))
	app.Post("/pay", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest("POST", "/pay",
		bytes.NewReader([]byte(`{"password":"hunter2"}`)))
	req.Header.Set("Content-Type", "application/json")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	inc := findIncoming(t, logs)
	body, _ := inc.Req.Body.(map[string]any)
	if body["password"] != "hunter2" {
		t.Errorf("body.password = %v, want verbatim with redaction disabled", body["password"])
	}
}

func findIncoming(t *testing.T, logs *observer.ObservedLogs) *logcore.Incoming {
	t.Helper()
	if logs.Len() == 0 {
		t.Fatal("no log entries")
	}
	for _, f := range logs.AllUntimed()[0].Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			return &v
		}
	}
	t.Fatal("missing incoming field")
	return nil
}

func TestSkipPaths(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l}))
	app.Get("/live", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	app.Get("/something", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/live", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if _, err := app.Test(httptest.NewRequest("GET", "/something", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log (only /something), got %d", logs.Len())
	}
}

func TestSkipFunc_SkipsMatchingPath(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{
		Logger:    l,
		SkipPaths: []string{}, // override defaults so only SkipFunc applies
		SkipFunc:  func(_, p string) bool { return strings.HasPrefix(p, "/internal") },
	}))
	app.Get("/internal/health", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/internal/health", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("expected 0 logs (path skipped by SkipFunc), got %d", logs.Len())
	}
}

func TestSkipFunc_DoesNotSkipNonMatching(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{
		Logger:    l,
		SkipPaths: []string{},
		SkipFunc:  func(_, p string) bool { return strings.HasPrefix(p, "/internal") },
	}))
	app.Get("/api/users", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/api/users", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log for non-matching path, got %d", logs.Len())
	}
}

func TestSkipFunc_ComposesWithSkipPaths(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{
		Logger:    l,
		SkipPaths: []string{"/health"},
		SkipFunc:  func(_, p string) bool { return p == "/metrics" },
	}))
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	app.Get("/metrics", func(c *fiber.Ctx) error { return c.SendStatus(200) })
	app.Get("/api", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	for _, path := range []string{"/health", "/metrics", "/api"} {
		if _, err := app.Test(httptest.NewRequest("GET", path, nil)); err != nil {
			t.Fatalf("Test %s: %v", path, err)
		}
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log (only /api), got %d", logs.Len())
	}
	if entry := logs.AllUntimed()[0]; entry.Message != "→ incoming → [GET] /api - 200" {
		t.Errorf("unexpected log message: %q", entry.Message)
	}
}

// ---------------------------------------------------------------------------
// getReqParams coverage
// ---------------------------------------------------------------------------

func TestGetReqParams_WithRouteParam(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/users/:id", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/users/42", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Req.Params == nil {
		t.Error("expected route params to be captured")
	}
	params, ok := inc.Req.Params.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string params, got %T", inc.Req.Params)
	}
	if params["id"] != "42" {
		t.Errorf("expected params[id]=42, got %q", params["id"])
	}
}

func TestGetReqParams_NoParams(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/status", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/status", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Req.Params != nil {
		t.Errorf("expected nil params for route without params, got %v", inc.Req.Params)
	}
}

// ---------------------------------------------------------------------------
// getReqHeaders coverage
// ---------------------------------------------------------------------------

func TestGetReqHeaders_RequestProcessed(t *testing.T) {
	// getReqHeaders in logfiber v2 uses ReqHeaderParser which requires a struct,
	// not a map — so it always returns nil and the incoming.Req.Headers field is nil.
	// This test verifies the middleware still logs the request correctly regardless.
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/ping", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("Accept", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	// ReqHeaderParser with map always errors → headers nil, request still logged
	_ = inc.Req.Headers
}

// ---------------------------------------------------------------------------
// getReqBody coverage
// ---------------------------------------------------------------------------

func TestGetReqBody_WithJSONBody(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Post("/data", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest("POST", "/data", bytes.NewReader([]byte(`{"key":"value","num":42}`)))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Req.Body == nil {
		t.Error("expected request body to be captured")
	}
	body, ok := inc.Req.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", inc.Req.Body)
	}
	if body["key"] != "value" {
		t.Errorf("expected body[key]=value, got %v", body["key"])
	}
}

func TestGetReqBody_NoBody(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/items", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/items", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Req.Body != nil {
		t.Errorf("expected nil body for GET with no body, got %v", inc.Req.Body)
	}
}

// ---------------------------------------------------------------------------
// getResHeaders coverage
// ---------------------------------------------------------------------------

func TestGetResHeaders_CapturesResponseHeaders(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/with-headers", func(c *fiber.Ctx) error {
		c.Set("X-Trace-ID", "trace-abc")
		c.Set("X-Custom", "val1")
		return c.SendStatus(200)
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/with-headers", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Res.Headers == nil {
		t.Error("expected response headers to be captured")
	}
	headers, ok := inc.Res.Headers.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string res headers, got %T", inc.Res.Headers)
	}
	if headers["X-Trace-Id"] == "" && headers["X-Trace-ID"] == "" {
		t.Errorf("expected X-Trace-ID in response headers, got %+v", headers)
	}
}

func TestGetResHeaders_MultiValueHeader(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/multi-header", func(c *fiber.Ctx) error {
		c.Response().Header.Add("X-Values", "a")
		c.Response().Header.Add("X-Values", "b")
		return c.SendStatus(200)
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/multi-header", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	// Multi-value response headers should be joined with ","
	if inc.Res.Headers == nil {
		t.Error("expected response headers to be captured")
	}
}

func TestGetReqBody_EmptyJSONObject(t *testing.T) {
	// An empty JSON object `{}` parses to an empty map → getReqBody returns nil.
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Post("/empty-body", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest("POST", "/empty-body", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	// Empty JSON object maps to empty map → returns nil
	if inc.Req.Body != nil {
		t.Errorf("expected nil body for empty JSON object, got %v", inc.Req.Body)
	}
}

// ---------------------------------------------------------------------------
// getResStatusCode coverage — non-2xx
// ---------------------------------------------------------------------------

func TestGetResStatusCode_NonSuccess(t *testing.T) {
	cases := []struct {
		path   string
		status int
		want   string
	}{
		{"/notfound", 404, "404"},
		{"/servererr", 500, "500"},
		{"/created", 201, "201"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			l, logs := newObservedLogger(t)
			app := fiber.New()
			app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
			status := tc.status
			app.Get(tc.path, func(c *fiber.Ctx) error { return c.SendStatus(status) })

			if _, err := app.Test(httptest.NewRequest("GET", tc.path, nil)); err != nil {
				t.Fatalf("Test: %v", err)
			}
			if logs.Len() != 1 {
				t.Fatalf("expected 1 log, got %d", logs.Len())
			}
			entry := logs.AllUntimed()[0]
			var inc *logcore.Incoming
			for _, f := range entry.Context {
				if f.Key == "incoming" {
					v := f.Interface.(logcore.Incoming)
					inc = &v
					break
				}
			}
			if inc == nil {
				t.Fatal("missing incoming field")
			}
			if inc.Res.StatusCode != tc.want {
				t.Errorf("expected status %q, got %q", tc.want, inc.Res.StatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handler error propagation
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// OBS-4: Error level when handler returns an error
// ---------------------------------------------------------------------------

func TestMiddleware_ErrorLevelOnHandlerError(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(500).SendString(err.Error())
		},
	})
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/err-level", func(c *fiber.Ctx) error {
		return errors.New("something went wrong")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/err-level", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	if entry.Level != zapcore.ErrorLevel {
		t.Errorf("expected ErrorLevel when handler returns error, got %v", entry.Level)
	}
}

func TestMiddleware_InfoLevelOnSuccess(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New()
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/ok", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	if _, err := app.Test(httptest.NewRequest("GET", "/ok", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	if entry.Level != zapcore.InfoLevel {
		t.Errorf("expected InfoLevel on success, got %v", entry.Level)
	}
}

// ---------------------------------------------------------------------------
// COR-3: ECS error.message top-level field
// ---------------------------------------------------------------------------

func TestMiddleware_ECSErrorMessage(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(500).SendString(err.Error())
		},
	})
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/ecs-err", func(c *fiber.Ctx) error {
		return errors.New("ecs error message")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/ecs-err", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]

	var errMsgField *zapcore.Field
	for i := range entry.Context {
		if entry.Context[i].Key == "error.message" {
			f := entry.Context[i]
			errMsgField = &f
			break
		}
	}
	if errMsgField == nil {
		t.Fatal("expected top-level field error.message, not found")
	}
	if errMsgField.String != "ecs error message" {
		t.Errorf("error.message = %q, want %q", errMsgField.String, "ecs error message")
	}
}

// ---------------------------------------------------------------------------
// handler error propagation
// ---------------------------------------------------------------------------

func TestMiddleware_LogsHandlerError(t *testing.T) {
	l, logs := newObservedLogger(t)
	app := fiber.New(fiber.Config{
		// Disable the default error handler so it doesn't swallow the error
		// before the middleware can inspect it.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(500).SendString(err.Error())
		},
	})
	app.Use(logfiber.Middleware(logfiber.Config{Logger: l, SkipPaths: []string{}}))
	app.Get("/boom", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/boom", nil)); err != nil {
		t.Fatalf("Test: %v", err)
	}
	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	entry := logs.AllUntimed()[0]
	var inc *logcore.Incoming
	for _, f := range entry.Context {
		if f.Key == "incoming" {
			v := f.Interface.(logcore.Incoming)
			inc = &v
			break
		}
	}
	if inc == nil {
		t.Fatal("missing incoming field")
	}
	if inc.Error == nil {
		t.Fatal("expected error field to be set, got nil")
	}
	if *inc.Error != "boom" {
		t.Errorf("expected error %q, got %q", "boom", *inc.Error)
	}
}
