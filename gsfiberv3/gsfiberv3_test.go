package gsfiberv3

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestReadinessHandlerReflectsManager(t *testing.T) {
	mgr := New(Config{})
	app := fiber.New()
	app.Get("/ready", ReadinessHandler(mgr))

	resp, err := app.Test(httptest.NewRequest("GET", "/ready", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("ready: got %d want 200", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	mgr.Trigger()
	_ = mgr.Wait()

	resp, err = app.Test(httptest.NewRequest("GET", "/ready", nil))
	if err != nil {
		t.Fatalf("Test post-trigger: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("after trigger: got %d want 503", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestLivenessHandlerAlwaysOK(t *testing.T) {
	app := fiber.New()
	app.Get("/live", LivenessHandler())

	resp, err := app.Test(httptest.NewRequest("GET", "/live", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestStartupHandlerReflectsMarkStarted(t *testing.T) {
	mgr := New(Config{})
	app := fiber.New()
	app.Get("/startup", StartupHandler(mgr))

	// Before MarkStarted → 503.
	resp, err := app.Test(httptest.NewRequest("GET", "/startup", nil))
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("before start: got %d want 503", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	mgr.MarkStarted()

	// After MarkStarted → 200.
	resp, err = app.Test(httptest.NewRequest("GET", "/startup", nil))
	if err != nil {
		t.Fatalf("Test post-start: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("after start: got %d want 200", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestListenAndTriggerCallsTriggerOnError(t *testing.T) {
	app := fiber.New()
	mgr := New(Config{})
	// Port 1 requires root — will fail immediately with a bind error.
	ListenAndTrigger(app, mgr, ":1")
	// Trigger must fire due to the Listen error; Wait should return.
	if err := mgr.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if mgr.IsReady() {
		t.Error("expected mgr.IsReady() == false after trigger")
	}
}

func TestRegisterAppDrainsViaManager(t *testing.T) {
	app := fiber.New()
	mgr := New(Config{})
	RegisterApp(mgr, app)
	mgr.Trigger()
	if err := mgr.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}
