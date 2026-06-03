package apmcore

import (
	"context"
	"time"

	apm "go.elastic.co/apm/v2"

	"github.com/adrielcodeco/go-tools/gscore"
)

// StartupMetricsGatherer implements apm.MetricsGatherer and emits four metrics
// on every APM metrics tick (default 30 s):
//
//   - app.startup.duration_ms  — milliseconds from processStart to MarkStarted.
//     Emits 0 until the startup probe has flipped; once started, the value is
//     stable. Use this to tune Kubernetes initialDelaySeconds / failureThreshold.
//   - app.probe.live           — always 1 (process is alive and responding).
//   - app.probe.ready          — 1 while accepting traffic, 0 during shutdown.
//   - app.probe.started        — 1 after MarkStarted, 0 during boot.
//
// All four metrics land in the metrics-apm.app.<service>-default data stream
// and are chartable from Kibana → Observability → Infrastructure → Metrics.
type StartupMetricsGatherer struct {
	processStart time.Time
	isReady      func() bool
	isStarted    func() bool
	startedAt    func() time.Time
}

// NewStartupMetricsGatherer constructs a StartupMetricsGatherer.
//
//   - processStart is the wall-clock time the process was launched (passed by
//     the caller so the gatherer does not call time.Now() internally, keeping
//     it deterministic and test-friendly).
//   - mgr is the gscore.Manager whose probe state is reflected in the metrics.
func NewStartupMetricsGatherer(processStart time.Time, mgr *gscore.Manager) *StartupMetricsGatherer {
	return &StartupMetricsGatherer{
		processStart: processStart,
		isReady:      mgr.IsReady,
		isStarted:    mgr.IsStarted,
		startedAt:    mgr.StartedAt,
	}
}

// GatherMetrics implements apm.MetricsGatherer.
func (g *StartupMetricsGatherer) GatherMetrics(_ context.Context, m *apm.Metrics) error {
	var startupMs float64
	if sat := g.startedAt(); !sat.IsZero() {
		startupMs = float64(sat.Sub(g.processStart).Milliseconds())
	}
	m.Add("app.startup.duration_ms", nil, startupMs)

	live := 1.0
	m.Add("app.probe.live", nil, live)

	ready := 0.0
	if g.isReady() {
		ready = 1.0
	}
	m.Add("app.probe.ready", nil, ready)

	started := 0.0
	if g.isStarted() {
		started = 1.0
	}
	m.Add("app.probe.started", nil, started)

	return nil
}

// RegisterStartupMetricsWithManager registers a StartupMetricsGatherer with
// the APM default tracer and wires its deregister function as a PhasePostDB
// closer so the gatherer is cleanly removed before the APM agent shuts down.
//
//   - processStart is the wall-clock time the process was launched. Pass
//     time.Now() from main before any other setup runs so the duration covers
//     the full boot sequence including dependency wiring.
//   - mgr is the gscore.Manager whose probe state is reflected in the metrics.
//
// The deregister closer uses a 5 s timeout, matching RegisterDBPoolMetricsWithManager.
func RegisterStartupMetricsWithManager(processStart time.Time, mgr *gscore.Manager) {
	if mgr == nil {
		return
	}
	g := NewStartupMetricsGatherer(processStart, mgr)
	deregister := apm.DefaultTracer().RegisterMetricsGatherer(g)
	mgr.RegisterCloser("apmcore-startup-metrics", int(gscore.PhasePostDB), 5*time.Second, func(_ context.Context) error {
		deregister()
		return nil
	})
}
