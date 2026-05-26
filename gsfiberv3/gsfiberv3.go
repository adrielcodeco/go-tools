// Package gsfiberv3 is the Fiber v3 adapter for the gscore graceful-shutdown
// engine. See the sibling gsfiber package for Fiber v2.
package gsfiberv3

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/adrielcodeco/go-tools/gscore"
)

// Manager re-exports the core Manager.
type Manager = gscore.Manager

// Config re-exports the core Config.
type Config = gscore.Config

// Hook re-exports the core Hook.
type Hook = gscore.Hook

// Phase enum re-exports.
const (
	PhasePreStop   = gscore.PhasePreStop
	PhaseDrain     = gscore.PhaseDrain
	PhasePostDrain = gscore.PhasePostDrain
	PhaseDB        = gscore.PhaseDB
	PhasePostDB    = gscore.PhasePostDB
)

// New constructs a Manager with the given Config.
func New(cfg Config) *Manager { return gscore.New(cfg) }

type fiberShutdowner struct{ app *fiber.App }

func (s fiberShutdowner) ShutdownWithContext(ctx context.Context) error {
	return s.app.ShutdownWithContext(ctx)
}

// RegisterApp adds a Fiber v3 app to be drained during the drain phase.
func RegisterApp(m *Manager, app *fiber.App) {
	m.RegisterServer(fiberShutdowner{app: app})
}

// ReadinessHandler returns a Fiber v3 handler suitable for a Kubernetes
// readiness probe. It returns 200 while the Manager is ready and 503
// once shutdown has begun.
func ReadinessHandler(m *Manager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if m.IsReady() {
			return c.SendStatus(fiber.StatusOK)
		}
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
}

// LivenessHandler returns a Fiber v3 handler suitable for a Kubernetes
// liveness probe. It always returns 200 — if the process can respond to
// HTTP, it is alive. Keep this handler free of external dependencies
// (DB, cache, etc.) to avoid spurious pod restarts.
func LivenessHandler() fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	}
}

// StartupHandler returns a Fiber v3 handler suitable for a Kubernetes
// startup probe. It returns 503 until MarkStarted is called on the
// Manager, and 200 afterwards. While the startup probe fails, Kubernetes
// suspends liveness and readiness probes, protecting pods with slow boot
// sequences (migrations, cache warm-up, etc.).
func StartupHandler(m *Manager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if m.IsStarted() {
			return c.SendStatus(fiber.StatusOK)
		}
		return c.SendStatus(fiber.StatusServiceUnavailable)
	}
}

// ListenAndTrigger starts the Fiber v3 app on addr in a goroutine. If
// app.Listen returns a non-nil error (e.g. port already in use), it calls
// mgr.Trigger() so the shutdown sequence starts instead of leaving the
// process hanging waiting for a signal that will never arrive.
//
// Typical use at the end of main, after all routes are registered:
//
//	gsfiberv3.ListenAndTrigger(app, mgr, ":8080")
//	if err := mgr.ListenAndWait(); err != nil {
//	    log.Fatal(err)
//	}
func ListenAndTrigger(app *fiber.App, m *Manager, addr string) {
	go func() {
		if err := app.Listen(addr); err != nil {
			m.Trigger()
		}
	}()
}
