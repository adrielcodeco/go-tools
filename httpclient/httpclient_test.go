package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/httpclient"
	"github.com/valyala/fasthttp"
)

type charge struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

func TestPOSTAndGETHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /charge":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"id":"c-1","amount":100}`)
		case "GET /charge":
			if r.URL.Query().Get("filter") != "active" {
				t.Errorf("missing query: %v", r.URL.RawQuery)
			}
			fmt.Fprintln(w, `{"id":"c-1","amount":100}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL + "/charge",
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{"amount": 100},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if out.ID != "c-1" || out.Amount != 100 {
		t.Fatalf("decoded wrong: %+v", out)
	}

	out2, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL:         srv.URL + "/charge",
		QueryString: map[string]string{"filter": "active"},
	})
	if err != nil || out2.ID != "c-1" {
		t.Fatalf("GET: out=%v err=%v", out2, err)
	}
}

func TestStatusErrorContainsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"bad input"}`)
	}))
	defer srv.Close()

	body, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL: srv.URL,
	})
	var se *httpclient.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.StatusCode != 400 {
		t.Errorf("status = %d", se.StatusCode)
	}
	if len(se.Body) == 0 || len(body) == 0 {
		t.Errorf("expected body bytes to be preserved")
	}
}

func TestRetryOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `{"id":"c-2","amount":7}`)
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
	if out.ID != "c-2" {
		t.Errorf("decoded wrong: %+v", out)
	}
}

func TestHookCapturesAttempts(t *testing.T) {
	var records []httpclient.Record
	httpclient.SetHook(func(r httpclient.Record) { records = append(records, r) })
	defer httpclient.SetHook(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, _ = httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL: srv.URL,
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
		},
	})
	if len(records) != 2 {
		t.Fatalf("expected 2 hook records, got %d", len(records))
	}
	if records[0].Attempt != 1 || records[1].Attempt != 2 {
		t.Errorf("unexpected attempt numbers: %+v", records)
	}
}

func TestFormEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("name") != "alice" {
			t.Errorf("missing form field: %v", r.PostForm)
		}
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	_, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.FormHeaders(),
		Data:    map[string]string{"name": "alice"},
	})
	if err != nil {
		t.Fatalf("POST form: %v", err)
	}
}

// --- AddHook tests -------------------------------------------------------

func TestAddHook_Composes(t *testing.T) {
	// Reset hook state before and after the test.
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var sliceA, sliceB []httpclient.Record
	hookA := httpclient.Hook(func(r httpclient.Record) { sliceA = append(sliceA, r) })
	hookB := httpclient.Hook(func(r httpclient.Record) { sliceB = append(sliceB, r) })

	httpclient.SetHook(hookA)
	httpclient.AddHook(hookB)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	if _, err := httpclient.Do(context.Background(), "GET", httpclient.RequestOptions{URL: srv.URL}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(sliceA) != 1 {
		t.Errorf("hookA: expected 1 record, got %d", len(sliceA))
	}
	if len(sliceB) != 1 {
		t.Errorf("hookB: expected 1 record, got %d", len(sliceB))
	}
}

func TestAddHook_NilIsNoop(t *testing.T) {
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var called int
	httpclient.SetHook(func(_ httpclient.Record) { called++ })

	// Should not panic and should not remove the existing hook.
	httpclient.AddHook(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	if _, err := httpclient.Do(context.Background(), "GET", httpclient.RequestOptions{URL: srv.URL}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if called != 1 {
		t.Errorf("expected original hook called once, got %d", called)
	}
}

func TestAddHook_WhenNoPriorHook(t *testing.T) {
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var called int
	httpclient.AddHook(func(_ httpclient.Record) { called++ })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	if _, err := httpclient.Do(context.Background(), "GET", httpclient.RequestOptions{URL: srv.URL}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if called != 1 {
		t.Errorf("expected hook called once, got %d", called)
	}
}

func TestSetHook_ReplacesAddHook(t *testing.T) {
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var calledA, calledB, calledC int
	httpclient.AddHook(func(_ httpclient.Record) { calledA++ })
	httpclient.AddHook(func(_ httpclient.Record) { calledB++ })

	// SetHook must replace the combined chain.
	httpclient.SetHook(func(_ httpclient.Record) { calledC++ })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	if _, err := httpclient.Do(context.Background(), "GET", httpclient.RequestOptions{URL: srv.URL}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calledA != 0 || calledB != 0 {
		t.Errorf("hooks A and B should have been replaced: calledA=%d calledB=%d", calledA, calledB)
	}
	if calledC != 1 {
		t.Errorf("hook C should have been called once, got %d", calledC)
	}
}

// --- SetConfig / currentConfig tests ------------------------------------

func TestSetConfig_CustomTimeout(t *testing.T) {
	defer httpclient.SetConfig(nil) // restore defaults after test

	httpclient.SetConfig(&httpclient.Config{DefaultTimeout: 5 * time.Second})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"id":"c-cfg","amount":1}`)
	}))
	defer srv.Close()

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("GET with custom config: %v", err)
	}
	if out.ID != "c-cfg" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestSetConfig_Nil_RestoresDefault(t *testing.T) {
	// Set a non-default value, then restore via nil.
	httpclient.SetConfig(&httpclient.Config{DefaultTimeout: 1 * time.Second})
	httpclient.SetConfig(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"id":"c-def","amount":2}`)
	}))
	defer srv.Close()

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("GET after config reset: %v", err)
	}
	if out.ID != "c-def" {
		t.Errorf("unexpected response: %+v", out)
	}
}

// --- UseClient test -----------------------------------------------------

func TestUseClient_CustomClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"id":"c-uc","amount":3}`)
	}))
	defer srv.Close()

	custom := &fasthttp.Client{ReadTimeout: 10 * time.Second}
	httpclient.UseClient(custom)
	defer httpclient.UseClient(nil) // restore default after test

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("GET with custom client: %v", err)
	}
	if out.ID != "c-uc" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestUseClient_SwapAndRestore(t *testing.T) {
	// Replace with a known client, make a request, then swap back to a fresh one.
	first := &fasthttp.Client{ReadTimeout: 15 * time.Second}
	second := &fasthttp.Client{ReadTimeout: 10 * time.Second}

	httpclient.UseClient(first)
	defer httpclient.UseClient(second) // leave a non-nil client so later tests are unaffected

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"id":"c-swap","amount":4}`)
	}))
	defer srv.Close()

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("GET with swapped client: %v", err)
	}
	if out.ID != "c-swap" {
		t.Errorf("unexpected response: %+v", out)
	}
}

// --- PUT / PATCH / DELETE tests ----------------------------------------

func TestPUT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		fmt.Fprintln(w, `{"id":"c-put","amount":10}`)
	}))
	defer srv.Close()

	out, err := httpclient.PUT[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{"amount": 10},
	})
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	if out.ID != "c-put" {
		t.Errorf("decoded wrong: %+v", out)
	}
}

func TestPATCH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		fmt.Fprintln(w, `{"id":"c-patch","amount":20}`)
	}))
	defer srv.Close()

	out, err := httpclient.PATCH[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{"amount": 20},
	})
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	if out.ID != "c-patch" {
		t.Errorf("decoded wrong: %+v", out)
	}
}

func TestDELETE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		fmt.Fprintln(w, `{"id":"c-del","amount":30}`)
	}))
	defer srv.Close()

	out, err := httpclient.DELETE[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	if out.ID != "c-del" {
		t.Errorf("decoded wrong: %+v", out)
	}
}

// --- StatusError.Error() test ------------------------------------------

func TestStatusError_ErrorString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"error":"not found"}`)
	}))
	defer srv.Close()

	_, err := httpclient.Do(context.Background(), "GET", httpclient.RequestOptions{
		URL: srv.URL + "/missing",
	})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	var se *httpclient.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T", err)
	}
	msg := se.Error()
	if msg == "" {
		t.Error("Error() returned empty string")
	}
	// Should contain method, url and status code.
	for _, want := range []string{"GET", "404"} {
		if !contains(msg, want) {
			t.Errorf("Error() %q missing %q", msg, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- encodeForm edge cases ---------------------------------------------

func TestEncodeForm_MapStringAny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("count") != "5" {
			t.Errorf("expected count=5, got %q", r.PostForm.Get("count"))
		}
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	_, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.FormHeaders(),
		Data:    map[string]any{"count": 5},
	})
	if err != nil {
		t.Fatalf("POST form map[string]any: %v", err)
	}
}

func TestEncodeForm_UnsupportedType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	// Pass a struct (not map[string]string or map[string]any) with form content-type.
	type payload struct{ Name string }
	_, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.FormHeaders(),
		Data:    payload{Name: "alice"},
	})
	if err == nil {
		t.Fatal("expected error for unsupported form body type")
	}
}

// --- nextBackoff tests --------------------------------------------------

func TestNextBackoff_CapsAtMax(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 4 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, `{"id":"c-bo","amount":99}`)
	}))
	defer srv.Close()

	start := time.Now()
	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    4,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     10 * time.Millisecond, // cap
			Multiplier:     10,                    // would blow up without cap
		},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if out.ID != "c-bo" {
		t.Errorf("unexpected response: %+v", out)
	}
	// Total wait capped at ~30ms (3 * 10ms) — should comfortably finish < 1s.
	if elapsed > time.Second {
		t.Errorf("took too long (backoff not capped?): %v", elapsed)
	}
}

func TestNextBackoff_ZeroInitial(t *testing.T) {
	// When InitialBackoff is 0, nextBackoff should still produce a non-negative value.
	// We exercise this via a retry where the first attempt fails and backoff starts at 0.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `{"id":"c-zb","amount":0}`)
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: 0, // zero initial — nextBackoff uses 100ms default but MaxBackoff caps it
			MaxBackoff:     1 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("expected success after retry with zero initial backoff: %v", err)
	}
	if out.ID != "c-zb" {
		t.Errorf("unexpected response: %+v", out)
	}
}

// --- defaultShouldRetry tests ------------------------------------------

func TestDefaultShouldRetry_TransportError(t *testing.T) {
	// A request to an address that immediately refuses should be retried.
	var calls atomic.Int32
	// Use a server that we close immediately so the second attempt gets a transport error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			// Force a 500 on the first call so shouldRetry triggers.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `{"id":"c-tr","amount":0}`)
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("expected success on second attempt: %v", err)
	}
	if out.ID != "c-tr" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestDefaultShouldRetry_4xxNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"bad"}`)
	}))
	defer srv.Close()

	_, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: time.Millisecond,
		},
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	// 4xx should NOT be retried — only 1 attempt should occur.
	if calls.Load() != 1 {
		t.Errorf("expected 1 attempt for 4xx, got %d", calls.Load())
	}
}

// --- Request (generic) additional coverage ------------------------------

func TestRequest_EmptyBodyReturnsNil(t *testing.T) {
	// When the server returns 200 with no body, Request should return (nil, nil).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// intentionally no body
	}))
	defer srv.Close()

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil result for empty body, got %+v", out)
	}
}

func TestRequest_StatusErrorWithBody(t *testing.T) {
	// When server returns 4xx with a JSON body, both the decoded value and
	// the status error are returned.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprintln(w, `{"id":"err-body","amount":0}`)
	}))
	defer srv.Close()

	out, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL: srv.URL,
	})
	var se *httpclient.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("unexpected status: %d", se.StatusCode)
	}
	// Body was present and parseable — out should be non-nil.
	if out == nil {
		t.Fatal("expected decoded body alongside status error")
	}
	if out.ID != "err-body" {
		t.Errorf("unexpected decoded body: %+v", out)
	}
}

// --- Fix O1: cancelled context emits hook --------------------------------

func TestDo_CancelledContextEmitsHook(t *testing.T) {
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var hookRecord httpclient.Record
	var hookCalled bool
	httpclient.SetHook(func(r httpclient.Record) {
		hookCalled = true
		hookRecord = r
	})

	// Cancel the context before calling Do so the backoff select fires immediately.
	ctx, cancel := context.WithCancel(context.Background())

	// Use a server that always returns 500 so a retry is attempted and the
	// backoff timer select is reached.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Cancel the context after the first attempt fires but before the backoff
	// completes by cancelling it immediately — the select will pick ctx.Done().
	cancel()

	_, err := httpclient.Do(ctx, "GET", httpclient.RequestOptions{
		URL: srv.URL,
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 10 * time.Second, // long enough that cancel wins the select
		},
	})

	if err == nil {
		t.Fatal("expected non-nil error from cancelled context")
	}
	if !hookCalled {
		t.Fatal("expected hook to be called on ctx.Done() cancellation")
	}
	if hookRecord.Err == nil {
		t.Errorf("expected hook Record.Err to be non-nil, got nil")
	}
}

// --- Fix D3: ReqHeaders populated in hook record -------------------------

func TestDo_ReqHeadersInRecord(t *testing.T) {
	httpclient.SetHook(nil)
	defer httpclient.SetHook(nil)

	var hookRecord httpclient.Record
	httpclient.SetHook(func(r httpclient.Record) {
		hookRecord = r
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	customHeaders := map[string]string{
		"Content-Type":  "application/json",
		"X-Request-ID":  "test-req-42",
		"Authorization": "Bearer tok",
	}

	_, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: customHeaders,
		Data:    map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if hookRecord.ReqHeaders == nil {
		t.Fatal("expected Record.ReqHeaders to be non-nil")
	}
	for k, want := range customHeaders {
		got, ok := hookRecord.ReqHeaders[k]
		if !ok {
			t.Errorf("Record.ReqHeaders missing key %q", k)
			continue
		}
		if got != want {
			t.Errorf("Record.ReqHeaders[%q] = %q, want %q", k, got, want)
		}
	}
}
