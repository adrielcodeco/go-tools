package setup

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fiberv2 "github.com/gofiber/fiber/v2"
	fiberv3 "github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
	"github.com/redis/rueidis"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/logcore"
)

// TestBuilder_MinimalBuild verifies that New().Build(mgr) with no options set
// completes without error and returns a non-nil Result with nil fields.
func TestBuilder_MinimalBuild(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	res, err := New().Build(mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil Result")
	}
	if res.Logger != nil {
		t.Error("expected Logger to be nil when WithLogger was not called")
	}
	if res.Shutdown != nil {
		t.Error("expected Shutdown to be nil when WithOTel was not called")
	}
}

// TestBuilder_WithLogger verifies that WithLogger causes Result.Logger to be
// non-nil and that the global logger is updated.
func TestBuilder_WithLogger(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	res, err := New().
		WithLogger(logcore.Options{DisableAPMCore: true}).
		Build(mgr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Logger == nil {
		t.Fatal("expected Result.Logger to be non-nil after WithLogger")
	}
}

// closerRecord stores a single registered closer for inspection in tests.
type closerRecord struct {
	name string
	fn   func(ctx context.Context) error
}

// fakeRegistrar is a recording fake manager that appends closer names and
// functions in registration order. It satisfies the internal registrar interface.
type fakeRegistrar struct {
	names   []string
	closers []closerRecord
}

func (r *fakeRegistrar) RegisterCloser(name string, _ int, _ time.Duration, fn func(ctx context.Context) error) {
	r.names = append(r.names, name)
	r.closers = append(r.closers, closerRecord{name: name, fn: fn})
}

func (r *fakeRegistrar) RegisterCloserWithPriority(name string, _ int, _ int, _ time.Duration, fn func(ctx context.Context) error) {
	r.names = append(r.names, name)
	r.closers = append(r.closers, closerRecord{name: name, fn: fn})
}

// TestBuilder_RegistrationOrder uses a recording fake manager to verify that
// txcore is registered before autobatch, matching the documented Build order.
func TestBuilder_RegistrationOrder(t *testing.T) {
	fake := &fakeRegistrar{}

	// Use a non-nil *gorm.DB placeholder. gorm.DB's zero value is safe here
	// because txcore.RegisterWithManager only stores a WaitGroup drain
	// closure — it never calls db methods during registration.
	db := &gorm.DB{}

	// autobatch.New requires a Config; nil LatencyThreshold disables batching
	// (plugin is a no-op) which is safe for this registration-order test.
	plugin := autobatch.New(autobatch.Config{})

	_, err := New().
		WithGORM(db).
		WithAutobatch(plugin).
		build(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := fake.names
	txcoreIdx := -1
	autobatchIdx := -1
	for i, n := range names {
		switch n {
		case "txcore-drain":
			txcoreIdx = i
		case "autobatch":
			autobatchIdx = i
		}
	}
	if txcoreIdx < 0 {
		t.Fatalf("txcore-drain not found in registration names: %v", names)
	}
	if autobatchIdx < 0 {
		t.Fatalf("autobatch not found in registration names: %v", names)
	}
	if txcoreIdx >= autobatchIdx {
		t.Errorf("expected txcore-drain (idx %d) before autobatch (idx %d) in %v",
			txcoreIdx, autobatchIdx, names)
	}
}

// fakeRedisClient is a minimal go-redis UniversalClient stub for testing.
// Only Close() needs to work; all query methods panic (they should never be
// called during registration).
type fakeRedisClient struct {
	redis.UniversalClient
	closed bool
}

func (f *fakeRedisClient) Close() error {
	f.closed = true
	return nil
}

// fakeRueidisClient is a minimal rueidis.Client stub for testing.
type fakeRueidisClient struct {
	rueidis.Client
	closed bool
}

func (f *fakeRueidisClient) Close() {
	f.closed = true
}

// TestBuilder_WithRedis_Registers verifies that WithRedis causes a closer to be
// registered under the supplied name and that multiple calls accumulate.
func TestBuilder_WithRedis_Registers(t *testing.T) {
	fake := &fakeRegistrar{}

	client1 := &fakeRedisClient{}
	client2 := &fakeRedisClient{}

	_, err := New().
		WithRedis(client1, "redis-cache").
		WithRedis(client2, "redis-rate-limit").
		build(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := map[string]bool{}
	for _, n := range fake.names {
		found[n] = true
	}
	if !found["redis-cache"] {
		t.Errorf("expected redis-cache to be registered, got names: %v", fake.names)
	}
	if !found["redis-rate-limit"] {
		t.Errorf("expected redis-rate-limit to be registered, got names: %v", fake.names)
	}
}

// TestBuilder_WithRueidis_Registers verifies that WithRueidis causes a closer
// to be registered under the supplied name and that multiple calls accumulate.
func TestBuilder_WithRueidis_Registers(t *testing.T) {
	fake := &fakeRegistrar{}

	client1 := &fakeRueidisClient{}
	client2 := &fakeRueidisClient{}

	_, err := New().
		WithRueidis(client1, "rueidis-primary").
		WithRueidis(client2, "rueidis-secondary").
		build(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := map[string]bool{}
	for _, n := range fake.names {
		found[n] = true
	}
	if !found["rueidis-primary"] {
		t.Errorf("expected rueidis-primary to be registered, got names: %v", fake.names)
	}
	if !found["rueidis-secondary"] {
		t.Errorf("expected rueidis-secondary to be registered, got names: %v", fake.names)
	}
}

// TestBuilder_WithRedis_CloserFires verifies the registered closer actually
// calls Close() on the client.
func TestBuilder_WithRedis_CloserFires(t *testing.T) {
	fake := &fakeRegistrar{}
	client := &fakeRedisClient{}

	_, err := New().WithRedis(client, "redis-test").build(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the closer for "redis-test" and fire it.
	for _, c := range fake.closers {
		if c.name == "redis-test" {
			if err := c.fn(context.Background()); err != nil {
				t.Fatalf("closer returned error: %v", err)
			}
			break
		}
	}
	if !client.closed {
		t.Error("expected Close() to be called on the redis client")
	}
}

// TestBuilder_WithRueidis_CloserFires verifies the registered closer actually
// calls Close() on the rueidis client.
func TestBuilder_WithRueidis_CloserFires(t *testing.T) {
	fake := &fakeRegistrar{}
	client := &fakeRueidisClient{}

	_, err := New().WithRueidis(client, "rueidis-test").build(fake)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range fake.closers {
		if c.name == "rueidis-test" {
			if err := c.fn(context.Background()); err != nil {
				t.Fatalf("closer returned error: %v", err)
			}
			break
		}
	}
	if !client.closed {
		t.Error("expected Close() to be called on the rueidis client")
	}
}

// openTestDB opens an in-memory SQLite database for use in tests that need a
// real *gorm.DB (e.g., db.Use or db.DB() calls).
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}
	return db
}

// TestBuilder_WithGORM_RegistersDB verifies that Build calls mgr.RegisterDB so
// the pool is closed during PhaseDB. We use a real *gscore.Manager because
// RegisterDB is not part of the registrar interface and requires the concrete type.
func TestBuilder_WithGORM_RegistersDB(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	db := openTestDB(t)

	_, err := New().WithGORM(db).Build(mgr)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	// Trigger + Wait exercises PhaseDB — if RegisterDB was called, the DB close
	// runs without a data race on the pool. A panic here would indicate the DB
	// was not registered at all, or was registered twice unexpectedly.
	mgr.Trigger()
	if waitErr := mgr.Wait(); waitErr != nil {
		t.Fatalf("Manager.Wait returned error after Trigger: %v", waitErr)
	}
}

// TestBuilder_WithGORM_PoolMetrics_Registers verifies that when both WithGORM
// and WithOTel are set, apmcore.RegisterDBPoolMetricsWithManager wires a
// "apmcore-pool-metrics" closer via the registrar.
func TestBuilder_WithGORM_PoolMetrics_Registers(t *testing.T) {
	fake := &fakeRegistrar{}
	db := openTestDB(t)

	ctx := context.Background()
	b := New().WithGORM(db)
	b.otelCtx = &ctx // inject otelCtx without calling SetupOTelSDK to avoid APM agent

	_, err := b.build(fake)
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}

	found := false
	for _, n := range fake.names {
		if n == "apmcore-pool-metrics" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("apmcore-pool-metrics not registered; got: %v", fake.names)
	}
}

// TestBuilder_WithGORM_AppliesGormPlugin verifies that Build wires
// apmcore.NewGormPlugin() onto the GORM DB when both WithGORM and WithOTel are
// active. A real in-memory SQLite DB is used so db.Use can call Initialize.
func TestBuilder_WithGORM_AppliesGormPlugin(t *testing.T) {
	fake := &fakeRegistrar{}
	db := openTestDB(t)

	ctx := context.Background()
	b := New().WithGORM(db)
	b.otelCtx = &ctx // inject otelCtx without calling SetupOTelSDK

	_, err := b.build(fake)
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}

	// Verify the plugin was registered on the DB.
	p := db.Config.Plugins["apmcore:dbtrace"]
	if p == nil {
		t.Error("expected apmcore:dbtrace plugin to be registered on the gorm.DB")
	}
}

// TestBuilder_WithAutobatchConfig_InjectsSpanEmitter verifies that when
// WithAutobatchConfig and WithOTel are both set, Build injects a non-nil
// SpanEmitter from apmcore.BatchSpanEmitter() into the resolved config so that
// batched writes are visible in APM.
func TestBuilder_WithAutobatchConfig_InjectsSpanEmitter(t *testing.T) {
	fake := &fakeRegistrar{}
	db := openTestDB(t)

	zero := time.Duration(0)
	cfg := autobatch.Config{LatencyThreshold: &zero}

	ctx := context.Background()
	b := New().WithGORM(db).WithAutobatchConfig(cfg)
	b.otelCtx = &ctx // inject without SetupOTelSDK

	_, err := b.build(fake)
	if err != nil {
		t.Fatalf("build returned error: %v", err)
	}

	// Verify that the autobatch plugin is registered and "autobatch" closer exists.
	found := false
	for _, n := range fake.names {
		if n == "autobatch" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("autobatch closer not found; got: %v", fake.names)
	}

	// Verify the plugin was applied to the DB (gormautobatch registers callbacks
	// under "gorm:autobatch").
	if db.Config.Plugins["gorm:autobatch"] == nil {
		t.Error("expected gorm:autobatch plugin to be registered on the gorm.DB")
	}
}

// TestBuilder_WithHealthProbesV2_RequiresFiberV2 verifies that Build returns an
// error when WithHealthProbesV2 is called without a prior WithFiberV2.
func TestBuilder_WithHealthProbesV2_RequiresFiberV2(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	_, err := New().WithHealthProbesV2(HealthProbesConfig{}).Build(mgr)
	if err == nil {
		t.Fatal("expected error when WithHealthProbesV2 called without WithFiberV2, got nil")
	}
}

// TestBuilder_WithHealthProbesV3_RequiresFiberV3 verifies that Build returns an
// error when WithHealthProbesV3 is called without a prior WithFiberV3.
func TestBuilder_WithHealthProbesV3_RequiresFiberV3(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	_, err := New().WithHealthProbesV3(HealthProbesConfig{}).Build(mgr)
	if err == nil {
		t.Fatal("expected error when WithHealthProbesV3 called without WithFiberV3, got nil")
	}
}

// TestBuilder_WithHealthProbesV2_RegistersRoutes verifies that the three probe
// routes are registered on the Fiber v2 app and return the expected status codes.
func TestBuilder_WithHealthProbesV2_RegistersRoutes(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	app := fiberv2.New()
	_, err := New().WithFiberV2(app).WithHealthProbesV2(HealthProbesConfig{}).Build(mgr)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/healthz/live", 200},
		{"/healthz/ready", 200},   // not yet triggered
		{"/healthz/startup", 503}, // not yet marked started
	} {
		resp, err := app.Test(newGET(tc.path))
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		if resp.StatusCode != tc.want {
			t.Errorf("GET %s: got %d want %d", tc.path, resp.StatusCode, tc.want)
		}
	}

	// After MarkStarted, startup probe flips to 200.
	mgr.MarkStarted()
	resp, err := app.Test(newGET("/healthz/startup"))
	if err != nil {
		t.Fatalf("GET /healthz/startup after MarkStarted: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("after MarkStarted: got %d want 200", resp.StatusCode)
	}
}

// TestBuilder_WithHealthProbesV3_RegistersRoutes verifies that the three probe
// routes are registered on the Fiber v3 app and return the expected status codes.
func TestBuilder_WithHealthProbesV3_RegistersRoutes(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	app := fiberv3.New()
	_, err := New().WithFiberV3(app).WithHealthProbesV3(HealthProbesConfig{}).Build(mgr)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/healthz/live", 200},
		{"/healthz/ready", 200},   // not yet triggered
		{"/healthz/startup", 503}, // not yet marked started
	} {
		resp, err := app.Test(newGET(tc.path))
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		if resp.StatusCode != tc.want {
			t.Errorf("GET %s: got %d want %d", tc.path, resp.StatusCode, tc.want)
		}
	}

	mgr.MarkStarted()
	resp, err := app.Test(newGET("/healthz/startup"))
	if err != nil {
		t.Fatalf("GET /healthz/startup after MarkStarted: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("after MarkStarted: got %d want 200", resp.StatusCode)
	}
}

// TestBuilder_WithHealthProbesV2_CustomPaths verifies that custom paths are
// respected when HealthProbesConfig is non-zero.
func TestBuilder_WithHealthProbesV2_CustomPaths(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	app := fiberv2.New()
	_, err := New().WithFiberV2(app).WithHealthProbesV2(HealthProbesConfig{
		LivenessPath:  "/live",
		ReadinessPath: "/ready",
		StartupPath:   "/startup",
	}).Build(mgr)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mgr.MarkStarted()

	for _, path := range []string{"/live", "/ready", "/startup"} {
		resp, err := app.Test(newGET(path))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: got %d want 200", path, resp.StatusCode)
		}
	}
}

func newGET(path string) *http.Request {
	return httptest.NewRequest("GET", path, nil)
}

// TestBuilder_WithStartupFn_CallsMarkStarted verifies that a successful
// startup function causes mgr.IsStarted() to be true after Build.
func TestBuilder_WithStartupFn_CallsMarkStarted(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	called := false
	_, err := New().WithStartupFn(func() error {
		called = true
		return nil
	}).Build(mgr)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !called {
		t.Error("startup function was not called")
	}
	if !mgr.IsStarted() {
		t.Error("expected mgr.IsStarted() == true after successful startup function")
	}
}

// TestBuilder_WithStartupFn_ErrorPreventsMarkStarted verifies that when the
// startup function returns an error, Build returns that error and
// mgr.IsStarted() remains false.
func TestBuilder_WithStartupFn_ErrorPreventsMarkStarted(t *testing.T) {
	mgr := gscore.New(gscore.Config{})
	_, err := New().WithStartupFn(func() error {
		return fmt.Errorf("migrations failed")
	}).Build(mgr)
	if err == nil {
		t.Fatal("expected Build to return error when startup function fails")
	}
	if mgr.IsStarted() {
		t.Error("expected mgr.IsStarted() == false when startup function fails")
	}
}
