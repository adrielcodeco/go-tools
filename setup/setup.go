// Package setup wires the full standard stack in one call with correct
// ordering, eliminating the "register everything in the right order" footgun.
//
// Usage:
//
//	mgr := gscore.New(gscore.Config{})
//	result, err := setup.New().
//	    WithLogger(logcore.Options{Service: "my-service"}).
//	    WithOTel(ctx).
//	    WithGORM(db).
//	    Build(mgr)
//	if err != nil {
//	    log.Fatal(err)
//	}
package setup

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/adrielcodeco/go-tools/apmcore"
	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/txcore"
)

// registrar is the minimal subset of *gscore.Manager required by Build.
// It is satisfied by *gscore.Manager directly. Exposing it as an unexported
// interface lets tests inject a recording fake without changing the public API.
type registrar interface {
	RegisterCloser(name string, phase int, timeout time.Duration, fn func(ctx context.Context) error)
	RegisterCloserWithPriority(name string, phase int, priority int, timeout time.Duration, fn func(ctx context.Context) error)
}

// Builder accumulates configuration via functional options before wiring
// everything in Build. The zero value is valid; calling Build with no options
// registers nothing and returns an empty Result.
type Builder struct {
	loggerOpts    *logcore.Options
	otelCtx       *context.Context
	gormDB        *gorm.DB
	autobatchP    *autobatch.Plugin
	httpClientLog bool
}

// Result is returned by Build and carries handles to the resources that were
// created. Fields are nil/zero when the corresponding With-option was not set.
type Result struct {
	// Logger is the logcore.Logger created by WithLogger; nil if not configured.
	Logger *logcore.Logger
	// Shutdown is the OTel/APM shutdown function registered by WithOTel; nil
	// if not configured.
	Shutdown apmcore.ShutdownFunc
}

// New returns an empty Builder.
func New() *Builder {
	return &Builder{}
}

// WithLogger configures the builder to create a logcore.Logger using opts and
// set it as the process-global logger.
func (b *Builder) WithLogger(opts logcore.Options) *Builder {
	b.loggerOpts = &opts
	return b
}

// WithOTel configures the builder to call apmcore.SetupOTelSDK(ctx) during
// Build. ctx is typically the application root context or context.Background().
func (b *Builder) WithOTel(ctx context.Context) *Builder {
	b.otelCtx = &ctx
	return b
}

// WithGORM configures the builder to register txcore with the Manager so that
// in-flight transactions are drained before the DB pool is closed.
func (b *Builder) WithGORM(db *gorm.DB) *Builder {
	b.gormDB = db
	return b
}

// WithAutobatch configures the builder to register p.Close() as a PhasePostDrain
// closer on the Manager, draining batched writes before the DB pool closes.
func (b *Builder) WithAutobatch(p *autobatch.Plugin) *Builder {
	b.autobatchP = p
	return b
}

// WithHTTPClientLogging configures the builder to install the global logcore
// HTTP client hook via logcore.InstallHTTPClientHook. The hook is installed
// after the logger is set up so it inherits the configured global logger.
func (b *Builder) WithHTTPClientLogging() *Builder {
	b.httpClientLog = true
	return b
}

// Build wires all configured components onto mgr in the required order:
//
//  1. apmcore.SetupOTelSDK            (if WithOTel)
//  2. logcore.New + logcore.SetGlobal (if WithLogger)
//  3. txcore.RegisterWithManager      (if WithGORM)
//  4. autobatch.RegisterWithManager   (if WithAutobatch)
//  5. apmcore.RegisterWithManager     (if WithOTel)
//  6. logcore.RegisterGlobalWithManager (if WithLogger)
//  7. logcore.InstallHTTPClientHook   (if WithHTTPClientLogging)
//
// Build returns an error only if apmcore.SetupOTelSDK or logcore.New fails.
func (b *Builder) Build(mgr *gscore.Manager) (*Result, error) {
	return b.build(mgr)
}

// build is the internal implementation that accepts the registrar interface,
// allowing tests to inject a recording fake without changing the public API.
func (b *Builder) build(mgr registrar) (*Result, error) {
	res := &Result{}

	// 1. OTel SDK setup — must happen before logger so APM core is ready.
	if b.otelCtx != nil {
		shutdown, err := apmcore.SetupOTelSDK(*b.otelCtx)
		if err != nil {
			return nil, err
		}
		res.Shutdown = shutdown
	}

	// 2. Logger — set global so subsequent registrations can log.
	if b.loggerOpts != nil {
		l, err := logcore.New(*b.loggerOpts)
		if err != nil {
			return nil, err
		}
		logcore.SetGlobal(l)
		res.Logger = l
	}

	// 3. txcore — drain in-flight transactions before DB pool closes.
	if b.gormDB != nil {
		txcore.RegisterWithManager(mgr, int(gscore.PhasePostDrain), 35*time.Second)
	}

	// 4. autobatch — drain pending batches after HTTP drain, before DB close.
	if b.autobatchP != nil {
		autobatch.RegisterWithManager(b.autobatchP, mgr, int(gscore.PhasePostDrain), 30*time.Second)
	}

	// 5. OTel shutdown — flush spans and metrics after DB is closed.
	if res.Shutdown != nil {
		apmcore.RegisterWithManager(res.Shutdown, mgr, int(gscore.PhasePostDB), 15*time.Second)
	}

	// 6. Logger sync — flush log buffers last so shutdown logs are not lost.
	if res.Logger != nil {
		logcore.RegisterGlobalWithManager(mgr, int(gscore.PhasePostDB), 5*time.Second)
	}

	// 7. HTTP client logging hook — installed after global logger is set.
	if b.httpClientLog {
		logcore.InstallHTTPClientHook()
	}

	return res, nil
}
