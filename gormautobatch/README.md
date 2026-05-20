# gormautobatch

A GORM plugin that automatically switches between individual and batch database operations based on measured P95 latency.

When latency is high, operations are buffered and flushed as a single transaction — reducing round-trips. When latency is low, operations are sent individually with no overhead.

## How it works

- Tracks operation latency using a **P95 sliding window** (Prometheus-style bucket ring, last 30s by default)
- When `P95 > LatencyThreshold` → **batch mode**: operations are buffered and flushed together
- When `P95 ≤ LatencyThreshold` → **individual mode**: operations pass through normally
- Flush triggers: elapsed time **or** buffer size, whichever comes first
- Each op in a batch is isolated via `SAVEPOINT`: a per-op failure does not roll back neighbours

## Install

```bash
go get github.com/adrielcodeco/go-tools/gormautobatch
```

> Requires Go 1.25+ and GORM v1.30+

## Usage

```go
import (
    autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
    "gorm.io/gorm"
)

db, err := gorm.Open(...)

threshold := 50 * time.Millisecond
err = db.Use(autobatch.New(autobatch.Config{
    LatencyThreshold: &threshold,            // nil = disabled; 0 = always batch; >0 = adaptive
    FlushTimeout:     10 * time.Millisecond, // flush batch after 10ms idle
    MaxBatchSize:     100,                   // or when 100 ops are buffered
    WindowDuration:   30 * time.Second,      // P95 measured over last 30s
}))

// Regular GORM calls — the plugin decides whether to batch transparently.
db.Create(&user)
db.Model(&user).Updates(&payload)
db.Delete(&record)
```

## Config

| Field | Default | Description |
|---|---|---|
| `LatencyThreshold` | `nil` | P95 above this switches to batch mode (`nil` disables batching) |
| `FlushTimeout` | `10ms` | Max wait before flushing a partial batch |
| `MaxBatchSize` | `100` | Max ops per batch before forced flush |
| `WindowDuration` | `30s` | Sliding window duration for P95 measurement |

All fields are optional — zero values use the defaults above.

## Supported operations

- `db.Create()`
- `db.Updates()`
- `db.Delete()`

Queries (`Find`, `First`, etc.) are not batched.

## Batch semantics

All operations in a batch run inside a **single transaction**. Each op is wrapped in its own `SAVEPOINT`, so a per-op failure (e.g. a unique-constraint violation) is isolated: only the failing caller gets the error, the rest of the batch still commits.

Infrastructure failures of the outer transaction (BEGIN/COMMIT, connection loss, savepoint unsupported) propagate to every caller in the batch.

Callers block transparently until their batch is flushed — from the caller's perspective it looks like a normal synchronous GORM call.

## Limitations & caveats

- **Operations inside `db.Transaction(...)` or `db.Begin()` are never batched.**
  They run inline on the user's transaction to preserve atomicity and rollback semantics. The plugin detects this automatically.
- **Callbacks registered *after* `gorm:create`/`gorm:update`/`gorm:delete` are skipped for batched ops.**
  The plugin suppresses the core GORM callback after the batch executes the op via a separate session. Model hooks (e.g. `AfterCreate`) do run, but inside the batch session — not on the caller's `*gorm.DB`.
- **`RowsAffected` is propagated** back to the caller's `*gorm.DB` after the batch executes, but other `Statement` fields are not.
- **Graceful shutdown:** call `plugin.Close()` before exiting your process to drain in-flight batches. After Close, intercepted ops return `ErrBatcherClosed`.

---

## Compatibility with DBResolver

The plugin is compatible with [GORM DBResolver](https://gorm.io/docs/dbresolver.html) in standard single-primary configurations, subject to the caveats below.

### Registration order is mandatory

**DBResolver must be registered before autobatch:**

```go
db.Use(dbresolver.New(...))  // must come first
db.Use(autobatch.New(...))   // must come second
```

At `Initialize` time, autobatch captures the `*gorm.DB` it receives and stores it as `rootDB` inside the flush closures. All batch flushes run `rootDB.Transaction(...)` using that captured pointer. If autobatch initializes before DBResolver, `rootDB.ConnPool` is the raw `*sql.DB` — before DBResolver has installed its routing wrapper — and all batched writes will bypass DBResolver entirely with no error or warning.

### Per-op routing is suppressed inside the flush transaction

DBResolver routes each operation to the correct pool by replacing `db.Statement.ConnPool` in a `Before("*")` callback. It also contains an internal guard that skips this reassignment when the operation is already inside a `*sql.Tx` (i.e., when `ConnPool` implements `gorm.TxCommitter`).

The flush opens a transaction via `rootDB.Transaction(...)`. Every op inside that transaction runs with a `ConnPool` that is a `*sql.Tx`, so DBResolver's guard fires and the per-op routing is suppressed. The routing decision for the entire batch is made once at `BEGIN` time, on `rootDB.ConnPool`.

For a **single-primary + replicas** setup, this is equivalent to normal behaviour: writes always go to the primary, and the batch transaction starts on the primary pool.

### Multi-source (sharded) configurations are not supported

If DBResolver is configured with multiple sources where different primaries are selected based on the table name or a custom policy, batched writes will all be routed to the pool selected at `BEGIN` time, not to the table-appropriate primary. Do not use autobatch alongside a sharded DBResolver configuration.

### Operations inside explicit user transactions are unaffected

autobatch detects `db.Transaction(...)` and `db.Begin()` and skips batching for those operations. DBResolver's full per-op routing applies normally to any op wrapped in a user-managed transaction. If an operation must go through DBResolver's routing logic, wrapping it in an explicit transaction is the correct escape hatch:

```go
db.Transaction(func(tx *gorm.DB) error {
    // autobatch skips this op; DBResolver routes normally
    return tx.Create(&record).Error
})
```

### Summary

| Scenario | Status |
|---|---|
| Single-primary + read replicas | Safe — register DBResolver first |
| Multi-source sharded primaries | Not supported |
| DBResolver registered after autobatch | Silently bypasses DBResolver for all batched writes |
| Op inside `db.Transaction(...)` | Safe — autobatch skips it, DBResolver routes normally |

## License

MIT
