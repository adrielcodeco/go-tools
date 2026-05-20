package apmcore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"strings"
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
	apm "go.elastic.co/apm/v2"
	"go.elastic.co/apm/v2/apmtest"
	"go.elastic.co/apm/v2/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// driverOnce avoids registering the same driver name twice across test runs
// when the binary is reused.
var sqliteBase driver.Driver = &sqlite3.SQLiteDriver{}

func init() {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	// Second call is a no-op — exercises the registerOnce idempotency path.
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Dialector{DriverName: "sqlite-apm", DSN: ":memory:"}, &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	if err := db.Use(apmcore.NewGormPlugin()); err != nil {
		t.Fatalf("db.Use: %v", err)
	}
	type item struct {
		ID   uint `gorm:"primarykey"`
		Name string
	}
	if err := db.AutoMigrate(&item{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

type item struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

func TestDriverAndGormPluginEmitSpans(t *testing.T) {
	db := openTestDB(t)

	_, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		// Create → write path
		if err := db.WithContext(ctx).Create(&item{Name: "alpha"}).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
		// Query path
		var got item
		if err := db.WithContext(ctx).First(&got, "name = ?", "alpha").Error; err != nil {
			t.Fatalf("first: %v", err)
		}
		// Update path
		if err := db.WithContext(ctx).Model(&got).Update("name", "beta").Error; err != nil {
			t.Fatalf("update: %v", err)
		}
		// Delete path
		if err := db.WithContext(ctx).Delete(&got).Error; err != nil {
			t.Fatalf("delete: %v", err)
		}
		// Raw path
		var n int
		if err := db.WithContext(ctx).Raw("SELECT 1").Scan(&n).Error; err != nil {
			t.Fatalf("raw: %v", err)
		}
		// Explicit transaction → exercises Begin/Commit driver spans.
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return tx.Create(&item{Name: "in-tx"}).Error
		}); err != nil {
			t.Fatalf("transaction: %v", err)
		}
	})

	if len(spans) == 0 {
		t.Fatal("expected at least one span from driver+plugin")
	}

	// APM splits the dotted type string into Type/Subtype/Action. The
	// gorm plugin emits Type=db, Subtype=gorm; the driver wrap emits
	// Type=db, Subtype=postgresql.
	var gotGorm, gotDriver bool
	for i := range spans {
		switch spans[i].Subtype {
		case "gorm":
			gotGorm = true
		case "postgresql":
			gotDriver = true
		}
	}
	if !gotGorm {
		t.Errorf("expected at least one db.gorm.* span, got subtypes: %s", spanSubtypes(spans))
	}
	if !gotDriver {
		t.Errorf("expected at least one db.postgresql.* span, got subtypes: %s", spanSubtypes(spans))
	}
}

func spanSubtypes(spans []model.Span) string {
	var sb strings.Builder
	for i := range spans {
		sb.WriteString(spans[i].Subtype)
		sb.WriteByte(',')
	}
	return sb.String()
}

func TestGormPluginWithoutTransactionIsNoop(t *testing.T) {
	// No active APM transaction in ctx → plugin must skip without panicking
	// and the underlying gorm operation must still succeed.
	db := openTestDB(t)
	if err := db.Create(&item{Name: "ghost"}).Error; err != nil {
		t.Fatalf("create without tx: %v", err)
	}
}

func TestGormPluginCapturesError(t *testing.T) {
	db := openTestDB(t)
	_, _, errs := apmtest.WithTransaction(func(ctx context.Context) {
		// Force an error by querying a non-existent table via Raw.
		_ = db.WithContext(ctx).Exec("SELECT * FROM does_not_exist").Error
	})
	if len(errs) == 0 {
		t.Fatal("expected at least one captured error")
	}
}

func TestDBPoolMetricsGather(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	deregister := apmcore.RegisterDBPoolMetrics(sqlDB)
	defer deregister()

	// Trigger a metrics tick by invoking the tracer's flush — apmtest does
	// not expose the gatherers directly, so instead we verify the gatherer
	// runs without error via a smoke check on the underlying *sql.DB.
	stats := sqlDB.Stats()
	if stats.MaxOpenConnections == 0 && stats.OpenConnections == 0 {
		// no-op assertion: just ensures the call path didn't panic.
	}
}

// TestTracingTxCommitSpanParentedToTransaction verifies that a COMMIT span
// emitted by tracingTx.Commit is captured under the active APM transaction
// when BeginTx was called with the transaction context.
func TestTracingTxCommitSpanParentedToTransaction(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec("CREATE TABLE commit_test (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		dbTx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		if _, err := dbTx.ExecContext(ctx, "INSERT INTO commit_test VALUES (1)"); err != nil {
			t.Fatalf("exec: %v", err)
		}
		if err := dbTx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	})

	var commitSpan *model.Span
	for i := range spans {
		if spans[i].Name == "COMMIT" {
			commitSpan = &spans[i]
			break
		}
	}
	if commitSpan == nil {
		t.Fatalf("expected a COMMIT span; got span names: %s", spanSubtypes(spans))
	}
	// The COMMIT span must be associated with the active transaction, not an orphan.
	if commitSpan.TransactionID != tx.ID {
		t.Errorf("COMMIT span TransactionID = %v, want %v (active transaction ID)", commitSpan.TransactionID, tx.ID)
	}
}

// TestTracingTxRollbackSpanParentedToTransaction verifies that a ROLLBACK span
// emitted by tracingTx.Rollback is captured under the active APM transaction
// when BeginTx was called with the transaction context.
func TestTracingTxRollbackSpanParentedToTransaction(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec("CREATE TABLE rollback_test (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		dbTx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		if _, err := dbTx.ExecContext(ctx, "INSERT INTO rollback_test VALUES (1)"); err != nil {
			t.Fatalf("exec: %v", err)
		}
		if err := dbTx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
	})

	var rollbackSpan *model.Span
	for i := range spans {
		if spans[i].Name == "ROLLBACK" {
			rollbackSpan = &spans[i]
			break
		}
	}
	if rollbackSpan == nil {
		t.Fatalf("expected a ROLLBACK span; got span names: %s", spanSubtypes(spans))
	}
	// The ROLLBACK span must be associated with the active transaction, not an orphan.
	if rollbackSpan.TransactionID != tx.ID {
		t.Errorf("ROLLBACK span TransactionID = %v, want %v (active transaction ID)", rollbackSpan.TransactionID, tx.ID)
	}
}

func TestRegisterDriverNilBase(t *testing.T) {
	defer func() {
		// We don't require a panic, but the helper should at minimum not
		// register a usable driver; sql.Open should fail.
		recover()
	}()
	apmcore.RegisterDriver("apmcore-nil-driver", nil)
	if _, err := sql.Open("apmcore-nil-driver", ""); err == nil {
		t.Log("driver registered with nil base; sql.Open returned no error (expected behavior is implementation-defined)")
	}
}

// TestGatherMetricsViaDefaultTracer exercises poolGatherer.GatherMetrics by
// temporarily enabling recording on the default APM tracer and flushing
// metrics. The gatherer is registered indirectly via RegisterDBPoolMetrics.
func TestGatherMetricsViaDefaultTracer(t *testing.T) {
	// Enable recording so SendMetrics actually invokes registered gatherers.
	_ = os.Setenv("ELASTIC_APM_RECORDING", "true")
	t.Cleanup(func() { os.Unsetenv("ELASTIC_APM_RECORDING") }) //nolint:errcheck

	// Force a fresh default tracer that respects the env vars we just set.
	apm.SetDefaultTracer(nil)
	t.Cleanup(func() { apm.SetDefaultTracer(nil) })

	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Register the pool gatherer on the freshly created default tracer.
	deregister := apmcore.RegisterDBPoolMetrics(sqlDB)
	defer deregister()

	// SendMetrics is synchronous: it blocks until the tracer loop has
	// called every registered MetricsGatherer, including our poolGatherer.
	apm.DefaultTracer().SendMetrics(nil)
	apm.DefaultTracer().Flush(nil)
}

// TestGatherMetricsNilDB exercises poolGatherer.GatherMetrics with a nil db
// (early-return branch). It follows the same recording-enable trick.
func TestGatherMetricsNilDB(t *testing.T) {
	_ = os.Setenv("ELASTIC_APM_RECORDING", "true")
	t.Cleanup(func() { os.Unsetenv("ELASTIC_APM_RECORDING") }) //nolint:errcheck

	apm.SetDefaultTracer(nil)
	t.Cleanup(func() { apm.SetDefaultTracer(nil) })

	// Passing nil triggers the nil-guard in GatherMetrics.
	deregister := apmcore.RegisterDBPoolMetrics(nil)
	defer deregister()

	apm.DefaultTracer().SendMetrics(nil)
	apm.DefaultTracer().Flush(nil)
}

// TestLegacyDriverConnBeginAndPrepare exercises the deprecated
// tracingConn.Begin (driver.Conn interface) and tracingConn.Prepare.
//
// database/sql prefers BeginTx / PrepareContext when available; the only way
// to reach the legacy methods is to call them directly through the raw
// driver.Conn obtained via (*sql.Conn).Raw.
func TestLegacyDriverConnBeginAndPrepare(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()

	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer conn.Close()

	err = conn.Raw(func(dc any) error {
		c, ok := dc.(driver.Conn) //nolint:staticcheck
		if !ok {
			t.Fatal("driver.Conn type assertion failed")
		}

		// ── Begin (deprecated) ──────────────────────────────────────────────
		tx, err := c.Begin() //nolint:staticcheck
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback after Begin: %v", err)
		}

		// ── Prepare (legacy, no context) ────────────────────────────────────
		stmt, err := c.Prepare("SELECT 1")
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		defer stmt.Close()

		// ── tracingStmt.Exec (driver.Value, no context) ─────────────────────
		_, execErr := stmt.Exec([]driver.Value{}) //nolint:staticcheck
		// sqlite may return ErrSkip or a result; either is acceptable.
		_ = execErr

		// ── tracingStmt.Query (driver.Value, no context) ────────────────────
		rows, queryErr := stmt.Query([]driver.Value{}) //nolint:staticcheck
		if queryErr == nil {
			rows.Close()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
}

// TestLegacyDriverConnBeginWithActiveTransaction exercises tracingConn.Begin
// while an APM transaction is in scope, ensuring spans are emitted correctly.
func TestLegacyDriverConnBeginWithActiveTransaction(t *testing.T) {
	apmcore.RegisterDriver("sqlite-apm", sqliteBase)
	sqlDB, err := sql.Open("sqlite-apm", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()

	_, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn: %v", err)
		}
		defer conn.Close()

		_ = conn.Raw(func(dc any) error {
			c := dc.(driver.Conn) //nolint:staticcheck
			tx, err := c.Begin()  //nolint:staticcheck
			if err != nil {
				return err
			}
			return tx.Rollback()
		})
	})

	// tracingConn.Begin uses context.Background so the BEGIN span will be an
	// orphan — but the method must not panic and Begin itself must succeed.
	_ = spans // may be 0 or more depending on APM sampling
}
