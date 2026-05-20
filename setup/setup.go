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

	fiberv2 "github.com/gofiber/fiber/v2"
	fiberv3 "github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"github.com/redis/rueidis"
	"gorm.io/gorm"

	"github.com/adrielcodeco/go-tools/apmcore"
	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/gsfiber"
	"github.com/adrielcodeco/go-tools/gsfiberv3"
	"github.com/adrielcodeco/go-tools/logcore"
	"github.com/adrielcodeco/go-tools/txcore"
)

// registrar is the minimal subset of *gscore.Manager required by Build.
// It is a type alias for gscore.CloserRegistrar; *gscore.Manager satisfies it directly.
// Kept as a private alias so tests can inject a recording fake without changing the public API.
type registrar = gscore.CloserRegistrar

// redisEntry holds a go-redis UniversalClient and its log name.
type redisEntry struct {
	client redis.UniversalClient
	name   string
}

// rueidisEntry holds a rueidis Client and its log name.
type rueidisEntry struct {
	client rueidis.Client
	name   string
}

// Builder accumulates configuration via functional options before wiring
// everything in Build. The zero value is valid; calling Build with no options
// registers nothing and returns an empty Result.
type Builder struct {
	loggerOpts     *logcore.Options
	otelCtx        *context.Context
	gormDB         *gorm.DB
	autobatchP     *autobatch.Plugin
	autobatchCfg   *autobatch.Config
	httpClientLog  bool
	redisClients   []redisEntry
	rueidisClients []rueidisEntry
	fiberV2App     *fiberv2.App
	fiberV3App     *fiberv3.App
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

// WithAutobatchConfig configures the builder to create an autobatch plugin from
// cfg and register it with the Manager. When WithOTel is also set, Build
// automatically injects cfg.SpanEmitter = apmcore.BatchSpanEmitter() before
// creating the plugin so batched writes appear in APM traces.
func (b *Builder) WithAutobatchConfig(cfg autobatch.Config) *Builder {
	b.autobatchCfg = &cfg
	return b
}

// WithHTTPClientLogging configures the builder to install the global logcore
// HTTP client hook via logcore.InstallHTTPClientHook. The hook is installed
// after the logger is set up so it inherits the configured global logger.
func (b *Builder) WithHTTPClientLogging() *Builder {
	b.httpClientLog = true
	return b
}

// WithRedis registers a go-redis UniversalClient for graceful shutdown at
// PhasePostDB and, if WithOTel was also called, instruments it with OTel
// tracing and metrics. Multiple calls accumulate; each client is registered
// independently. name is used in shutdown log lines.
func (b *Builder) WithRedis(client redis.UniversalClient, name string) *Builder {
	b.redisClients = append(b.redisClients, redisEntry{client: client, name: name})
	return b
}

// WithRueidis registers a rueidis Client for graceful shutdown at PhasePostDB
// and, if WithOTel was also called, wraps it with OTel tracing and metrics.
// Multiple calls accumulate; each client is registered independently.
// name is used in shutdown log lines.
func (b *Builder) WithRueidis(client rueidis.Client, name string) *Builder {
	b.rueidisClients = append(b.rueidisClients, rueidisEntry{client: client, name: name})
	return b
}

// WithFiberV2 registers a Fiber v2 app on the Manager so it is drained during
// the drain phase of graceful shutdown. Multiple calls are not supported;
// only the last supplied app is registered.
func (b *Builder) WithFiberV2(app *fiberv2.App) *Builder {
	b.fiberV2App = app
	return b
}

// WithFiberV3 registers a Fiber v3 app on the Manager so it is drained during
// the drain phase of graceful shutdown. Multiple calls are not supported;
// only the last supplied app is registered.
func (b *Builder) WithFiberV3(app *fiberv3.App) *Builder {
	b.fiberV3App = app
	return b
}

// Build wires all configured components onto mgr in the required order:
//
//  0. gsfiber.RegisterApp / gsfiberv3.RegisterApp  (if WithFiberV2 / WithFiberV3)
//  1. apmcore.SetupOTelSDK                         (if WithOTel)
//  2. logcore.New + logcore.SetGlobal              (if WithLogger)
//  3. apmcore.NewGormPlugin via db.Use             (if WithGORM + WithOTel)
//  4. mgr.RegisterDB                               (if WithGORM)
//  5. txcore.RegisterWithManager                   (if WithGORM)
//  6. apmcore.RegisterDBPoolMetricsWithManager     (if WithGORM + WithOTel)
//  7. autobatch.RegisterWithManager                (if WithAutobatch or WithAutobatchConfig)
//  8. go-redis closers + optional OTel hooks       (if WithRedis)
//  9. rueidis closers + optional OTel wrapping     (if WithRueidis)
//  10. apmcore.RegisterWithManager                 (if WithOTel)
//  11. logcore.RegisterGlobalWithManager           (if WithLogger)
//  12. logcore.InstallHTTPClientHook               (if WithHTTPClientLogging)
//
// Build returns an error only if apmcore.SetupOTelSDK, logcore.New, or
// db.Use(apmcore.NewGormPlugin()) fails.
func (b *Builder) Build(mgr *gscore.Manager) (*Result, error) {
	return b.build(mgr)
}

// build is the internal implementation that accepts the registrar interface,
// allowing tests to inject a recording fake without changing the public API.
func (b *Builder) build(mgr registrar) (*Result, error) {
	res := &Result{}

	// 0. Register Fiber apps — must happen before any closers so the server is
	// known to the Manager before ListenAndWait is called.
	if b.fiberV2App != nil {
		if m, ok := mgr.(*gscore.Manager); ok {
			gsfiber.RegisterApp(m, b.fiberV2App)
		}
	}
	if b.fiberV3App != nil {
		if m, ok := mgr.(*gscore.Manager); ok {
			gsfiberv3.RegisterApp(m, b.fiberV3App)
		}
	}

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

	// 3. GORM OTel plugin — install before any txcore/autobatch registration so
	// all callbacks are in place when the first query runs.
	if b.gormDB != nil && b.otelCtx != nil {
		if err := b.gormDB.Use(apmcore.NewGormPlugin()); err != nil {
			return nil, err
		}
	}

	// 4. RegisterDB — ensure the pool is closed during PhaseDB.
	if b.gormDB != nil {
		if m, ok := mgr.(*gscore.Manager); ok {
			m.RegisterDB(b.gormDB)
		}
	}

	// 5. txcore — drain in-flight transactions before DB pool closes.
	if b.gormDB != nil {
		txcore.RegisterWithManager(mgr, int(gscore.PhasePostDrain), 35*time.Second)
	}

	// 6. DB pool metrics — register APM gatherer and its deregister closer.
	if b.gormDB != nil && b.otelCtx != nil {
		if sqlDB, err := b.gormDB.DB(); err == nil {
			apmcore.RegisterDBPoolMetricsWithManager(sqlDB, mgr, int(gscore.PhasePostDB), 0)
		}
		// If sqlDB extraction fails, skip metrics silently — don't abort Build.
	}

	// 7. autobatch — drain pending batches after HTTP drain, before DB close.
	if b.autobatchCfg != nil {
		cfg := *b.autobatchCfg
		if b.otelCtx != nil {
			cfg.SpanEmitter = apmcore.BatchSpanEmitter()
		}
		p := autobatch.New(cfg)
		autobatch.RegisterWithManager(p, mgr, int(gscore.PhasePostDrain), 30*time.Second)
		if b.gormDB != nil {
			_ = b.gormDB.Use(p)
		}
	} else if b.autobatchP != nil {
		autobatch.RegisterWithManager(b.autobatchP, mgr, int(gscore.PhasePostDrain), 30*time.Second)
	}

	// 8a. go-redis clients — optionally instrument with OTel, then register closer.
	for _, e := range b.redisClients {
		if res.Shutdown != nil {
			// Ignore instrumentation error; tracing is best-effort.
			_ = apmcore.InstrumentRedis(e.client)
		}
		client := e.client
		mgr.RegisterCloser(e.name, int(gscore.PhasePostDB), 5*time.Second, func(ctx context.Context) error {
			done := make(chan error, 1)
			go func() { done <- client.Close() }()
			select {
			case err := <-done:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	// 8b. rueidis clients — optionally wrap with OTel, then register closer.
	for _, e := range b.rueidisClients {
		c := e.client
		if res.Shutdown != nil {
			c = apmcore.InstrumentRueidis(c)
		}
		name := e.name
		mgr.RegisterCloser(name, int(gscore.PhasePostDB), 5*time.Second, func(ctx context.Context) error {
			done := make(chan struct{})
			go func() { c.Close(); close(done) }()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	// 9. OTel shutdown — flush spans and metrics after DB is closed.
	if res.Shutdown != nil {
		apmcore.RegisterWithManager(res.Shutdown, mgr, int(gscore.PhasePostDB), 15*time.Second)
	}

	// 10. Logger sync — flush log buffers last so shutdown logs are not lost.
	if res.Logger != nil {
		logcore.RegisterGlobalWithManager(mgr, int(gscore.PhasePostDB), 5*time.Second)
	}

	// 11. HTTP client logging hook — installed after global logger is set.
	if b.httpClientLog {
		logcore.InstallHTTPClientHook()
	}

	return res, nil
}
