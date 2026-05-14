package logfiberv3_test

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/logfiberv3"
)

func TestMiddlewareLogsIncoming(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	l, _ := logcore.New(logcore.Options{
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})

	app := fiber.New()
	app.Use(logfiberv3.Middleware(logfiberv3.Config{Logger: l}))
	app.Post("/echo", func(c fiber.Ctx) error {
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
}

func TestMiddlewareSkipPaths(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	l, _ := logcore.New(logcore.Options{
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})

	app := fiber.New()
	app.Use(logfiberv3.Middleware(logfiberv3.Config{Logger: l}))
	app.Get("/health", func(c fiber.Ctx) error { return c.SendStatus(200) })
	app.Get("/orders", func(c fiber.Ctx) error { return c.SendStatus(200) })

	_, _ = app.Test(httptest.NewRequest("GET", "/health", nil))
	_, _ = app.Test(httptest.NewRequest("GET", "/orders", nil))

	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
}
