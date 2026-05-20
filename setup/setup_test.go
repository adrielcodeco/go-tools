package setup

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/adrielcodeco/go-tools/gormautobatch"
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

// fakeRegistrar is a recording fake manager that appends closer names in
// registration order. It satisfies the internal registrar interface.
type fakeRegistrar struct {
	names []string
}

func (r *fakeRegistrar) RegisterCloser(name string, _ int, _ time.Duration, _ func(ctx context.Context) error) {
	r.names = append(r.names, name)
}

func (r *fakeRegistrar) RegisterCloserWithPriority(name string, _ int, _ int, _ time.Duration, _ func(ctx context.Context) error) {
	r.names = append(r.names, name)
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
