# go-tools

A toolbox for production [Fiber](https://github.com/gofiber/fiber) + [GORM](https://gorm.io) services. It ships three complementary primitives that share the same design philosophy (framework-agnostic core + thin Fiber adapters for v2 and v3):

1. **`txctx` / `txctxv3`** — request-scoped database transactions with lazy `BEGIN`, automatic rollback on error/timeout/panic, and commit/rollback callbacks.
2. **`gsfiber` / `gsfiberv3`** — Kubernetes-aware graceful shutdown for Fiber + GORM + outbound calls, with ordered phases, hooks, liveness/readiness/startup probes, and force-kill ceiling.
3. **`apmfiber` / `apmfiberv3`** — Elastic APM tracing for Fiber + GORM + outgoing HTTP + Redis: foldable DB spans, log↔trace correlation, transaction labels, error capture, and DB-pool metrics.
4. **`gsrueidis`** — Rueidis adapter for the graceful-shutdown manager, with timeout-bounded close so a wedged Redis client cannot stall shutdown.
5. **`gsredis`** — go-redis/v9 adapter for the graceful-shutdown manager, analogous to gsrueidis.
6. **`httpclient`** — Generics-friendly fasthttp client wrapper with built-in APM tracing, pluggable structured-log hook, configurable retry, and sonic JSON.
7. **`logcore` + `logfiber` / `logfiberv3`** — Structured zap logger pre-wired for APM (auto-error capture, trace.id correlation), with Fiber incoming middleware and an httpclient outgoing hook that share the same `req`/`res`/`responseTime` schema.
8. **`gormautobatch`** — GORM plugin that transparently batches Create/Update/Delete operations based on measured P95 latency, reducing round-trips under load with per-op SAVEPOINT isolation.
9. **`gormcache`** — GORM plugin for query result caching and request deduplication (easer). Ships with ready-made Redis and Rueidis backends (`gsredis.NewRedisCacher`, `gsrueidis.NewRueidisCache`) and optional OTel instrumentation via `apmcore.InstrumentCacher`.
10. **`sentrycore` + `sentryfiber` / `sentryfiberv3`** — Sentry error/crash reporting: no-op when the DSN is empty, a zap hook that reports `Error`+ logs, Fiber panic-recovery middleware, and outgoing-HTTP breadcrumbs — all cross-linked to the Elastic APM trace. Complements (does not replace) `apmfiber`, which owns tracing and metrics.
11. **`setup`** — One-call bootstrap that wires the gscore Manager, Fiber app, GORM, APM, Sentry, logger, httpclient, gsredis, gsrueidis, and gormcache together in the correct order. Recommended for production services.

**Module:** `github.com/adrielcodeco/go-tools`

| Feature | Fiber v2 | Fiber v3 | Go min |
|---|---|---|---|
| Request-scoped transactions | `…/txctx` | `…/txctxv3` | 1.22 / 1.25 |
| Graceful shutdown | `…/gsfiber` | `…/gsfiberv3` | 1.22 / 1.25 |
| Elastic APM instrumentation | `…/apmfiber` | `…/apmfiberv3` | 1.22 / 1.25 |
| Sentry error reporting | `…/sentryfiber` | `…/sentryfiberv3` | 1.25 |

Each trio shares a framework-agnostic engine (`txcore`, `gscore`, `apmcore`, `sentrycore`) so both Fiber versions have identical semantics.

---

## Table of Contents

- [Quick Setup (`setup`)](#quick-setup-setup)
- [Module integration map](#module-integration-map)
  - [Dependency graph](#dependency-graph)
  - [APM + txctx: span parenting](#apm--txctx-span-parenting)
  - [APM + gormautobatch: batch spans](#apm--gormautobatch-batch-spans)
  - [APM + logcore: trace correlation](#apm--logcore-trace-correlation)
  - [APM + httpclient: exit spans + outgoing logs](#apm--httpclient-exit-spans--outgoing-logs)
  - [gscore + everything: graceful shutdown registration order](#gscore--everything-graceful-shutdown-registration-order)
  - [gsredis / gsrueidis: which one to use](#gsredis--gsrueidis-which-one-to-use)
  - [logcore + gscore: logger lifecycle](#logcore--gscore-logger-lifecycle)
- [Transactions (`txctx` / `txctxv3`)](#transactions-txctx--txctxv3)
  - [Features](#features)
  - [Installation](#installation)
  - [Public API](#public-api)
  - [Usage](#usage)
  - [Commit / Rollback Decision Table](#commit--rollback-decision-table)
  - [Propagating cancellation to outbound calls](#propagating-cancellation-to-outbound-calls)
- [Graceful Shutdown (`gsfiber` / `gsfiberv3`)](#graceful-shutdown-gsfiber--gsfiberv3)
  - [Features](#features-1)
  - [Installation](#installation-1)
  - [Public API](#public-api-1)
  - [Phases](#phases)
  - [Usage](#usage-1)
  - [Kubernetes integration](#kubernetes-integration)
    - [Health probe handlers](#health-probe-handlers)
    - [Recommended Kubernetes manifest](#recommended-kubernetes-manifest)
    - [Shutdown sequence on kubectl delete pod](#shutdown-sequence-on-kubectl-delete-pod)
- [Rueidis graceful shutdown (`gsrueidis`)](#rueidis-gsrueidis)
- [Redis graceful shutdown (`gsredis`)](#redis-graceful-shutdown-gsredis)
- [GORM Auto-batch (`gormautobatch`)](#gorm-auto-batch-gormautobatch)
- [GORM Cache (`gormcache`)](#gorm-cache-gormcache)
  - [Features](#features-3)
  - [Installation](#installation-3)
  - [Quick start](#quick-start)
  - [Easer (request deduplication)](#easer-request-deduplication)
  - [Cacher interface](#cacher-interface)
  - [Ready-made backends](#ready-made-backends)
  - [Tag-based invalidation](#tag-based-invalidation)
  - [OTel instrumentation](#otel-instrumentation)
  - [setup integration](#setup-integration)
- [Elastic APM (`apmfiber` / `apmfiberv3`)](#elastic-apm-apmfiber--apmfiberv3)
  - [Packages](#packages)
  - [Features](#features-2)
  - [Installation](#installation-2)
  - [Quick start (Fiber v2)](#quick-start-fiber-v2)
  - [Manager integration](#manager-integration)
  - [Startup and probe metrics](#startup-and-probe-metrics)
  - [APM transaction context in txctx](#apm-transaction-context-in-txctx)
  - [Local stack](#local-stack)
  - [Pitfall index](#pitfall-index)
- [Sentry error reporting (`sentryfiber` / `sentryfiberv3`)](#sentry-error-reporting-sentryfiber--sentryfiberv3)
  - [Packages](#packages-1)
  - [How it relates to Elastic APM](#how-it-relates-to-elastic-apm)
  - [Features](#features-3)
  - [Installation](#installation-3)
  - [Configuration](#configuration)
  - [Quick start (Fiber v2)](#quick-start-fiber-v2-1)
  - [Quick start (Fiber v3)](#quick-start-fiber-v3)
  - [Via `setup.Builder` (recommended)](#via-setupbuilder-recommended)
  - [Log → Sentry hook](#log--sentry-hook)
  - [Manager integration](#manager-integration-1)
  - [Sharing a custom redactor](#sharing-a-custom-redactor)
  - [Pitfall index](#pitfall-index-1)
- [HTTP client (`httpclient`)](#http-client-httpclient)
- [Structured logging (`logcore` / `logfiber` / `logfiberv3`)](#structured-logging-logcore--logfiber--logfiberv3)

---

## Quick Setup (`setup`)

The `setup` package wires the full standard stack in one call with the correct
registration order, eliminating the manual "register everything in the right
order" ceremony. It is the recommended way to bootstrap a production service.

```bash
go get github.com/adrielcodeco/go-tools/setup
```

```go
func main() {
    processStart := time.Now() // capture as early as possible — covers full boot duration

    ctx := context.Background()
    db := openGORM()
    app := fiber.New()

    mgr := gscore.New(gscore.Config{
        PreStopDelay:   5 * time.Second,
        DrainTimeout:   25 * time.Second,
        DBCloseTimeout: 5 * time.Second,
        ForceKillAfter: 55 * time.Second, // must be < terminationGracePeriodSeconds
    })

    result, err := setup.New().
        WithLogger(logcore.Options{Service: "my-service", Version: "1.0.0"}).
        WithOTel(ctx).
        WithGORM(db).
        WithFiberV2(app).
        WithHealthProbesV2(setup.HealthProbesConfig{}). // registers /healthz/{live,ready,startup}
        WithHTTPClientLogging().
        WithProcessStart(processStart).                 // enables app.startup.duration_ms metric
        WithStartupFn(func() error {                    // runs migrations; calls MarkStarted on success
            return runMigrations(db)
        }).
        Build(mgr)
    if err != nil {
        log.Fatal(err) // includes startup function errors
    }
    defer result.Shutdown(ctx)

    registerRoutes(app)

    // Background workers must derive their context from mgr.RootContext() so
    // they are cancelled the instant SIGTERM is received, before the drain starts.
    go pollWorker(mgr.RootContext())

    // ListenAndTrigger starts the server in a goroutine and calls mgr.Trigger()
    // if Listen fails (e.g. port already in use), so the process never hangs
    // waiting for a signal that will never arrive.
    gsfiber.ListenAndTrigger(app, mgr, ":8080")

    if err := mgr.ListenAndWait(); err != nil {
        log.Fatal(err)
    }
}
```

`Build` performs all registrations in a fixed, safe order:

1. Register Fiber app with the Manager
2. `apmcore.SetupOTelSDK` — OTel/APM bootstrap
3. `sentrycore.SetupSentry` — Sentry bootstrap (if `WithSentry` set; no-op on empty DSN). Runs after APM so trace tags resolve and before the logger so a `SentryHook` logger core has a live client.
4. `logcore.New` + `logcore.SetGlobal` — structured logger
5. Install the GORM APM plugin (`apmcore.NewGormPlugin`)
6. `mgr.RegisterDB` — GORM pool close during `PhaseDB`
7. `txcore.RegisterWithManager` — drain in-flight transactions before DB closes
8. `apmcore.RegisterDBPoolMetricsWithManager` — deregister pool metric gatherer at shutdown
9. `autobatch.RegisterWithManager` (if `WithAutobatch`/`WithAutobatchConfig` set)
10. `db.Use(gormcache plugin)` (if `WithGORMCache`/`WithGORMCacheConfig` set; `Cacher` auto-wrapped with OTel if `WithOTel` set)
11. go-redis closers + optional OTel instrumentation (if `WithRedis`)
12. rueidis closers + optional OTel wrapping (if `WithRueidis`)
13. `apmcore.RegisterWithManager` — flush OTel spans/metrics at `PhasePostDB`
14. `sentrycore.RegisterWithManager` — flush buffered Sentry events at `PhasePostDB` (if `WithSentry` set)
15. `apmcore.RegisterStartupMetricsWithManager` — register startup/probe metric gatherer (if `WithOTel` + `WithProcessStart`)
16. `logcore.RegisterGlobalWithManager` — flush zap buffers last

### Builder API

```go
setup.New().
    WithLogger(logcore.Options{...}).          // create + set global logger
    WithOTel(ctx).                             // call SetupOTelSDK
    WithSentry(ctx, sentrycore.Options{...}).  // call SetupSentry (no-op on empty DSN)
    WithGORM(db).                              // register GORM plugin, DB close, txcore
    WithAutobatchConfig(autobatch.Config{...}). // create + register autobatch plugin
    WithAutobatch(existingPlugin).             // register a pre-built autobatch plugin
    WithGORMCacheConfig(caches.Config{...}).   // create + register gormcache plugin (OTel auto-wired)
    WithGORMCache(existingPlugin).             // register a pre-built gormcache plugin
    WithRedis(client, "redis-cache").          // go-redis closer + OTel instrumentation
    WithRueidis(client, "redis-pubsub").       // rueidis closer + OTel wrapping
    WithFiberV2(app).                          // register Fiber v2 app for drain
    WithHealthProbesV2(setup.HealthProbesConfig{}). // register /healthz/{live,ready,startup}
    WithFiberV3(app).                          // register Fiber v3 app for drain
    WithHealthProbesV3(setup.HealthProbesConfig{}). // register /healthz/{live,ready,startup}
    WithHTTPClientLogging().                   // install logcore hook on httpclient
    WithProcessStart(processStart).            // origin timestamp for app.startup.duration_ms
    WithStartupFn(func() error {               // run boot logic; calls MarkStarted on success
        return runMigrations(db)
    }).
    Build(mgr)                                 // wire everything; returns *Result, error
```

`WithHealthProbesV2` / `WithHealthProbesV3` must be called after the
corresponding `WithFiberV2` / `WithFiberV3`. Calling `WithHealthProbesV2`
without a prior `WithFiberV2` causes `Build` to return an error.
`HealthProbesConfig` may be zero-valued to use the defaults
(`/healthz/live`, `/healthz/ready`, `/healthz/startup`), or you can
override individual paths:

```go
WithHealthProbesV2(setup.HealthProbesConfig{
    LivenessPath:  "/live",
    ReadinessPath: "/ready",
    StartupPath:   "/startup",
})
```

`Result.Logger` is the `*logcore.Logger` created by `WithLogger` (nil if not
set). `Result.Shutdown` is the OTel shutdown function from `SetupOTelSDK`.
`Result.SentryShutdown` is the Sentry flush function from `WithSentry` (nil if
not set; a no-op when the DSN is empty).

To auto-report `Error`+ logs to Sentry, enable the hook on the logger — it is a
no-op until `WithSentry` initializes a live client, so it is safe to leave on:

```go
setup.New().
    WithSentry(ctx, sentrycore.Options{}).                       // DSN from SENTRY_DSN
    WithLogger(logcore.Options{SentryHook: true, RedactFields: true}).
    Build(mgr)
```

When `WithOTel` and `WithAutobatchConfig` are both set, `Build` automatically
injects `cfg.SpanEmitter = apmcore.BatchSpanEmitter()` so batched writes appear
as APM spans without any extra configuration.

---

## Module integration map

Every module in this toolbox is independently importable, but they are designed
to compose. This section shows how they connect and what order matters.

### Dependency graph

```
                        ┌─────────────────────────────────────────────────┐
                        │                   setup                         │
                        │  (wires everything below in the correct order)  │
                        └───────────────────┬─────────────────────────────┘
                                            │
     ┌──────────────┬────────────┬──────────┼──────────────────┬──────────────────┐
     ▼              ▼            ▼          ▼                  ▼                  ▼
 gsfiber/       apmfiber/   sentryfiber/ logfiber/          txctx/            gsredis/
gsfiberv3     apmfiberv3   sentryfiberv3 logfiberv3         txctxv3           gsrueidis
     │              │            │          │                  │                  │
     ▼              ▼            ▼          ▼                  ▼                  ▼
  gscore         apmcore   sentrycore ──► logcore           txcore           gormcache
     │              │  ▲          │          │                  │
     │              └──┴──────────┘          │  (sentrycore → apmcore for trace tags;
     │              │                        │   logcore → sentrycore for the zap hook)
     └──────────────┴────────────────────────┴──────────────────┘
                                    │
                              go-tools (root)
                         gscore.CloserRegistrar,
                          txctx.ContextExtractor, …
```

### Connection points between modules

#### APM + txctx: span parenting

`apmfiber.Middleware()` attaches the APM transaction to the underlying
`*fasthttp.RequestCtx`. `txctx.Middleware()` derives its internal context from
`context.Background()` by default, which breaks span nesting — DB spans created
inside handlers end up as root spans in Kibana instead of children of the
request transaction.

Fix: pass `apmfiber.TxContextExtractor()` to `txctx.Middleware` so it inherits
the request context that already carries the APM transaction:

```go
app.Use(apmfiber.Middleware())   // must be first
app.Use(txctx.Middleware(db, txctx.Config{...}, apmfiber.TxContextExtractor()))
```

This is wired automatically when using the `setup` package.

#### APM + gormautobatch: batch spans

`gormautobatch` can emit an APM span per flush via its `Config.SpanEmitter`
field. `apmcore.BatchSpanEmitter()` returns the right function:

```go
plugin := autobatch.New(autobatch.Config{
    SpanEmitter: apmcore.BatchSpanEmitter(),
})
```

When using `setup.New().WithOTel(ctx).WithAutobatchConfig(cfg)`, this wiring
is automatic — `Build` injects `BatchSpanEmitter` before registering the
plugin.

#### APM + logcore: trace correlation

`logcore.New` wraps the zap core with `apmzap.Core` by default. Any
`logger.Error(...)` call is automatically emitted as an APM error event, and
`logcore.LogCtx(ctx)` appends `trace.id` / `transaction.id` fields to every
log line. No extra setup needed — the correlation is active as long as
`apmcore.SetupOTelSDK` was called before `logcore.New`.

#### APM + httpclient: exit spans + outgoing logs

`httpclient` produces an APM exit span for every call via
`apmcore.TraceFastHTTPCall` internally. It also propagates the active
transaction's `traceparent` header so downstream services appear as children in
the APM trace waterfall.

To add structured logging on top, install the logcore hook:

```go
httpclient.SetHook(logcore.HTTPClientHook())
```

Both concerns are enabled by `setup.New().WithOTel(ctx).WithHTTPClientLogging()`.

#### gscore + everything: graceful shutdown registration order

The shutdown sequence is ordered. Registering in the wrong phase causes
resources to be torn down before dependents have finished:

| Phase | What to register |
|---|---|
| `PhasePreStop` | In-memory queue flushes, worker signals |
| `PhaseDrain` | Fiber app drain (automatic via `gsfiber.RegisterApp`) |
| `PhasePostDrain` | `txcore.RegisterWithManager` — wait for in-flight transactions |
| `PhaseDB` | `mgr.RegisterDB(db)` — close GORM pool |
| `PhasePostDB` | `gsredis`, `gsrueidis`, `apmcore.RegisterWithManager`, `logcore.RegisterGlobalWithManager` |

`setup.Build` follows this order exactly. If you wire manually, register in the
order shown above — particularly, do **not** close Redis before transactions
finish, and do **not** flush the logger before OTel spans are exported.

#### APM + gormcache: cache spans

`apmcore.InstrumentCacher(inner)` wraps any `caches.Cacher` with OTel spans so
cache hits, misses, stores, and invalidations appear in APM traces alongside DB
spans. When using `setup.New().WithOTel(ctx).WithGORMCacheConfig(cfg)`, this
wrapping is automatic — `Build` injects `InstrumentCacher` before registering
the plugin.

#### gsredis / gsrueidis: which one to use

Use `gsredis` for `go-redis/v9` clients and `gsrueidis` for `rueidis` clients.
Both register a timeout-bounded closer at `PhasePostDB`. They are independent —
a service can register both if it uses both clients.

For APM tracing:
- `go-redis/v9`: call `apmcore.InstrumentRedis(client)` after `SetupOTelSDK`.
- `rueidis`: build the client via `rueidisotel.NewClient` (preferred, adds pool
  metrics) or wrap an existing one with `apmcore.InstrumentRueidis(client)`.

`gsredis.InstrumentAndRegister` and `gsrueidis` combine the OTel instrumentation
and shutdown registration in one call.

#### logcore + gscore: logger lifecycle

The zap logger buffers internally. At shutdown, `logcore.RegisterGlobalWithManager`
registers a `PhasePostDB` closer that calls `logger.Sync()` — ensuring buffered
log lines are flushed after OTel spans (which may themselves log) are exported.
The ordering matters: register `apmcore` before `logcore`.

---

## Transactions (`txctx` / `txctxv3`)

---

## Features

- **Lazy transactions** — a DB transaction is only opened when the first write (`Create`/`Update`/`Delete`) occurs in a request. Pure read requests never touch a transaction.
- **Timeout-triggered rollback** — each request gets a configurable context timeout. If it expires before the handler finishes, the transaction is rolled back automatically.
- **`Outside(c)`** — returns a `*gorm.DB` connected to `context.Background()`, completely outside the request transaction. Writes via `Outside` persist even if the main transaction rolls back.
- **`OnRollback(c, fn)`** — registers a compensating callback that runs if the transaction rolls back (timeout, error, or panic). Runs with a fresh context (`CompensationCtx` duration) because the request context is already cancelled.
- **`OnCommit(c, fn)`** — registers a callback that runs only after a successful commit. Useful for the outbox pattern (publish events only after the DB write is confirmed).
- **Panic recovery** — the middleware recovers panics, rolls back the transaction, runs `OnRollback` callbacks, then re-panics so Fiber's `ErrorHandler` can still handle it.
- **Context propagation** — all public functions have both `*fiber.Ctx` and `context.Context` variants, so repository and service layers stay framework-agnostic.

---

## Installation

For Fiber v2:
```bash
go get github.com/adrielcodeco/go-tools/txctx
```

For Fiber v3:
```bash
go get github.com/adrielcodeco/go-tools/txctxv3
```

The v3 adapter has the same surface as v2; just swap `txctx` → `txctxv3` and replace `*fiber.Ctx` with `fiber.Ctx` in your handler signatures.

---

## Public API

```go
// Middleware
txctx.Middleware(db *gorm.DB, cfg txctx.Config) fiber.Handler

// Config
type Config struct {
    Timeout         time.Duration   // request deadline (default: 30s)
    LazyTx          *bool           // open tx only on first write (default: true; use txctx.BoolPtr to set)
    CompensationCtx time.Duration   // timeout for OnRollback callbacks (default: 5s)
    OnCallbackError func(error)     // optional sink for errors from OnCommit/OnRollback callbacks and from rollback/commit
}

// DB access
txctx.DB(c *fiber.Ctx) *gorm.DB
txctx.DBFromCtx(ctx context.Context) *gorm.DB

// Outside-tx access
txctx.Outside(c *fiber.Ctx) *gorm.DB
txctx.OutsideCtx(ctx context.Context) *gorm.DB

// Callbacks
txctx.OnRollback(c *fiber.Ctx, fn func(*gorm.DB) error)
txctx.OnRollbackCtx(ctx context.Context, fn func(*gorm.DB) error)
txctx.OnCommit(c *fiber.Ctx, fn func(*gorm.DB) error)
txctx.OnCommitCtx(ctx context.Context, fn func(*gorm.DB) error)
```

---

## Usage

### 1. Setup

Register the middleware once, globally or on a route group:

```go
app.Use(txctx.Middleware(db, txctx.Config{
    Timeout:         5 * time.Second,
    LazyTx:          txctx.BoolPtr(true),
    CompensationCtx: 3 * time.Second,
}))
```

- `Timeout` — maximum duration allowed for a single request before the context is cancelled and any open transaction is rolled back.
- `LazyTx` — when `true`, `BEGIN` is deferred until the first write operation. Read-only requests skip transactions entirely.
- `CompensationCtx` — timeout granted to `OnRollback` callbacks. Because the original request context is already cancelled at rollback time, each callback receives a fresh context with this duration.

---

### 2. Read-only handler

When `LazyTx` is `true` and no write happens, `DB(c)` returns a plain `*gorm.DB` without ever opening a transaction.

```go
func getUser(c *fiber.Ctx) error {
    var u User
    if err := txctx.DB(c).First(&u, c.Params("id")).Error; err != nil {
        return err
    }
    return c.JSON(u)
}
```

---

### 3. Simple write (lazy tx)

The first call to a write operation (`Create`, `Save`, `Update`, `Delete`) transparently triggers `BEGIN`.

```go
func createUser(c *fiber.Ctx) error {
    var u User
    c.BodyParser(&u)
    // First write: middleware transparently opens BEGIN here
    if err := txctx.DB(c).Create(&u).Error; err != nil {
        return err
    }
    return c.JSON(u) // handler returns nil → COMMIT
}
```

---

### 4. Multiple writes in the same transaction

All calls to `DB(c)` within the same request share the same underlying transaction.

```go
func createOrder(c *fiber.Ctx) error {
    db := txctx.DB(c)
    user := User{Email: "a@b.com"}
    db.Create(&user)                                        // opens tx
    db.Create(&Order{UserID: user.ID, Total: 100})          // same tx
    db.Model(&user).Update("Name", "updated")               // same tx
    return c.JSON(user)                                     // COMMIT — all three writes atomic
}
```

---

### 5. `Outside` — write that survives rollback

`Outside(c)` returns a `*gorm.DB` backed by `context.Background()`, completely independent of the request transaction. Writes via `Outside` are committed immediately and are not affected by a subsequent rollback of the main transaction.

```go
func signupWithAudit(c *fiber.Ctx) error {
    var u User
    c.BodyParser(&u)

    // Persists regardless of what happens to the main tx
    txctx.Outside(c).Create(&AuditLog{Action: "signup_attempt", Payload: u.Email})

    if err := txctx.DB(c).Create(&u).Error; err != nil {
        return err // rollback of User, but AuditLog stays
    }
    return c.JSON(u)
}
```

---

### 6. `OnRollback` — compensating transaction

`OnRollback` registers a function that runs only if the transaction is rolled back (due to a handler error, timeout, or panic). The callback receives a `*gorm.DB` with a fresh context whose deadline is `CompensationCtx`.

```go
func paymentHandler(c *fiber.Ctx) error {
    var u User
    c.BodyParser(&u)
    txctx.DB(c).Create(&u)

    txctx.OnRollback(c, func(bg *gorm.DB) error {
        return bg.Create(&FailedSignup{Email: u.Email, Error: "rolled back"}).Error
    })

    if err := chargeExternal(c.UserContext(), u.ID); err != nil {
        return err // triggers rollback → OnRollback callback fires
    }
    return c.JSON(u)
}
```

---

### 7. `OnCommit` — outbox / post-commit event

`OnCommit` registers a function that runs only after a successful commit. This is the recommended pattern for publishing domain events (outbox pattern): the event is only dispatched once the DB write is durably confirmed.

```go
func createOrder(c *fiber.Ctx) error {
    var o Order
    c.BodyParser(&o)
    txctx.DB(c).Create(&o)

    txctx.OnCommit(c, func(bg *gorm.DB) error {
        return publishEvent("order.created", o.ID)
    })
    return c.JSON(o)
}
```

---

### 8. Handler returns error → rollback

Any non-nil error returned by the handler causes the middleware to roll back the active transaction before passing the error to Fiber's error handler.

```go
func manualRollback(c *fiber.Ctx) error {
    var u User
    txctx.DB(c).Create(&u)
    if u.Email == "" {
        return errors.New("email required") // rollback triggered
    }
    return c.JSON(u)
}
```

---

### 9. Panic → rollback + re-panic

The middleware recovers from panics, rolls back the transaction (running any registered `OnRollback` callbacks), and then re-panics so that Fiber's `ErrorHandler` or `RecoverHandler` can process it normally.

```go
func panicHandler(c *fiber.Ctx) error {
    txctx.DB(c).Create(&User{Email: "boom"})
    txctx.OnRollback(c, func(bg *gorm.DB) error {
        return bg.Create(&AuditLog{Action: "panicked"}).Error
    })
    panic("something went very wrong") // middleware: recover → rollback → re-panic
}
```

---

### 10. Layered architecture

The `*Ctx` variants (`DBFromCtx`, `OutsideCtx`, `OnRollbackCtx`, `OnCommitCtx`) accept a `context.Context` instead of a `*fiber.Ctx`. This allows repository and service layers to remain completely framework-agnostic while still participating in the request-scoped transaction.

```go
// handler — Fiber layer
func createUserHandler(c *fiber.Ctx) error {
    var u User
    c.BodyParser(&u)
    if err := userService.Create(c.UserContext(), &u); err != nil {
        return err
    }
    return c.JSON(u)
}

// service — no Fiber dependency
func (s *UserService) Create(ctx context.Context, u *User) error {
    if err := s.repo.Insert(ctx, u); err != nil {
        return err
    }
    txctx.OnCommitCtx(ctx, func(db *gorm.DB) error {
        return s.events.Publish("user.created", u.ID)
    })
    return nil
}

// repository — no Fiber dependency
func (r *UserRepository) Insert(ctx context.Context, u *User) error {
    return txctx.DBFromCtx(ctx).Create(u).Error
}
```

---

## Commit / Rollback Decision Table

| Situation | Result |
|---|---|
| Handler returns `nil` | COMMIT → `OnCommit` callbacks run |
| Handler returns `error` | ROLLBACK → `OnRollback` callbacks run |
| Request context timeout | ROLLBACK → `OnRollback` callbacks run |
| Panic in handler | ROLLBACK → `OnRollback` callbacks run → re-panic |
| `tx.Commit()` itself fails | ROLLBACK → `OnRollback` callbacks run → commit error returned |
| `OnCommit` callback fails (after successful commit) | Tx stays committed; error surfaced via `OnCallbackError` + returned to Fiber. `OnRollback` does **not** fire. |
| Write via `Outside` | Always persists, independent of tx. Context cancellation is decoupled but values (request-id, tracing) are preserved. |

### Concurrency notes

The request-scoped `*gorm.DB` is safe for sequential use within the handler.
If you spawn goroutines from the handler, do **not** use `DB(c)` from them
after the handler returns — the middleware will commit/rollback as soon as
the handler returns, and the underlying `*sql.Tx` becomes invalid. Use
`Outside(c)` for fire-and-forget work, or wait for the goroutine before
returning from the handler.

---

## Propagating cancellation to outbound calls

The middleware wraps `c.UserContext()` with the configured `Timeout`. **Any
outbound call (HTTP, gRPC, Redis, message broker, etc.) that receives this
context will be cancelled automatically when the request times out, errors,
or the client disconnects** — Go's standard libraries already implement this:
`net/http` aborts the in-flight TCP request, `database/sql` interrupts the
query, gRPC closes the stream, and so on.

For this to work you must **thread the context through every outbound call**.
The package can't do this for you — it would require wrapping every client
type in the ecosystem. The discipline is:

```go
func chargeExternal(c *fiber.Ctx, userID uint) error {
    // ✅ Pass the request context — cancels on Fiber timeout/error/panic.
    req, err := http.NewRequestWithContext(c.UserContext(),
        http.MethodPost, "https://payments.example/charge", body)
    if err != nil {
        return err
    }
    resp, err := http.DefaultClient.Do(req)
    // ...
}

func chargeExternalBAD(userID uint) error {
    // ❌ No context: the call will keep running after the request times out,
    //    burning a goroutine and a connection until the remote replies.
    resp, err := http.Post("https://payments.example/charge", "...", body)
    // ...
}
```

The same applies to gRPC (`grpc.Invoke(ctx, ...)`), Redis
(`rdb.Get(ctx, ...)`), AWS SDK v2 (`client.GetItem(ctx, ...)`), and any other
client that accepts a `context.Context` as its first argument.

**Service / repository layers:** use the `*Ctx` variants
(`DBFromCtx`, `OutsideCtx`, `OnRollbackCtx`, `OnCommitCtx`) so the same
`context.Context` flows through the whole call chain — DB, HTTP, gRPC, queue
publishes, etc. — and a single cancellation point unwinds everything.

**When you need to escape cancellation** (e.g. publishing a "request-failed"
event to a queue from `OnRollback` callbacks), `Outside(c)` already gives you
a context decoupled from the request cancellation while preserving values
like request-id and trace headers — use the same pattern for outbound HTTP
in that scenario:

```go
txctx.OnRollback(c, func(_ *gorm.DB) error {
    // Need a fresh ctx because c.UserContext() is already cancelled here.
    ctx, cancel := context.WithTimeout(
        context.WithoutCancel(c.UserContext()), 3*time.Second)
    defer cancel()
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, alertURL, body)
    _, _ = http.DefaultClient.Do(req)
    return nil
})
```

---

## Graceful Shutdown (`gsfiber` / `gsfiberv3`)

A coordinator for the full shutdown sequence of a Fiber + GORM service:
**drain in-flight HTTP requests**, **cancel outbound calls**, **flush
application state**, **close the database pool**, all bounded by per-phase
and global timeouts. Designed around the Kubernetes pod lifecycle.

### Features

- **Phased sequence** — `PreStop → Drain → PostDrain → DB → PostDB`, so each
  resource is cleaned up at the right moment (e.g. flush outbox *before*
  closing the DB; close Redis *after*).
- **Ordered hooks** — each phase runs registered hooks sorted by `Priority`;
  a failing hook is logged but does not stop the sequence.
- **`RootContext()`** — a `context.Context` that is cancelled the moment
  shutdown begins. Derive outbound HTTP/gRPC/queue calls from it and they
  abort cleanly on SIGTERM.
- **Readiness flip** — `IsReady()` (and the provided `ReadinessHandler`)
  returns `200` while serving and `503` once shutdown begins, so kube-proxy
  can remove the pod from service endpoints before any request is dropped.
- **Configurable timeouts** — independent `PreStopDelay`, `DrainTimeout`,
  `HookTimeout`, `DBCloseTimeout`, plus a global `ForceKillAfter` that
  `os.Exit(1)`s if the whole sequence overshoots
  `terminationGracePeriodSeconds`.
- **Configurable signals** — defaults to `SIGINT` + `SIGTERM`, override via
  `Config.Signals`.
- **Structured logging** — every phase logs begin/end with duration; plug
  any logger that implements the 3-method `Logger` interface.
- **GORM-aware** — closes the underlying `*sql.DB` of each registered
  `*gorm.DB` with a deadline (avoids hanging on a stuck pool).
- **Concurrent drain** — multiple `*fiber.App` instances (or anything
  implementing `Shutdowner`) are drained in parallel under a shared
  deadline.

### Installation

For Fiber v2:
```bash
go get github.com/adrielcodeco/go-tools/gsfiber
```

For Fiber v3:
```bash
go get github.com/adrielcodeco/go-tools/gsfiberv3
```

The two adapters share an engine (`gscore`); the public surface is
identical apart from `*fiber.App` vs `fiber.App` and `*fiber.Ctx` vs
`fiber.Ctx` in the readiness handler.

### Public API

```go
// Manager
gsfiber.New(cfg gsfiber.Config) *gsfiber.Manager

// Registration
gsfiber.RegisterApp(m *Manager, app *fiber.App)    // one or more
mgr.RegisterDB(db *gorm.DB)                        // one or more
mgr.RegisterCloser(name string, phase Phase,       // any client with a Close() method;
    timeout time.Duration,                         // see also the gsrueidis adapter
    fn func(ctx context.Context) error)            // for rueidis.Client.
mgr.AddHook(gsfiber.Hook{Name, Phase, Priority, Run})

// Lifecycle
mgr.RootContext() context.Context                  // cancelled on shutdown
mgr.IsReady() bool                                 // false once shutdown began
mgr.Trigger()                                      // start sequence programmatically
mgr.ListenAndWait() error                          // block on signals + run
mgr.Wait() error                                   // block until sequence done

// Readiness probe
gsfiber.ReadinessHandler(mgr) fiber.Handler

// Phases (re-exported on the adapter package)
gsfiber.PhasePreStop
gsfiber.PhaseDrain
gsfiber.PhasePostDrain
gsfiber.PhaseDB
gsfiber.PhasePostDB

// Config
type Config struct {
    Signals        []os.Signal     // default: SIGINT, SIGTERM
    PreStopDelay   time.Duration   // wait before any phase runs (default: 0)
    DrainTimeout   time.Duration   // bound on HTTP drain (default: 25s)
    HookTimeout    time.Duration   // bound per phase (default: 10s)
    DBCloseTimeout time.Duration   // bound on each gorm.DB close (default: 5s)
    ForceKillAfter time.Duration   // global ceiling, os.Exit(1) (default: 60s)
    Logger         gscore.Logger   // structured logger; nil = silent
    OnHookError    func(name string, phase gscore.Phase, err error)
}
```

### Phases

| Phase | Purpose |
|---|---|
| `PhasePreStop` | Runs first, while the server is still serving. Use for actions that need the HTTP layer alive (signal in-flight workers, flush in-memory queue). |
| `PhaseDrain` | Drains all registered Fiber apps concurrently with `DrainTimeout`. |
| `PhasePostDrain` | Runs after HTTP is fully drained, before DB close. Best place for outbound-call cleanups, worker pool waits, etc. |
| `PhaseDB` | Closes each registered `*gorm.DB`'s underlying `*sql.DB` with `DBCloseTimeout`. |
| `PhasePostDB` | Last phase. Use for resources that do not depend on the DB: Kafka producers, log flushers, metric exporters. |

### Usage

#### 1. Minimum setup

> For production services, prefer the `setup` package (see [Quick Setup](#quick-setup-setup))
> which handles registration order automatically. This example shows the
> manual wiring for when you need full control.

```go
func main() {
    db := openGORM()
    app := fiber.New()
    app.Use(txctx.Middleware(db, txctx.Config{Timeout: 5 * time.Second}))
    registerRoutes(app)

    mgr := gsfiber.New(gsfiber.Config{
        PreStopDelay:   5 * time.Second,  // give kube-proxy time to drop the endpoint
        DrainTimeout:   25 * time.Second,
        DBCloseTimeout: 5 * time.Second,
        ForceKillAfter: 55 * time.Second, // < terminationGracePeriodSeconds
    })
    gsfiber.RegisterApp(mgr, app)
    mgr.RegisterDB(db)

    // Register all three Kubernetes probe endpoints.
    app.Get("/healthz/live",    gsfiber.LivenessHandler())        // always 200
    app.Get("/healthz/ready",   gsfiber.ReadinessHandler(mgr))   // 503 on shutdown
    app.Get("/healthz/startup", gsfiber.StartupHandler(mgr))     // 503 until MarkStarted

    // Boot sequence: complete migrations before accepting traffic.
    // The startup probe returns 503 until MarkStarted() is called.
    if err := runMigrations(db); err != nil {
        log.Fatal(err)
    }
    mgr.MarkStarted()

    // Background workers must use mgr.RootContext() so they are cancelled
    // the instant SIGTERM is received, before the HTTP drain begins.
    go pollWorker(mgr.RootContext())

    // ListenAndTrigger starts the server and calls mgr.Trigger() if Listen
    // fails, so the process never hangs waiting for a signal that won't come.
    gsfiber.ListenAndTrigger(app, mgr, ":8080")

    if err := mgr.ListenAndWait(); err != nil {
        log.Fatal(err)
    }
}
```

#### 2. Cancel outbound calls on shutdown

Derive any long-running outbound call from `mgr.RootContext()`. It is
cancelled the moment SIGTERM is observed, so the call aborts cleanly
during the drain phase.

```go
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-mgr.RootContext().Done():
            return
        case <-ticker.C:
            req, _ := http.NewRequestWithContext(mgr.RootContext(),
                http.MethodGet, "https://api.example/poll", nil)
            _, _ = http.DefaultClient.Do(req)
        }
    }
}()
```

For per-request outbound calls inside a handler, keep using
`c.UserContext()` — `txctx` already wires its cancellation.

#### 3. Ordered hooks across phases

```go
mgr.AddHook(gsfiber.Hook{
    Name:     "outbox-flush",
    Phase:    gsfiber.PhasePreStop, // before we stop accepting requests
    Priority: 0,
    Run: func(ctx context.Context) error {
        return outbox.FlushAll(ctx)
    },
})

mgr.AddHook(gsfiber.Hook{
    Name:     "kafka-close",
    Phase:    gsfiber.PhasePostDB,  // after DB is closed
    Priority: 10,
    Run: func(ctx context.Context) error {
        return kafkaProducer.Close()
    },
})

mgr.AddHook(gsfiber.Hook{
    Name:     "redis-close",
    Phase:    gsfiber.PhasePostDB,
    Priority: 0, // runs before kafka-close (lower priority first)
    Run: func(ctx context.Context) error {
        return redisClient.Close()
    },
})
```

Lower `Priority` runs first within the same phase; equal priorities run in
registration order.

#### 4. Custom logger

Any type that satisfies the three-method `gscore.Logger` interface works
(slog, zap, zerolog, logrus, etc.).

```go
type slogAdapter struct{ l *slog.Logger }

func (s slogAdapter) Info(msg string, kv ...any)  { s.l.Info(msg, kv...) }
func (s slogAdapter) Warn(msg string, kv ...any)  { s.l.Warn(msg, kv...) }
func (s slogAdapter) Error(msg string, kv ...any) { s.l.Error(msg, kv...) }

mgr := gsfiber.New(gsfiber.Config{
    Logger: slogAdapter{l: slog.Default()},
})
```

#### 5. Triggering shutdown programmatically

`mgr.Trigger()` starts the sequence from anywhere — useful for fatal
errors caught outside the HTTP layer (e.g. a background worker losing a
critical connection).

```go
if err := kafkaConsumer.Run(mgr.RootContext()); err != nil && !errors.Is(err, context.Canceled) {
    log.Printf("consumer fatal: %v", err)
    mgr.Trigger()
}
```

`Trigger` is idempotent — the sequence runs exactly once regardless of
how many times it is called or whether a signal also arrives.

### Kubernetes integration

#### Health probe handlers

The package ships three handlers, one per Kubernetes probe type:

| Handler | Probe | Endpoint | Behaviour |
|---|---|---|---|
| `LivenessHandler()` | `livenessProbe` | `/healthz/live` | Always `200`. If the process responds, it is alive. **No external dependencies.** |
| `ReadinessHandler(mgr)` | `readinessProbe` | `/healthz/ready` | `200` while ready, `503` the instant shutdown begins. Drives kube-proxy endpoint removal. |
| `StartupHandler(mgr)` | `startupProbe` | `/healthz/startup` | `503` until `mgr.MarkStarted()` is called, then `200`. Protects slow-boot pods (migrations, cache warm-up). |

**Via `setup.Builder` (recommended):** `WithHealthProbesV2` / `WithHealthProbesV3`
registers all three routes automatically during `Build`:

```go
mgr := gscore.New(gscore.Config{...})
_, err := setup.New().
    WithFiberV2(app).
    WithHealthProbesV2(setup.HealthProbesConfig{}).  // zero value = default paths
    WithGORM(db).
    Build(mgr)

// Boot sequence: WithStartupFn runs migrations and calls MarkStarted
// automatically on success — startup probe flips to 200 after Build returns.
// Build itself returns an error if migrations fail, so the process exits
// cleanly before ever calling Listen.
gsfiber.ListenAndTrigger(app, mgr, ":8080")
mgr.ListenAndWait()
```

**Manual registration** (without `setup`):

```go
app.Get("/healthz/live",    gsfiber.LivenessHandler())
app.Get("/healthz/ready",   gsfiber.ReadinessHandler(mgr))
app.Get("/healthz/startup", gsfiber.StartupHandler(mgr))

if err := runMigrations(db); err != nil {
    log.Fatal(err)
}
mgr.MarkStarted() // ← startup probe flips to 200

gsfiber.ListenAndTrigger(app, mgr, ":8080")
mgr.ListenAndWait()
```

> **Rule of thumb:** never check databases, caches, or any external
> dependency inside `LivenessHandler`. A slow dependency would cause
> liveness to fail → Kubernetes restarts the pod → cascading restarts
> across the fleet.

---

#### Recommended Kubernetes manifest

```yaml
spec:
  terminationGracePeriodSeconds: 60   # must be > ForceKillAfter
  containers:
  - name: api
    # --- startup probe -------------------------------------------
    # Kubernetes suspends liveness + readiness until this passes once.
    # Gives slow-boot pods (migrations, warm-up) time to initialize
    # without being killed by a failing liveness probe.
    startupProbe:
      httpGet:
        path: /healthz/startup
        port: 8080
      # Allow up to 5 min for boot: periodSeconds(10) × failureThreshold(30)
      periodSeconds: 10
      failureThreshold: 30
      timeoutSeconds: 2

    # --- liveness probe ------------------------------------------
    # Restarts the pod if the process stops responding entirely.
    # Keep it cheap: no DB, no cache, no external calls.
    livenessProbe:
      httpGet:
        path: /healthz/live
        port: 8080
      initialDelaySeconds: 0   # startupProbe already guards the boot window
      periodSeconds: 10
      failureThreshold: 3
      timeoutSeconds: 2

    # --- readiness probe -----------------------------------------
    # Removes the pod from service endpoints during shutdown.
    # Tight period + low threshold so kube-proxy reacts quickly.
    readinessProbe:
      httpGet:
        path: /healthz/ready
        port: 8080
      periodSeconds: 2
      failureThreshold: 1
      timeoutSeconds: 1

    lifecycle:
      preStop:
        exec:
          # Belt-and-suspenders sleep in case SIGTERM races with
          # kube-proxy propagation. The Manager's PreStopDelay
          # provides the same guarantee in-process.
          command: ["sleep", "5"]
```

**Timing relationships that must hold:**

```
startupProbe:  periodSeconds × failureThreshold  ≥  expected max boot time
livenessProbe: does NOT query DB / cache / external services
ForceKillAfter  <  terminationGracePeriodSeconds  (e.g. 55s < 60s)
PreStopDelay    ≥  readinessProbe.periodSeconds   (e.g. 5s ≥ 2s)
```

---

#### Shutdown sequence on `kubectl delete pod`

1. Kubernetes sends `SIGTERM` and starts the `preStop` hook (in parallel).
2. The Manager observes the signal → flips readiness to `503` → starts
   `PreStopDelay`.
3. kube-proxy sees the failing readiness probe and removes the pod from
   service endpoints → no new requests arrive.
4. `PreStopDelay` elapses → hooks run → HTTP drain → DB close → post-DB
   hooks.
5. Process exits cleanly, well before
   `terminationGracePeriodSeconds`.

Keep `ForceKillAfter` strictly **less than** `terminationGracePeriodSeconds`
so the Manager's own ceiling fires first, with logs you can read, instead
of an abrupt `SIGKILL` from the kubelet.

---

## Rueidis (`gsrueidis`)

[Rueidis](https://github.com/redis/rueidis) clients expose a synchronous
`Close()` with no context and no error return — it waits for every
in-flight pipelined command and PubSub subscriber to settle. In a
misbehaving setup that wait can be indefinite. The `gsrueidis` submodule
runs `Close()` in a goroutine bounded by the closer's timeout so a
wedged Redis client cannot stall the rest of the shutdown sequence.

```bash
go get github.com/adrielcodeco/go-tools/gsrueidis
```

```go
gsrueidis.RegisterRueidis(mgr, "redis-cache", client,
    gscore.PhasePostDB, 5*time.Second)
```

- `PhasePostDB` is the recommended phase: any `txctx.OnCommit` callback
  that publishes to Redis after a DB commit must have completed during
  the drain, so it's safe to tear the client down.
- `timeout=0` falls back to `gsrueidis.DefaultTimeout` (5s) — Redis
  hangs deserve a dedicated default rather than `Config.HookTimeout`.
- If `Close()` does not return within the timeout, the registered closer
  returns `gsrueidis.ErrCloseTimedOut`. The manager logs it (and calls
  `OnHookError` if set) and continues to the next closer.

For non-rueidis clients, the underlying `gscore.RegisterCloser` is
public — use it directly to register any cleanup function that has a
similar "blocking Close()" shape (Kafka, gRPC pools, etc.).

### Tracing rueidis with Elastic APM

There is no dedicated `apmrueidis` package — rueidis already publishes
an official OTel adapter
([`rueidisotel`](https://pkg.go.dev/github.com/redis/rueidis/rueidisotel)),
and `apmcore.SetupOTelSDK` plants the APM agent as the OTel global
`TracerProvider`/`MeterProvider`. Construct the client via `rueidisotel`
and spans + metrics flow through APM automatically:

```go
import (
    "github.com/redis/rueidis"
    "github.com/redis/rueidis/rueidisotel"

    "github.com/adrielcodeco/go-tools/apmcore"
)

func main() {
    shutdown, _ := apmcore.SetupOTelSDK(context.Background())
    defer shutdown(context.Background())

    client, err := rueidisotel.NewClient(rueidis.ClientOption{
        InitAddress: []string{"localhost:6379"},
    })
    // ... use client as usual; spans appear in Kibana → APM → Services.
}
```

Notes:

- `rueidisotel.NewClient` is the only way to get **pool-level metrics**
  (it has to install its own `DialFn`). `rueidisotel.WithClient(existing)`
  wraps an already-built client but only adds tracing — no pool metrics.
- `apmcore.InstrumentRedis(client)` is for `redis/go-redis/v9` clients
  (it uses `redisotel`). It does **not** apply to rueidis.
- The `SetupOTelSDK` call must happen before `rueidisotel.NewClient`, so
  the client picks up the APM-backed `TracerProvider`/`MeterProvider`.

---

## Redis graceful shutdown (`gsredis`)

The `gsredis` package is the [go-redis/v9](https://github.com/redis/go-redis)
`UniversalClient` analog of `gsrueidis`. It registers `client.Close()` as a
context-aware closer on the Manager, bounded by a per-client timeout so a slow
Redis pool cannot stall the shutdown sequence.

```bash
go get github.com/adrielcodeco/go-tools/gsredis
```

```go
// Register for graceful shutdown (Close bounded by timeout).
gsredis.Register(client, mgr, gscore.PhasePostDB, 5*time.Second)

// Register + instrument with OTel tracing and metrics in one call.
if err := gsredis.InstrumentAndRegister(client, mgr, gscore.PhasePostDB, 5*time.Second); err != nil {
    // instrumentation failed; tracing is best-effort — client is still registered.
    log.Printf("redis instrumentation: %v", err)
}
```

- `timeout=0` falls back to `gsredis.DefaultTimeout` (5s).
- If `Close()` does not return within the timeout the registered closer
  returns `gsredis.ErrCloseTimedOut`. The Manager logs it and continues.
- `InstrumentAndRegister` calls `apmcore.InstrumentRedis(client)` before
  registering the closer. Call `apmcore.SetupOTelSDK` first so the client
  picks up the APM-backed `TracerProvider`.
- `PhasePostDB` is recommended for the same reason as `gsrueidis`: any
  `txctx.OnCommit` callbacks that write to Redis will have completed by then.

---

## GORM Auto-batch (`gormautobatch`)

A GORM plugin that transparently switches between individual and batched
database operations based on measured P95 write latency. When latency exceeds
the configured threshold, `Create`/`Updates`/`Delete` calls are buffered and
flushed as a single transaction, reducing round-trips under load. When latency
drops back, operations pass through normally with no overhead.

```bash
go get github.com/adrielcodeco/go-tools/gormautobatch
```

```go
import autobatch "github.com/adrielcodeco/go-tools/gormautobatch"

threshold := 50 * time.Millisecond
p := autobatch.New(autobatch.Config{
    LatencyThreshold: &threshold,            // nil = disabled; 0 = always batch; >0 = adaptive
    FlushTimeout:     10 * time.Millisecond, // flush batch after 10ms idle
    MaxBatchSize:     100,                   // or when 100 ops are buffered
    WindowDuration:   30 * time.Second,      // P95 measured over last 30s
})
if err := db.Use(p); err != nil {
    log.Fatal(err)
}
defer p.Close() // drain in-flight batches before exit
```

Regular GORM calls are unchanged — the plugin decides whether to batch
transparently. `db.Create(&u)`, `db.Model(&u).Updates(&payload)`, and
`db.Delete(&r)` all participate. `Find`/`First` are never buffered.

### Batch semantics

All operations in a batch run inside a single transaction. Each individual
operation is wrapped in its own `SAVEPOINT`, so a per-op failure (e.g. a
unique-constraint violation) is isolated: only the failing caller sees the
error, and the rest of the batch still commits. Callers block synchronously
until their batch is flushed — from the caller's perspective it looks like a
normal GORM call.

Operations inside `db.Transaction(...)` or `db.Begin()` are never batched —
they run inline on the user's transaction to preserve atomicity.

### Graceful shutdown integration

Register the plugin with the Manager so in-flight batches are drained before
the DB pool closes:

```go
autobatch.RegisterWithManager(p, mgr, int(gscore.PhasePostDrain), 30*time.Second)
```

Or let `setup.New().WithAutobatchConfig(cfg).Build(mgr)` handle this
automatically.

### APM tracing

When `WithOTel` is set in the `setup` builder, batched flush transactions
automatically appear as APM spans via `apmcore.BatchSpanEmitter()`. To wire
this manually:

```go
cfg.SpanEmitter = apmcore.BatchSpanEmitter()
p := autobatch.New(cfg)
```

### DBResolver compatibility

DBResolver must be registered before autobatch. Multi-source (sharded) primary
configurations are not supported — batched writes are routed to the pool
selected at `BEGIN` time. Single-primary + read-replica setups are fully
supported.

---

## HTTP client (`httpclient`)

A small fasthttp-based client wrapper with generics, sonic JSON,
configurable retry, and APM tracing already wired through `apmcore`.
Each call produces an APM exit span (via `apmcore.TraceFastHTTPCall`)
and propagates the active transaction's `traceparent` header.

```bash
go get github.com/adrielcodeco/go-tools/httpclient
```

```go
type charge struct {
    ID     string `json:"id"`
    Amount int    `json:"amount"`
}

out, err := httpclient.POST[charge](ctx, httpclient.RequestOptions{
    URL:     "https://api.example.com/charge",
    Headers: httpclient.JSONHeaders(),
    Data:    map[string]any{"amount": 100},
    Retry: httpclient.RetryPolicy{
        MaxAttempts:    3,
        InitialBackoff: 100 * time.Millisecond,
    },
})
```

### Configuration

```go
// Override the underlying *fasthttp.Client (default has 30s read/write).
httpclient.UseClient(&fasthttp.Client{ReadTimeout: 10 * time.Second})

// Install a structured-log hook called once per attempt — even retries.
httpclient.SetHook(func(r httpclient.Record) {
    logger.LogCtx(r.Ctx).Info("← outgoing",
        zap.String("method", r.Method),
        zap.String("url", r.URL),
        zap.Int("status", r.Status),
        zap.Duration("rt", r.ResponseTime),
        zap.Int("attempt", r.Attempt),
        zap.Error(r.Err),
    )
})
```

### Error handling

- Transport errors (DNS, connection refused, timeouts) are returned as-is.
- HTTP status outside `[200, 300)` is returned as a `*httpclient.StatusError`
  with `StatusCode` and the raw response `Body` preserved — callers can
  inspect or decode partial responses even on failure.
- `Request[O]` / `GET[O]` / etc. attempt to decode the body into `*O`
  via sonic regardless of error; the returned error is the original
  call error so retry logic still works.

### Retry

`RetryPolicy.ShouldRetry` defaults to retrying transport errors and 5xx
responses. Override for app-specific semantics (e.g. retry on 429, but
not 401).

---

## Structured logging (`logcore` / `logfiber` / `logfiberv3`)

A zap-based logger pre-wired for APM, plus middlewares that emit a
consistent `incoming` / `outgoing` schema across the request lifecycle.

```bash
go get github.com/adrielcodeco/go-tools/logcore
go get github.com/adrielcodeco/go-tools/logfiber       # Fiber v2
# or
go get github.com/adrielcodeco/go-tools/logfiberv3     # Fiber v3
```

### Bootstrap

```go
l, _ := logcore.New(logcore.Options{
    Service:      "ledger",
    Version:      "1.2.3",
    Environment:  "production",
    RedactFields: true, // mask sensitive fields on every log call (recommended)
})
logcore.SetGlobal(l)

// Outgoing logs for httpclient (request/response secrets masked by default).
httpclient.SetHook(logcore.HTTPClientHook())
```

`logcore.New` wraps the zap core with `apmzap.Core` by default — any
`logger.Error(...)` or `logger.Fatal(...)` call is auto-emitted as an
APM error event in Kibana → APM → Errors. Disable with
`Options{DisableAPMCore: true}` in tests.

### Fiber middleware (incoming)

```go
app.Use(apmfiber.Middleware())          // first, so transactions exist
app.Use(logfiber.Middleware(logfiber.Config{
    // SkipPaths defaults to ["/live", "/ready", "/health"]
}))
```

Every request produces one log line with the schema:

```json
{
  "msg": "→ incoming → [POST] /charge - 200",
  "trace.id": "…",
  "transaction.id": "…",
  "incoming": {
    "req":  { "params": …, "queryString": …, "headers": …, "body": … },
    "res":  { "headers": …, "body": …, "statusCode": "200" },
    "responseTime": "12.4ms"
  }
}
```

The `httpclient.SetHook(logcore.HTTPClientHook())` emits the **same
shape** under the `outgoing` key, so Kibana queries like
`outgoing.res.body.id` match outbound calls and `incoming.req.body.id`
match inbound — without separate dashboards per direction.

### Secret redaction

Sensitive headers and body fields are masked **by default** before they
reach the encoder, so credentials and PII never land in Kibana in clear
text. There are two complementary layers:

**1. Schema-level (request/response logs).** The Fiber middlewares and the
`httpclient` hook run their `incoming` / `outgoing` payloads through a
`logcore.Redactor` before logging. The default policy
(`logcore.DefaultRedactor()`) masks a conservative-but-broad set of keys
(case-insensitive) with the constant `logcore.RedactMask` (`"[REDACTED]"`).

**Default exact keys** (`logcore.DefaultSensitiveKeys`) — matched case-insensitively:

| Group | Keys |
|---|---|
| Auth / session headers | `authorization`, `proxy-authorization`, `cookie`, `set-cookie`, `x-api-key`, `x-auth-token`, `x-access-token`, `x-csrf-token`, `x-xsrf-token`, `x-amz-security-token` |
| Credentials / tokens | `password`, `passwd`, `pwd`, `secret`, `client_secret`, `clientsecret`, `token`, `access_token`, `accesstoken`, `refresh_token`, `refreshtoken`, `id_token`, `idtoken`, `api_key`, `apikey`, `private_key`, `privatekey` |
| Payments / PII | `card`, `card_number`, `cardnumber`, `pan`, `cvv`, `cvc`, `cvv2`, `security_code`, `securitycode`, `ssn`, `taxid`, `tax_id` |

**Default substring patterns** (`logcore.DefaultSensitivePatterns`) — any key
*containing* one of these (case-insensitive) is masked, catching fields like
`user_password` or `stripeSecretKey`:

`password`, `secret`, `passwd`, `authorization`

A pattern matches anywhere in the key, so a single entry covers a whole family
of field names without listing each one. For example, the default `secret`
pattern matches all of these:

| Field name | Masked? | Why |
|---|---|---|
| `secret` | ✅ | exact + contains `secret` |
| `client_secret` | ✅ | contains `secret` |
| `stripeSecretKey` | ✅ | contains `secret` (case-insensitive) |
| `SECRET_TOKEN` | ✅ | contains `secret` (case-insensitive) |
| `secrets` | ✅ | contains `secret` |
| `secretariat` | ⚠️ | contains `secret` — a false positive; see below |

Nested maps and arrays in the decoded JSON body are walked recursively, and
`Bearer <token>` sequences inside error messages are scrubbed too. The
caller's data is never mutated — redaction always returns a copy.

```json
{
  "incoming": {
    "req": {
      "headers": { "authorization": "[REDACTED]", "content-type": "application/json" },
      "body":    { "amount": 100, "card": "[REDACTED]", "password": "[REDACTED]" }
    }
  }
}
```

Customize or disable per middleware:

```go
// Custom policy (defaults + extra keys):
red := logcore.NewRedactor(logcore.RedactorOptions{
    Extra: []string{"account_balance", "x-internal-token"},
})
app.Use(logfiber.Middleware(logfiber.Config{Redactor: red}))

// Custom policy for the httpclient hook:
httpclient.SetHook(logcore.HookForRedacting(nil, red))

// Opt out entirely (log verbatim — only when payloads are known safe):
app.Use(logfiber.Middleware(logfiber.Config{DisableRedaction: true}))
```

#### Overriding the defaults

Each `RedactorOptions` field composes differently, so you can extend, trim, or
replace the default policy:

| Field | `nil` (unset) | `[]string{}` (empty) | with values |
|---|---|---|---|
| `Keys` | uses `DefaultSensitiveKeys` | **disables** exact matching | **replaces** the default exact set |
| `Patterns` | uses `DefaultSensitivePatterns` | **disables** substring matching | **replaces** the default patterns |
| `Extra` | — | — | **adds** to the resolved exact set |
| `RemoveKeys` | — | — | **drops** keys from the resolved exact set |

```go
// Add to the defaults:
logcore.NewRedactor(logcore.RedactorOptions{
    Extra: []string{"account_balance", "x-internal-token"},
})

// Drop a default key (keep everything else):
logcore.NewRedactor(logcore.RedactorOptions{
    RemoveKeys: []string{"cookie"},
})

// Replace the whole policy (defaults ignored):
logcore.NewRedactor(logcore.RedactorOptions{
    Keys:     []string{"authorization", "x-my-token"},
    Patterns: []string{}, // also turn substring matching off
})
```

`Keys` / `Extra` / `RemoveKeys` are applied in that order. Note that
`RemoveKeys` only affects *exact* matching — a key also caught by a substring
pattern (e.g. `client_secret` matches the `secret` pattern) stays masked unless
you override `Patterns` too.

#### Pattern matching (substring)

`Patterns` is the substring counterpart to `Keys`: rather than listing every
exact field name, you list a token and **any key that contains it** is masked.
Reach for a pattern when a sensitive value shows up under many unpredictable
names (`db_password`, `smtpPassword`, `adminPasswordHash`, …) — one pattern
covers them all.

```go
// Extend the defaults with extra patterns (defaults still apply):
red := logcore.NewRedactor(logcore.RedactorOptions{
    Patterns: append(logcore.DefaultSensitivePatterns,
        "credential", // db_credential, awsCredentials, …
        "ssn",        // applicant_ssn, ssn_last4, …
        "_key",       // signing_key, encryption_key, … (but not "monkey": see caveat)
    ),
})
```

```go
// Replace the defaults with your own pattern set only:
red := logcore.NewRedactor(logcore.RedactorOptions{
    Keys:     []string{},                       // turn exact matching off
    Patterns: []string{"token", "passwd", "pii"},
})
// Masks: "access_token", "x_passwd", "pii_blob", "customerPII", …
```

The match is plain `strings.Contains` on the lowercased key — there is no word
boundary, so choose tokens carefully:

- **Good:** distinctive stems like `password`, `secret`, `credential`. Broad
  but safe — over-matching only costs a masked field, never a leak.
- **Caveat:** short or common tokens over-match. `key` would also mask
  `monkey` and `keyboard_layout`; `pan` would mask `company` and `plan`. Prefer
  a more specific token (`_key`, `card_number`) or use an exact `Keys`/`Extra`
  entry instead.

Patterns and exact keys are checked together — a key is masked if it matches
*either*. Pattern matching also feeds the core-level redactor when
`Options{RedactFields: true}` is set, so a stray `zap.String("db_password", …)`
anywhere in the code is caught too.

#### Partial reveal

Instead of fully masking, specific keys can keep their last *N* characters —
useful for correlating a card or token in logs without exposing it. Configure
per key via `PartialReveal`; unlisted keys stay fully masked, so the default
policy is unchanged:

```go
red := logcore.NewRedactor(logcore.RedactorOptions{
    PartialReveal: map[string]int{"card": 4, "pan": 4},
})
// "4111111111111111" → "[REDACTED]…1111"
// "authorization"    → "[REDACTED]"        (not configured → full mask)
```

Safety guards: a value is only partially revealed when its kept tail is at most
**half** the value (so a short secret never ends up mostly exposed — e.g. a
3-char `cvv` with reveal 4 falls back to a full mask), and non-string values
(numbers, bools, nested objects) under a sensitive key are always fully masked.
Partial reveal applies in both the schema layer and the core layer below.

**2. Core-level (every log call).** `Options{RedactFields: true}` wraps the
zap core with `logcore.NewRedactCore`, so *any* log field with a sensitive
key — anywhere in the codebase, not just request/response logs — is masked
on the way out. This catches a stray `zap.String("authorization", …)` or a
`zap.Any("payload", m)` that contains a nested secret. It is applied as the
outermost core, so APM error events are redacted too.

```go
l, _ := logcore.New(logcore.Options{
    Service:      "ledger",
    RedactFields: true,                 // global field redaction
    Redactor:     logcore.NewRedactor(logcore.RedactorOptions{ /* … */ }), // optional
})
```

`RedactFields` defaults to `false` for backwards compatibility; new services
should enable it. The two layers compose — schema redaction keeps the
structured `incoming`/`outgoing` shape readable, and the core layer is the
safety net for everything else.

### Trace correlation everywhere else

For any non-middleware log call, use the global helpers so the trace
fields are added automatically:

```go
logcore.LogCtx(ctx).Info("processed", zap.String("id", id))
// → adds trace.id / transaction.id from the active APM span
```

### Graceful shutdown integration

Register the logger's `Sync()` as a shutdown closer so buffered log lines are
flushed before the process exits. Register this last — after all other closers —
so shutdown log lines from every earlier phase are not lost:

```go
// On a specific logger instance:
l.RegisterWithManager(mgr, int(gscore.PhasePostDB), 0)

// Or using the global logger:
logcore.RegisterGlobalWithManager(mgr, int(gscore.PhasePostDB), 0)
```

`phase=0` defaults to `PhasePostDB`; `timeout=0` defaults to 5s. The `setup`
builder calls `logcore.RegisterGlobalWithManager` automatically as the last step
of `Build`.

### gscore.Logger adapter

`logcore.GSCoreGlobalLogger()` returns a value that satisfies `gscore.Logger`
(the three-method `Info`/`Warn`/`Error` interface), backed by the global zap
logger. Pass it to `gscore.Config.Logger` so shutdown phase events are emitted
through the same structured logger as the rest of the application:

```go
mgr := gscore.New(gscore.Config{
    Logger: logcore.GSCoreGlobalLogger(),
    // ...
})
```

---

## GORM Cache (`gormcache`)

A GORM plugin that reduces database load via two complementary mechanisms:
**request deduplication** (easer) and **response caching**. The plugin ships a
`Cacher` interface; ready-made implementations for go-redis and rueidis are
provided in `gsredis` and `gsrueidis`. All cache operations can be wrapped with
OTel spans via `apmcore.InstrumentCacher`.

### Features

- **Request deduplication (easer)** — if N identical queries run concurrently,
  only the first hits the database; the rest wait and receive the same result.
- **Response caching** — implement the `Cacher` interface to plug any backend
  (Redis, in-memory, etc.). Cache hits skip the database entirely.
- **Granular invalidation** — every Create/Update/Delete mutation fires
  `Cacher.Invalidate` with an `InvalidationEvent` carrying the affected tables,
  primary key values, and mutation type.
- **Tag-based invalidation** — tag cached queries via `Config.TagsFunc` and
  selectively evict them with `caches.WithInvalidateTags` on mutations.
- **Cache bypass per query** — `Config.SkipFunc` excludes queries (e.g. whole
  tables) from the cache entirely: no cache read, no write, no easer dedup.
- **Safe under concurrent load** — invalidation only fires after a mutation
  completes without error; failed or short-circuited operations (e.g. autobatch)
  do not evict the cache.
- **Compatible with any gorm-supported database.**

### Installation

```bash
go get github.com/adrielcodeco/go-tools/gormcache
```

For the ready-made Redis backend, also install:

```bash
go get github.com/adrielcodeco/go-tools/gsredis    # go-redis backend
# or
go get github.com/adrielcodeco/go-tools/gsrueidis  # rueidis backend
```

### Quick start

```go
import (
    "github.com/adrielcodeco/go-tools/gormcache"
    "github.com/adrielcodeco/go-tools/gsredis"
)

redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

cachesPlugin := &caches.Caches{Conf: &caches.Config{
    Easer:  true,                                          // enable request deduplication
    Cacher: gsredis.NewRedisCacher(redisClient, 5*time.Minute), // Redis backend
}}

if err := db.Use(cachesPlugin); err != nil {
    log.Fatal(err)
}
```

All subsequent `db.Find`, `db.First`, etc. calls are intercepted: cache hits
return immediately; misses run the query and store the result. `db.Create`,
`db.Save`, `db.Updates`, and `db.Delete` trigger `Cacher.Invalidate`
automatically.

### Easer (request deduplication)

When `Config.Easer = true`, identical concurrent queries are coalesced: the
first goroutine to arrive executes the query; all others block until it
completes and receive a deep copy of the result. This eliminates thundering-herd
behaviour for hot read paths without any cache backend.

```go
cachesPlugin := &caches.Caches{Conf: &caches.Config{
    Easer: true,
}}
_ = db.Use(cachesPlugin)

// The two concurrent Find calls below share one DB roundtrip.
var (
    q1Users []UserModel
    q2Users []UserModel
)
wg := &sync.WaitGroup{}
wg.Add(2)
go func() {
    db.Model(&UserModel{}).Joins("Role").Find(&q1Users, "Role.Name = ?", "Admin")
    wg.Done()
}()
go func() {
    time.Sleep(50 * time.Millisecond)
    db.Model(&UserModel{}).Joins("Role").Find(&q2Users, "Role.Name = ?", "Admin")
    wg.Done()
}()
wg.Wait()
```

### Cacher interface

Implement three methods to plug any cache backend:

```go
type Cacher interface {
    // Get returns the cached result for key, or (nil, nil) on a miss.
    Get(ctx context.Context, key string, q *Query[any]) (*Query[any], error)

    // Store persists the query result under key.
    // Use caches.TagsFromContext(ctx) to read tags set by TagsFunc.
    Store(ctx context.Context, key string, val *Query[any]) error

    // Invalidate evicts cache entries based on the mutation event.
    // event.Tags is populated from WithInvalidateTags; event.Tables and
    // event.EntityIDs are always populated from the GORM statement.
    Invalidate(ctx context.Context, event *InvalidationEvent) error
}
```

`Query[T]` provides `Marshal() ([]byte, error)` and `Unmarshal([]byte) error`
for serialisation. Use them in `Store` and `Get`:

```go
func (c *myCacher) Store(ctx context.Context, key string, val *caches.Query[any]) error {
    b, err := val.Marshal()  // JSON
    if err != nil {
        return err
    }
    return c.backend.Set(ctx, key, b, c.ttl)
}

func (c *myCacher) Get(ctx context.Context, key string, q *caches.Query[any]) (*caches.Query[any], error) {
    b, err := c.backend.Get(ctx, key)
    if err != nil {
        return nil, nil  // treat missing key as a miss
    }
    if err := q.Unmarshal(b); err != nil {
        return nil, err
    }
    return q, nil
}
```

### Ready-made backends

Both backends index cache keys by tag (via Redis `SADD tag:<tag> <key>`) so
`Invalidate` can evict exactly the right keys without a full-cache scan.

#### go-redis (`gsredis.NewRedisCacher`)

```go
import "github.com/adrielcodeco/go-tools/gsredis"

cacher := gsredis.NewRedisCacher(redisClient, 5*time.Minute)
// ttl=0 → keys persist until explicitly invalidated
```

#### rueidis (`gsrueidis.NewRueidisCache`)

```go
import "github.com/adrielcodeco/go-tools/gsrueidis"

cacher := gsrueidis.NewRueidisCache(rueidisClient, 5*time.Minute)
```

Rueidis pipelines all `Store` commands (`SET` + `SADD` + `EXPIRE`) in a single
`DoMulti` call, and `Invalidate` deletes all member keys in a single
multi-key `DEL`, making it the higher-throughput option for write-heavy
invalidation workloads.

> **Tag index note:** both backends build the tag→key index only from tags
> provided via `Config.TagsFunc` (or `caches.WithTags` at query sites). If no
> tags are stored, `Invalidate` is a no-op (entries expire via TTL). To
> invalidate by table, emit the table name as a tag:
>
> ```go
> TagsFunc: func(db *gorm.DB) []string {
>     return []string{db.Statement.Table}
> },
> ```

On every mutation both backends resolve and evict, in addition to explicit
`event.Tags`, the plain table tag (`"<table>"`) for each affected table —
always — plus entity tags (`"<table>:<id>"`) for each affected primary key.
`gsrueidis.WithTableFallback()` is deprecated and a no-op: table-tag eviction
is unconditional, since skipping it on single-row mutations silently left
table-tagged SELECTs stale until TTL.

### Tag-based invalidation

Tags let you selectively evict cache entries, similar to TanStack Query's query
keys. Instead of wiping the full cache on every mutation, you tag queries and
only evict the relevant ones.

**Tag queries** via `Config.TagsFunc`:

```go
cachesPlugin := &caches.Caches{Conf: &caches.Config{
    Cacher: cacher,
    TagsFunc: func(db *gorm.DB) []string {
        return []string{db.Statement.Table}
    },
}}
```

**Invalidate by tag** via `caches.WithInvalidateTags` on the mutation context:

```go
ctx := caches.WithInvalidateTags(context.Background(), "users")
db.WithContext(ctx).Create(&User{Name: "John"})
// → fires Cacher.Invalidate with event.Tags = ["users"]
```

**`InvalidationEvent` fields:**

| Field | Type | Description |
|---|---|---|
| `Tables` | `[]string` | Tables involved in the mutation |
| `EntityIDs` | `[]interface{}` | Primary key values of affected entities |
| `MutationType` | `MutationType` | `MutationCreate`, `MutationUpdate`, or `MutationDelete` |
| `Tags` | `[]string` | Tags from `WithInvalidateTags` (empty if not set) |

**Minimal invalidation example** (in-memory backend):

```go
func (c *memoryCacher) Invalidate(ctx context.Context, event *caches.InvalidationEvent) error {
    if len(event.Tags) > 0 {
        return c.invalidateByTags(event.Tags)
    }
    // No tags: fall back to wiping everything.
    c.store = &sync.Map{}
    return nil
}
```

### Skipping tables (`Config.SkipFunc`)

Some tables must never be served from cache — typically rows used in
validation reads where staleness breaks correctness (balances, idempotency
records, dependency/audit tables). `SkipFunc` is consulted on every SELECT
before the cache lookup; returning `true` bypasses the plugin entirely (no
cache read, no write, no easer dedup) and the query always hits the database:

```go
var skipTables = map[string]struct{}{
    "TransactionsDependency": {},
}

cachesPlugin := &caches.Caches{Conf: &caches.Config{
    Cacher: cacher,
    SkipFunc: func(db *gorm.DB) bool {
        _, skip := skipTables[db.Statement.Table]
        return skip
    },
    TagsFunc: func(db *gorm.DB) []string {
        return []string{db.Statement.Table}
    },
}}
```

> **Warning:** returning `nil` from `TagsFunc` does **not** skip caching — the
> entry is still cached, just untagged, which makes it *uninvalidatable* until
> TTL. Use `SkipFunc` to exclude queries from the cache.

### OTel instrumentation

Wrap any `Cacher` with `apmcore.InstrumentCacher` to emit OTel spans for every
`Get`, `Store`, and `Invalidate` call. Spans are named `gormcache.get`,
`gormcache.store`, and `gormcache.invalidate`, and include `cache.hit` (bool),
`cache.tags` (count), and `db.tables` attributes. Errors are recorded and the
span status is set to `Error` so APM error-rate metrics fire correctly.

```go
import "github.com/adrielcodeco/go-tools/apmcore"

cachesPlugin := &caches.Caches{Conf: &caches.Config{
    Cacher: apmcore.InstrumentCacher(
        gsredis.NewRedisCacher(redisClient, 5*time.Minute),
    ),
}}
```

### setup integration

`setup.Builder` wires gormcache in the correct position (after `gormautobatch`,
so autobatch's batch callbacks fire before cache invalidation callbacks). When
`WithOTel` is also set, `Build` automatically wraps the `Cacher` with
`apmcore.InstrumentCacher`.

```go
result, err := setup.New().
    WithGORM(db).
    WithOTel(ctx).
    WithRedis(redisClient, "redis-cache").
    WithGORMCacheConfig(caches.Config{
        Easer:  true,
        Cacher: gsredis.NewRedisCacher(redisClient, 5*time.Minute),
        TagsFunc: func(db *gorm.DB) []string {
            return []string{db.Statement.Table}
        },
    }).
    Build(mgr)
```

`WithGORMCache(p *caches.Caches)` is also available when you need to construct
the plugin yourself before passing it to `Build`.

---

## Elastic APM (`apmfiber` / `apmfiberv3`)

Wraps the [Elastic APM Go agent](https://www.elastic.co/guide/en/apm/agent/go/current/index.html)
into the same core-plus-adapter shape used by the rest of this toolbox.

### Packages

| Submodule | Import | Purpose |
|---|---|---|
| `apmcore` | `github.com/adrielcodeco/go-tools/apmcore` | Bootstrap + OTel bridge, DB driver wrapper, GORM plugin, pool metrics, zap helpers |
| `apmfiber` | `github.com/adrielcodeco/go-tools/apmfiber` | Fiber v2 middleware + labels + error capture |
| `apmfiberv3` | `github.com/adrielcodeco/go-tools/apmfiberv3` | Fiber v3 middleware + labels + error capture |

Each submodule has its own `go.mod` so projects that don't need APM aren't
forced to pull the Elastic + OTel dependency tree.

### Features

- **One-call bootstrap** — `apmcore.SetupOTelSDK(ctx)` wires the APM agent
  into the OTel global providers and registers an `apm.MetricsGatherer`.
  Returns a shutdown func to call after the server drains.
- **Fiber middleware** — `apmfiber.Middleware()` / `apmfiberv3.Middleware()`
  starts an APM transaction per request, names it `<METHOD> <route>`, and
  attaches it to the underlying `*fasthttp.RequestCtx`. Fiber v3 has no
  upstream adapter — this package ships one.
- **Transaction labels** — `Labels(LabelsConfig{...})` decodes a typed
  struct from the request body once and publishes business identifiers
  (`wallet_id`, `external_id`, …) as `labels.<key>` filters in Kibana.
- **Inline error capture** — `CaptureError(c, err)` records an error
  against the active transaction so handler-mapped errors (that never
  bubble to Fiber's `ErrorHandler`) still appear in Kibana → APM → Errors.
- **Foldable DB spans** — `apmcore.NewGormPlugin()` + a driver wrap via
  `apmcore.RegisterDriver(name, baseDriver)` produce a parent gorm span
  per logical operation, with prepare/exec/query/close spans nested
  inside. Works with any `database/sql` driver — pgx, mysql, sqlite —
  because the base driver is passed in.
- **DB-pool metrics** — `apmcore.RegisterDBPoolMetrics(sqlDB)` emits
  `db.pool.*` on the agent's metrics tick (chartable in Metrics Explorer).
- **Startup / probe metrics** — `apmcore.RegisterStartupMetricsWithManager`
  emits `app.startup.duration_ms` and the three `app.probe.*` gauges so you
  can see exactly when the pod started and tune Kubernetes probe parameters
  from real data.
- **HTTP / Redis / zap helpers** — `WrapHTTPTransport`, `InstrumentRedis`,
  `WrapZapCore`, `LogCtxFields` cover the surrounding instrumentation
  surface without forcing a particular client style.

### Installation

```bash
go get github.com/adrielcodeco/go-tools/apmcore
go get github.com/adrielcodeco/go-tools/apmfiber       # Fiber v2
# or
go get github.com/adrielcodeco/go-tools/apmfiberv3     # Fiber v3
```

Configure the agent via the standard `ELASTIC_APM_*` environment
variables (`ELASTIC_APM_SERVER_URL`, `ELASTIC_APM_SERVICE_NAME`, …). The
agent ignores `OTEL_*` variables — set both if you need the same value
in both subsystems.

### Quick start (Fiber v2)

```go
func main() {
    shutdown, err := apmcore.SetupOTelSDK(context.Background())
    if err != nil { panic(err) }
    http.DefaultTransport = apmcore.WrapHTTPTransport(http.DefaultTransport)

    app := fiber.New()
    app.Use(apmfiber.Middleware())          // must be first
    app.Use(apmfiber.Labels(apmfiber.LabelsConfig{
        Headers: map[string]string{"X-Origin": "origin"},
    }))

    go app.Listen(":8080")
    // ... wait for shutdown signal, then drain ...
    _ = shutdown(context.Background())
}
```

### Manager integration

`apmcore` ships two helpers for registering shutdown actions with the
graceful-shutdown Manager without importing `gscore` directly:

```go
shutdown, err := apmcore.SetupOTelSDK(ctx)

// Flush OTel spans and metrics after the DB pool closes.
apmcore.RegisterWithManager(shutdown, mgr, int(gscore.PhasePostDB), 0)

// Deregister the DB-pool metrics gatherer (avoids a zeroed-metrics tick
// after the pool closes). Call after mgr.RegisterDB(db).
sqlDB, _ := db.DB()
apmcore.RegisterDBPoolMetricsWithManager(sqlDB, mgr, int(gscore.PhasePostDB), 0)
```

Both helpers accept `phase=0` to default to `PhasePostDB` and `timeout=0` to
use the built-in defaults (15s for the OTel shutdown; 5s for pool metrics).

#### Startup and probe metrics

`apmcore.RegisterStartupMetricsWithManager` registers a metrics gatherer that
emits four values on every APM metrics tick (default 30 s, configurable via
`ELASTIC_APM_METRICS_INTERVAL`):

| Metric | Type | Description |
|---|---|---|
| `app.startup.duration_ms` | gauge | Milliseconds from `processStart` to `MarkStarted()`. Emits `0` until started, then holds the final value. |
| `app.probe.live` | gauge | Always `1` — the process is responding. |
| `app.probe.ready` | gauge | `1` while accepting traffic, `0` once shutdown begins. |
| `app.probe.started` | gauge | `1` after `MarkStarted()`, `0` during boot. |

All four land in the `metrics-apm.app.<service>-default` data stream and are
chartable from **Kibana → Observability → Infrastructure → Metrics Explorer**.

**Use case — tuning Kubernetes probe parameters:**

Plot `app.startup.duration_ms` across pods over time to find the P95 boot
duration. Then set:

```
startupProbe.failureThreshold = ceil(P95_boot_ms / (periodSeconds * 1000)) + 1
```

The `app.probe.*` gauges help answer "did this pod ever flip to not-ready?" in
a time range — useful when correlating a traffic spike or a rollout with probe
state.

**Manual wiring:**

```go
processStart := time.Now() // as early as possible in main

shutdown, _ := apmcore.SetupOTelSDK(ctx)
// ... set up db, app, mgr ...

if err := runMigrations(db); err != nil {
    log.Fatal(err)
}
mgr.MarkStarted()

apmcore.RegisterStartupMetricsWithManager(processStart, mgr)
// deregistered automatically at PhasePostDB, before the OTel shutdown
```

**Via `setup.Builder` (recommended):** call `WithProcessStart` with the
timestamp captured at the top of `main`. When both `WithOTel` and
`WithProcessStart` are set, `Build` calls
`apmcore.RegisterStartupMetricsWithManager` automatically:

```go
processStart := time.Now()

setup.New().
    WithOTel(ctx).
    WithProcessStart(processStart). // ← enables startup metrics
    WithHealthProbesV2(setup.HealthProbesConfig{}).
    WithStartupFn(func() error { return runMigrations(db) }).
    Build(mgr)
```

`WithProcessStart` has no effect if `WithOTel` is not also set.

#### InstrumentRueidis

`apmcore.InstrumentRueidis(client)` wraps a rueidis client with the OTel
`rueidisotel` adapter in a single call. Use it when you already hold a
`rueidis.Client` and want to add tracing without rebuilding the client:

```go
// SetupOTelSDK must have been called first.
tracedClient := apmcore.InstrumentRueidis(existingClient)
```

For new clients, prefer `rueidisotel.NewClient` directly so pool-level metrics
are also captured (see [Tracing rueidis with Elastic APM](#tracing-rueidis-with-elastic-apm)).

### APM transaction context in txctx

When `apmfiber.Middleware()` and `txctx.Middleware()` are both in the chain, the
txctx middleware must inherit the APM transaction from the Fiber request context,
not from `context.Background()`. Pass `apmfiber.TxContextExtractor()` as the
third argument to `txctx.Middleware`:

```go
// Fiber v2
app.Use(apmfiber.Middleware())   // must be first
app.Use(txctx.Middleware(db, txctx.Config{Timeout: 5 * time.Second},
    apmfiber.TxContextExtractor()))
```

```go
// Fiber v3
app.Use(apmfiberv3.Middleware())
app.Use(txctxv3.Middleware(db, txctxv3.Config{Timeout: 5 * time.Second},
    apmfiberv3.TxContextExtractor()))
```

Without the extractor, the transaction context would be derived from
`context.Background()` and the DB spans created inside handlers would not be
nested under the request's APM transaction.

### Local stack

A reference `docker-compose.apm.yml` lives in `examples/`. It bootstraps
Elasticsearch + Kibana + APM Server (8.13.4) with security enabled, the
APM integration pre-installed, and recommended agent central
configuration applied — no manual Kibana clicks required.

```bash
docker compose -f examples/docker-compose.apm.yml up -d
open http://localhost:5601    # elastic / changeme
```

### Pitfall index

- Read the active transaction via `c.Context()` (Fiber v2) or
  `c.RequestCtx()` (Fiber v3), **not** `c.UserContext()` — the agent
  stores the transaction on the underlying `*fasthttp.RequestCtx`.
- Set `ELASTIC_APM_*` env vars; the agent ignores `OTEL_*`.
- Call `apmcore.SetupOTelSDK` **before** opening DB/Redis clients so
  they pick up the global TracerProvider.
- `span, _ := apm.StartSpan(ctx, …)` discards the new ctx and breaks
  span nesting — use `span, ctx := …`.
- For foldable DB spans, the gorm plugin reassigns
  `tx.Statement.Context`; preserve it through your repositories with
  `db.WithContext(ctx)`.

---

## Sentry error reporting (`sentryfiber` / `sentryfiberv3`)

Wraps the [Sentry Go SDK](https://docs.sentry.io/platforms/go/) into the same
core-plus-adapter shape used by the rest of this toolbox. Its scope is
deliberately narrow: **error and crash reporting only.** Tracing and metrics
stay with `apmfiber`/`apmcore` (Elastic APM). The two coexist — a captured
Sentry error carries the active APM `trace_id`/`transaction_id` as tags so you
can jump from a Sentry issue to the matching trace in Kibana.

### Packages

| Submodule | Import | Purpose |
|---|---|---|
| `sentrycore` | `github.com/adrielcodeco/go-tools/sentrycore` | Bootstrap (no-op on empty DSN), capture helpers, APM trace correlation, HTTP breadcrumb transport, Manager closer |
| `sentryfiber` | `github.com/adrielcodeco/go-tools/sentryfiber` | Fiber v2 recovery/error-capture middleware |
| `sentryfiberv3` | `github.com/adrielcodeco/go-tools/sentryfiberv3` | Fiber v3 recovery/error-capture middleware |

Each submodule has its own `go.mod` so projects that don't need Sentry aren't
forced to pull the SDK.

### How it relates to Elastic APM

The Sentry Go SDK has **no automatic instrumentation** (unlike Python/Node,
there is no monkey-patching). Everything is explicit. The "auto, non-invasive"
surface this toolbox offers is three composable pieces, none of which change
application behavior when the DSN is empty:

| Concern | Elastic APM (existing) | Sentry (this module) |
|---|---|---|
| Request tracing / spans | ✅ `apmfiber` | ✖ (not its job) |
| Metrics / DB pool | ✅ `apmcore` | ✖ |
| Error events from logs | `Error` logs → Kibana APM Errors (`apmcore.WrapZapCore`) | `Error`+ logs → Sentry issues (`logcore.Options.SentryHook`) |
| Panics / unhandled errors in HTTP | `apmfiber` middleware | `sentryfiber` middleware |
| Outgoing HTTP context | exit spans (`apmcore.WrapHTTPTransport`) | breadcrumbs (`sentrycore.WrapHTTPTransport`) |
| Cross-link | — | trace tags copied from APM onto every Sentry event |

### Features

- **No-op when disabled** — `sentrycore.SetupSentry` with an empty DSN (and no
  `SENTRY_DSN` in the environment) returns a no-op shutdown func, reports
  `sentrycore.Enabled() == false`, and makes every capture a cheap no-op.
  You can ship the wiring unconditionally and turn Sentry on purely via config.
- **Zap hook** — `logcore.Options{SentryHook: true}` stacks a `zapcore.Core`
  that reports every log at or above `SentryLevel` (default `Error`) to Sentry.
  Wrapped *inside* the redaction core, so secrets are masked before an event
  leaves the process.
- **Fiber recovery middleware** — `sentryfiber.New()` / `sentryfiberv3.New()`
  bind a per-request hub, enrich it with the request and APM trace tags,
  recover panics (report + respond `500`, or re-panic with `Config.Repanic`),
  and capture errors bubbled to Fiber's `ErrorHandler`.
- **Inline error capture** — `sentryfiber.CaptureError(c, err)` reports against
  the current request's hub for handler-mapped errors that never bubble.
- **HTTP breadcrumbs** — `sentrycore.WrapHTTPTransport(base)` records each
  outgoing request as a breadcrumb; composes on top of
  `apmcore.WrapHTTPTransport`.
- **APM trace correlation** — `trace_id` / `transaction_id` / `span_id` from the
  active Elastic APM transaction are attached as Sentry tags automatically.
- **Header / query redaction** — the Fiber middleware attaches the request's
  headers and query string to the event, but sensitive values are masked first
  by a `logcore.Redactor` — the same type that redacts the structured logs
  (auth headers, tokens, `card`/`pan`/`cvv`, `ssn`, … with substring matching).
  It defaults to `logcore.DefaultRedactor()`; set `Config.Redactor` (or use
  `setup`, which wires it automatically) to share a **custom** denylist between
  logs and Sentry. See [Sharing a custom redactor](#sharing-a-custom-redactor).

### Installation

```bash
go get github.com/adrielcodeco/go-tools/sentrycore
go get github.com/adrielcodeco/go-tools/sentryfiber       # Fiber v2
# or
go get github.com/adrielcodeco/go-tools/sentryfiberv3     # Fiber v3
```

### Configuration

`sentrycore.Options` is an explicit struct (matching `logcore.Options`); any
zero field falls back to the SDK's own environment lookup, so the common case
is env-driven:

| Env var | `Options` field | Notes |
|---|---|---|
| `SENTRY_DSN` | `DSN` | Empty ⇒ Sentry disabled (no-op). |
| `SENTRY_ENVIRONMENT` | `Environment` | e.g. `production`, `staging`. |
| `SENTRY_RELEASE` | `Release` | version or git sha. |

Other fields: `ServerName`, `SampleRate` (0 ⇒ 1.0), `Debug`, `Tags`
(global tags, e.g. `service.name`), `FlushTimeout` (0 ⇒ 2s), and
`Extra func(*sentry.ClientOptions)` for anything the struct doesn't expose
(`BeforeSend`, custom transport, integrations).

### Quick start (Fiber v2)

Compile-checked, copy-pasteable versions of every snippet below live in
[`examples/sentry_scenarios.go`](examples/sentry_scenarios.go).

```go
func main() {
    // APM first so trace tags are available to Sentry.
    apmShutdown, _ := apmcore.SetupOTelSDK(context.Background())

    // DSN/env/release read from SENTRY_* when the fields are empty.
    sentryShutdown, err := sentrycore.SetupSentry(context.Background(), sentrycore.Options{
        Tags: map[string]string{"service.name": "my-service"},
    })
    if err != nil { panic(err) }

    http.DefaultTransport = sentrycore.WrapHTTPTransport(
        apmcore.WrapHTTPTransport(http.DefaultTransport),
    )

    app := fiber.New()
    app.Use(apmfiber.Middleware())   // must be first (starts APM transaction)
    app.Use(sentryfiber.New())       // after APM so trace tags resolve

    app.Get("/thing", func(c *fiber.Ctx) error {
        if err := doWork(c.Context()); err != nil {
            sentryfiber.CaptureError(c, err) // inline capture with trace tags
            return c.SendStatus(500)
        }
        return c.SendStatus(200)
    })

    go app.Listen(":8080")
    // ... on shutdown ...
    _ = sentryShutdown(context.Background()) // flush buffered events
    _ = apmShutdown(context.Background())
}
```

### Quick start (Fiber v3)

Identical shape; use the v3 packages and the value-receiver handler signature:

```go
app := fiber.New()
app.Use(apmfiberv3.Middleware())
app.Use(sentryfiberv3.New())

app.Get("/thing", func(c fiber.Ctx) error {
    if err := doWork(c.Context()); err != nil {
        sentryfiberv3.CaptureError(c, err)
        return c.SendStatus(500)
    }
    return c.SendStatus(200)
})
```

### Via `setup.Builder` (recommended)

`WithSentry` initializes Sentry (after APM, before the logger) and registers
the flush as a `PhasePostDB` closer. Enable `SentryHook` on the logger to also
report `Error`+ logs:

```go
res, err := setup.New().
    WithOTel(ctx).
    WithSentry(ctx, sentrycore.Options{
        Tags: map[string]string{"service.name": "my-service"},
    }).
    WithLogger(logcore.Options{
        Service:      "my-service",
        SentryHook:   true,   // Error+ logs → Sentry
        RedactFields: true,   // mask secrets before they reach Sentry
    }).
    WithGORM(db).
    WithFiberV2(app).
    Build(mgr)

// res.SentryShutdown is the flush func (no-op when DSN is empty).
app.Use(apmfiber.Middleware())
app.Use(sentryfiber.New())
```

Because everything is a no-op when the DSN is empty, this exact wiring is safe
in every environment — Sentry simply activates wherever `SENTRY_DSN` is set.

### Log → Sentry hook

With `SentryHook: true`, a call like the following produces both a normal log
line **and** a Sentry event, grouped by the error's type and tagged with the
request's trace id:

```go
logcore.LogCtx(ctx).Error("charge failed",
    zap.String("wallet_id", walletID),
    zap.Error(err),            // ← promoted to the Sentry exception
)
```

Fields whose key is `trace.id` / `transaction.id` / `span.id` are promoted to
Sentry tags; the rest land in the event's `log` context. When a `zap.Error`
field is present it is captured as the exception so issues group by error type
rather than by message.

> **Avoid double-reporting:** if you also use `apmcore.WrapZapCore` (the
> logcore default), an `Error` log already becomes an APM error doc *and* — with
> the hook on — a Sentry event. That is intended (two systems, two purposes).
> But at a central error-handler that also calls `apmfiber.CaptureError`, log at
> `Warn` (not `Error`) to avoid a duplicate Sentry issue for the same failure.

### Manager integration

`sentrycore` ships a helper to register the flush with the graceful-shutdown
Manager without importing `gscore` directly:

```go
sentryShutdown, _ := sentrycore.SetupSentry(ctx, opts)

// Flush buffered events after the DB pool closes (PhasePostDB).
sentrycore.RegisterWithManager(sentryShutdown, mgr, int(gscore.PhasePostDB), 0)
```

`phase=0` defaults to `PhasePostDB`; `timeout=0` defaults to 5s.

### Sharing a custom redactor

The header/query redaction (Fiber middleware) and the log-field redaction use
the **same type** (`logcore.Redactor`), but they are **separate instances**
unless you connect them. So "I added a pattern to the log redactor" does **not**
automatically mean Sentry masks it too — it depends on *how* you added it:

| How you extend the denylist | Logs | Sentry (headers/query) | Sentry (log hook) |
|---|---|---|---|
| Mutate globals `logcore.DefaultSensitiveKeys` / `DefaultSensitivePatterns` | ✅ | ✅ | ✅ |
| Custom `Redactor` shared with middleware (`Config.Redactor` / `Result.Redactor`) | ✅ | ✅ | ✅ |
| Custom `Redactor` passed **only** to `logcore.Options.Redactor` | ✅ | ❌ **leaks** | ✅ |
| Nothing (defaults) | ✅ | ✅ | ✅ |

The **log hook** column always reflects your custom redactor because the hook
core is wrapped *inside* the redact core (fields are masked before Sentry sees
them). The gap is the **middleware**: it holds its own `Redactor`, so a custom
one reaches it only via `Config.Redactor` (or `Result.Redactor` from `setup`).

Runnable versions of all three rows are in
[`examples/sentry_scenarios.go`](examples/sentry_scenarios.go)
(`sentryRedactSharedGlobals`, `sentryRedactSharedCustomInstance`,
`sentryRedactNotSharedPitfall`).

There are two ways to extend the denylist, with different reach:

**1. Extend the global defaults** — mutate the exported slices before building
anything. This reflects everywhere automatically (logs, log middleware, Sentry
middleware), because `logcore.DefaultRedactor()` reads them:

```go
logcore.DefaultSensitiveKeys = append(logcore.DefaultSensitiveKeys, "x-tenant-secret")
logcore.DefaultSensitivePatterns = append(logcore.DefaultSensitivePatterns, "passphrase")
```

**2. Pass a custom `Redactor`** — the idiomatic way, without touching globals.
A custom redactor given only to the logger does **NOT** reach Sentry on its
own; you must pass the same instance to the middleware. Build it once and share:

```go
red := logcore.NewRedactor(logcore.RedactorOptions{
    Extra:    []string{"x-tenant-secret"},   // add to the default keys
    Patterns: append(logcore.DefaultSensitivePatterns, "passphrase"),
})

app.Use(sentryfiber.NewWithConfig(sentryfiber.Config{Redactor: red}))
setup.New().
    WithLogger(logcore.Options{RedactFields: true, Redactor: red}).
    // ...
```

**Via `setup` (recommended)** — you don't have to thread it manually.
`Build` exposes the logger's effective redactor on `Result.Redactor` (the
custom one if set, else the default). Hand it to the middleware:

```go
res, _ := setup.New().
    WithSentry(ctx, sentrycore.Options{}).
    WithLogger(logcore.Options{RedactFields: true, Redactor: red}).
    Build(mgr)

app.Use(apmfiber.Middleware())
app.Use(sentryfiber.NewWithConfig(sentryfiber.Config{Redactor: res.Redactor}))
```

### Pitfall index

- **No auto-instrumentation in Go** — there is nothing to "enable" beyond the
  three pieces above (zap hook, Fiber middleware, HTTP transport). Adding the
  DSN alone captures nothing on its own.
- **Order matters** — put `sentryfiber.New()` *after* `apmfiber.Middleware()`;
  otherwise the APM transaction isn't on the context yet and events won't carry
  trace tags. In `setup.Builder`, `WithSentry` already runs after `WithOTel`.
- **Empty DSN is not an error** — `SetupSentry` returns `(noop, nil)`. Check
  `sentrycore.Enabled()` if you need to branch on it.
- **Flush on shutdown** — Sentry delivers asynchronously; call the returned
  shutdown func (or use `WithSentry`, which registers it) or you will drop the
  last events on exit.
- **Redaction** — enable `logcore.Options.RedactFields` alongside `SentryHook`
  so sensitive log field values are masked before the event is built. The Fiber
  middleware redacts request headers/query with a `logcore.Redactor`
  (default: `logcore.DefaultRedactor()`). A **custom** redactor passed only to
  the logger does not reach the middleware — share the instance via
  `Config.Redactor` / `Result.Redactor`. See
  [Sharing a custom redactor](#sharing-a-custom-redactor).
