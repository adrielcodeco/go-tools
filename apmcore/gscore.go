package apmcore

import (
	"context"
	"database/sql"
	"time"
)

// CloserRegistrar is the subset of gscore.Manager used by apmcore helpers.
// Using an interface avoids an import cycle between apmcore and gscore.
// *gscore.Manager satisfies this interface directly.
type CloserRegistrar interface {
	RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error)
}

// RegisterWithManager registers the OTel SDK shutdown function as a
// PhasePostDB closer. Call this immediately after SetupOTelSDK so that
// in-flight spans and metrics are flushed before the process exits.
//
// phase must be gscore.PhasePostDB (value 4). Pass 0 to use PhasePostDB.
// timeout=0 defaults to 15s.
//
//	shutdown, err := apmcore.SetupOTelSDK(ctx)
//	apmcore.RegisterWithManager(shutdown, mgr, gscore.PhasePostDB, 0)
func RegisterWithManager(fn ShutdownFunc, mgr CloserRegistrar, phase int, timeout time.Duration) {
	if fn == nil || mgr == nil {
		return
	}
	if phase == 0 {
		phase = 4 // PhasePostDB
	}
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	mgr.RegisterCloser("apmcore-otel-shutdown", phase, timeout, fn)
}

// RegisterDBPoolMetricsWithManager registers the APM pool metrics gatherer and
// wires its deregister function as a PhasePostDB closer. Without deregistering,
// the APM agent collects zeroed metrics from the closed pool for one extra tick.
//
// phase must be gscore.PhasePostDB (value 4). Pass 0 to use PhasePostDB.
// timeout=0 defaults to 5s.
//
//	apmcore.RegisterDBPoolMetricsWithManager(sqlDB, mgr, gscore.PhasePostDB, 0)
func RegisterDBPoolMetricsWithManager(db *sql.DB, mgr CloserRegistrar, phase int, timeout time.Duration) {
	if db == nil || mgr == nil {
		return
	}
	if phase == 0 {
		phase = 4 // PhasePostDB
	}
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	deregister := RegisterDBPoolMetrics(db)
	mgr.RegisterCloser("apmcore-pool-metrics", phase, timeout, func(_ context.Context) error {
		deregister()
		return nil
	})
}
