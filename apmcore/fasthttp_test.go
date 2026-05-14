package apmcore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valyala/fasthttp"
	"go.elastic.co/apm/v2/apmtest"

	"github.com/adrielcodeco/go-tools/apmcore"
)

func TestTraceFastHTTPCallSuccess(t *testing.T) {
	_, spans, _ := apmtest.WithTransaction(func(ctx context.Context) {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		defer fasthttp.ReleaseResponse(resp)

		req.SetRequestURI("https://api.example.com/charge")
		req.Header.SetMethod("POST")

		var called bool
		err := apmcore.TraceFastHTTPCall(ctx, req, resp, func() error {
			called = true
			resp.SetStatusCode(200)
			return nil
		})
		if err != nil || !called {
			t.Fatalf("err=%v called=%v", err, called)
		}
		if got := string(req.Header.Peek("Traceparent")); got == "" {
			t.Error("expected traceparent injected on request")
		}
	})

	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Type != "external" || spans[0].Subtype != "http" {
		t.Errorf("expected external.http span, got %s.%s", spans[0].Type, spans[0].Subtype)
	}
}

func TestTraceFastHTTPCallCapturesError(t *testing.T) {
	_, _, errs := apmtest.WithTransaction(func(ctx context.Context) {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseRequest(req)
		defer fasthttp.ReleaseResponse(resp)

		req.SetRequestURI("https://api.example.com/x")
		req.Header.SetMethod("GET")
		_ = apmcore.TraceFastHTTPCall(ctx, req, resp, func() error {
			return errors.New("network unreachable")
		})
	})
	if len(errs) == 0 {
		t.Fatal("expected captured error")
	}
}

func TestTraceFastHTTPCallNoTransaction(t *testing.T) {
	// No active APM tx → call still runs.
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)
	req.SetRequestURI("https://example.com/")
	req.Header.SetMethod("GET")

	var called bool
	err := apmcore.TraceFastHTTPCall(context.Background(), req, resp, func() error {
		called = true; return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestTraceFastHTTPCallNilGuards(t *testing.T) {
	// Nil req → do still runs.
	var called bool
	err := apmcore.TraceFastHTTPCall(context.Background(), nil, nil, func() error {
		called = true; return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	// Nil do → returns nil without panic.
	if err := apmcore.TraceFastHTTPCall(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
}

func TestInjectFastHTTPTraceContext(t *testing.T) {
	apmtest.WithTransaction(func(ctx context.Context) {
		req := fasthttp.AcquireRequest()
		defer fasthttp.ReleaseRequest(req)
		apmcore.InjectFastHTTPTraceContext(ctx, req)
		if got := string(req.Header.Peek("Traceparent")); got == "" {
			t.Error("expected traceparent header to be set")
		}
	})
	// No-ops should not panic.
	apmcore.InjectFastHTTPTraceContext(nil, nil)
	apmcore.InjectFastHTTPTraceContext(context.Background(), nil)
	r := fasthttp.AcquireRequest()
	apmcore.InjectFastHTTPTraceContext(context.Background(), r) // no tx → silent
	fasthttp.ReleaseRequest(r)
}
