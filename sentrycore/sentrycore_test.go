package sentrycore_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/sentrycore"
)

// Compile-time assertion: *gscore.Manager must satisfy the registrar the
// package's RegisterWithManager expects.
var _ sentrycore.CloserRegistrar = (*gscore.Manager)(nil)

// TestSetupSentry_EmptyDSNIsNoop verifies that with no DSN configured the
// setup returns a usable no-op ShutdownFunc, reports Enabled()==false, and
// never errors — the "does not interfere with the running app" guarantee.
func TestSetupSentry_EmptyDSNIsNoop(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")

	shutdown, err := sentrycore.SetupSentry(context.Background(), sentrycore.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil ShutdownFunc")
	}
	if sentrycore.Enabled() {
		t.Error("expected Enabled() to be false with empty DSN")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown returned error: %v", err)
	}
	// Captures must be safe no-ops when disabled.
	sentrycore.CaptureException(context.Background(), context.DeadlineExceeded)
}

// TestCaptureFields_NoTransaction returns nil when ctx has no APM
// transaction, so the Fiber adapters skip tagging cleanly.
func TestCaptureFields_NoTransaction(t *testing.T) {
	if tags := sentrycore.CaptureFields(context.Background()); tags != nil {
		t.Errorf("expected nil tags without an APM transaction, got %v", tags)
	}
	if tags := sentrycore.CaptureFields(nil); tags != nil { //nolint:staticcheck // nil ctx is a valid guard input
		t.Errorf("expected nil tags for nil ctx, got %v", tags)
	}
}

// TestWrapHTTPTransport_ForwardsWhenDisabled ensures the breadcrumb
// transport delegates to its base even when Sentry is disabled.
func TestWrapHTTPTransport_ForwardsWhenDisabled(t *testing.T) {
	called := false
	base := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	rt := sentrycore.WrapHTTPTransport(base)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://example.test", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if !called {
		t.Error("base RoundTripper was not called")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
