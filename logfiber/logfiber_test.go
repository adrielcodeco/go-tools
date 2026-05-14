package logfiber_test

import (
	"bytes"
	"net/http/httptest"
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
