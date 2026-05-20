// Package httpclient is a thin, generics-friendly fasthttp client wrapper
// with built-in Elastic APM instrumentation (via apmcore), pluggable
// structured-log hook, and optional retry-with-backoff.
//
// Top-level functions (GET[O], POST[O], …) cover the 99% case:
//
//	type charge struct{ ID string `json:"id"` }
//	out, err := httpclient.POST[charge](ctx, httpclient.RequestOptions{
//	    URL:     "https://api.example.com/charge",
//	    Headers: httpclient.JSONHeaders(),
//	    Data:    map[string]any{"amount": 100},
//	})
//
// Configure once at boot:
//
//	httpclient.UseClient(&fasthttp.Client{ReadTimeout: 10*time.Second})
//	httpclient.SetHook(func(r httpclient.Record) {
//	    logger.LogCtx(r.Ctx).Info("← outgoing", zap.Any("outgoing", buildOutgoing(r)))
//	})
//
// All requests are wrapped in an APM exit span and propagate the active
// transaction's traceparent header automatically.
package httpclient

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"

	"github.com/adrielcodeco/go-tools/apmcore"
)

// RequestOptions describes a single outgoing call. All fields are
// optional except URL.
type RequestOptions struct {
	URL         string
	QueryString map[string]string
	Headers     map[string]string

	// Data is the request body. Any value is marshalled with sonic
	// (JSON) by default; when Headers["Content-Type"] is
	// "application/x-www-form-urlencoded" and Data is a map[string]any
	// or map[string]string, it is form-encoded instead.
	Data any

	// Timeout overrides the per-call timeout. Zero falls back to
	// Config.DefaultTimeout.
	Timeout time.Duration

	// Retry policy. Zero attempts → no retry (one call). Each attempt
	// produces its own APM span.
	Retry RetryPolicy
}

// RetryPolicy describes how many times to retry a failed call and how
// to wait between attempts.
type RetryPolicy struct {
	// MaxAttempts is the total number of calls (1 = no retry).
	MaxAttempts int
	// InitialBackoff is the wait before the second attempt.
	InitialBackoff time.Duration
	// MaxBackoff caps the per-attempt wait.
	MaxBackoff time.Duration
	// Multiplier scales the backoff after each attempt (default 2 if 0).
	Multiplier float64
	// ShouldRetry decides whether err+status warrants another attempt.
	// Nil → retry on transport error or 5xx.
	ShouldRetry func(status int, err error) bool
}

// Record carries everything a hook (logger / auditor) needs to know
// about an outgoing call.
type Record struct {
	Ctx          context.Context
	Method       string
	URL          string
	Status       int
	ResponseTime time.Duration
	ReqHeaders   map[string]string
	ReqBody      []byte // bytes that hit the wire (already JSON or form-encoded)
	ResHeaders   map[string]string
	ResBody      []byte
	Err          error
	Attempt      int // 1-based; >1 means this is a retry
}

// Hook is invoked once per attempt — including retried ones — right
// after the call returns. Hooks must not panic; use them for logging
// or metrics, not control flow.
type Hook func(Record)

// --- package-level globals --------------------------------------------

var (
	clientPtr atomic.Pointer[fasthttp.Client]
	hookPtr   atomic.Pointer[Hook]
	cfg       atomic.Pointer[Config]

	initOnce sync.Once
)

// Config tunes package defaults. Apply via SetConfig.
type Config struct {
	DefaultTimeout time.Duration // default 30s; zero means no timeout
}

func defaultConfig() *Config { return &Config{DefaultTimeout: 30 * time.Second} }

// SetConfig replaces the active configuration. Pass nil to restore
// defaults.
func SetConfig(c *Config) {
	if c == nil {
		cfg.Store(defaultConfig())
		return
	}
	cp := *c
	cfg.Store(&cp)
}

func currentConfig() *Config {
	if c := cfg.Load(); c != nil {
		return c
	}
	return defaultConfig()
}

// UseClient swaps the underlying *fasthttp.Client used by all top-level
// helpers. Pass nil to restore the default.
func UseClient(c *fasthttp.Client) {
	clientPtr.Store(c)
}

func currentClient() *fasthttp.Client {
	if c := clientPtr.Load(); c != nil {
		return c
	}
	initOnce.Do(func() {
		clientPtr.CompareAndSwap(nil, &fasthttp.Client{
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		})
	})
	return clientPtr.Load()
}

// SetHook installs a global hook called after every attempt, replacing any
// previously installed hook. Pass nil to disable.
// To add a hook without removing an existing one, use AddHook instead.
func SetHook(h Hook) {
	if h == nil {
		hookPtr.Store(nil)
		return
	}
	hookPtr.Store(&h)
}

// AddHook appends h to the current hook chain. If no hook is installed,
// h becomes the sole hook. Subsequent calls compose without dropping prior
// hooks — safe to call multiple times at boot (e.g. log hook + audit hook).
func AddHook(h Hook) {
	if h == nil {
		return
	}
	prev := currentHook()
	if prev == nil {
		SetHook(h)
		return
	}
	combined := Hook(func(r Record) { prev(r); h(r) })
	hookPtr.Store(&combined)
}

func currentHook() Hook {
	if h := hookPtr.Load(); h != nil {
		return *h
	}
	return nil
}

// JSONHeaders returns a map with Content-Type: application/json. Useful
// shorthand in RequestOptions.Headers.
func JSONHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/json"}
}

// FormHeaders returns a map with Content-Type: application/x-www-form-urlencoded.
func FormHeaders() map[string]string {
	return map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
}

// --- low-level call ---------------------------------------------------

// Do executes a single fasthttp call described by opts. Returns the raw
// response body and an error. Status codes outside [200, 300) produce
// a non-nil error but the body is still returned.
//
// The call is wrapped in an APM exit span via apmcore.TraceFastHTTPCall
// and the configured Hook is invoked once per attempt.
func Do(ctx context.Context, method string, opts RequestOptions) ([]byte, error) {
	if opts.Retry.MaxAttempts <= 1 {
		return doOnce(ctx, method, opts, 1)
	}

	mult := opts.Retry.Multiplier
	if mult == 0 {
		mult = 2
	}
	backoff := opts.Retry.InitialBackoff
	shouldRetry := opts.Retry.ShouldRetry
	if shouldRetry == nil {
		shouldRetry = defaultShouldRetry
	}

	var (
		body []byte
		err  error
	)
	for attempt := 1; attempt <= opts.Retry.MaxAttempts; attempt++ {
		body, err = doOnce(ctx, method, opts, attempt)
		status := 0
		if err != nil {
			if se, ok := err.(*StatusError); ok {
				status = se.StatusCode
			}
		} else {
			status = 200
		}
		if !shouldRetry(status, err) || attempt == opts.Retry.MaxAttempts {
			return body, err
		}
		if backoff > 0 {
			t := time.NewTimer(backoff)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				ctxErr := ctx.Err()
				emitHook(ctx, method, opts.URL, opts.Headers, nil, nil, 0, 0, ctxErr, attempt)
				return body, ctxErr
			}
		}
		backoff = nextBackoff(backoff, mult, opts.Retry.MaxBackoff)
	}
	return body, err
}

func nextBackoff(cur time.Duration, mult float64, max time.Duration) time.Duration {
	if cur <= 0 {
		cur = 100 * time.Millisecond
	}
	next := time.Duration(float64(cur) * mult)
	if max > 0 && next > max {
		return max
	}
	return next
}

func defaultShouldRetry(status int, err error) bool {
	if err != nil {
		if _, ok := err.(*StatusError); !ok {
			return true // transport-level
		}
	}
	return status >= 500
}

// StatusError indicates the request reached the server but the status
// code is outside [200, 300). Body is preserved.
type StatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       []byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpclient: %s %s → %d", e.Method, e.URL, e.StatusCode)
}

func doOnce(ctx context.Context, method string, opts RequestOptions, attempt int) ([]byte, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	finalURL, err := formatURL(opts.URL, opts.QueryString)
	if err != nil {
		emitHook(ctx, method, opts.URL, opts.Headers, nil, nil, 0, 0, err, attempt)
		return nil, err
	}
	req.SetRequestURI(finalURL)
	req.Header.SetMethod(method)

	body, err := encodeBody(opts.Data, opts.Headers)
	if err != nil {
		emitHook(ctx, method, finalURL, opts.Headers, nil, nil, 0, 0, err, attempt)
		return nil, err
	}
	if body != nil {
		req.SetBody(body)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = currentConfig().DefaultTimeout
	}

	start := time.Now()
	callErr := apmcore.TraceFastHTTPCall(ctx, req, resp, func() error {
		if timeout > 0 {
			return currentClient().DoTimeout(req, resp, timeout)
		}
		return currentClient().Do(req, resp)
	})
	elapsed := time.Since(start)

	respBody := append([]byte(nil), resp.Body()...) // copy, fasthttp reuses the buffer
	status := resp.StatusCode()

	if callErr != nil {
		emitHook(ctx, method, finalURL, opts.Headers, body, respBody, status, elapsed, callErr, attempt)
		return respBody, callErr
	}

	if status < 200 || status >= 300 {
		se := &StatusError{Method: method, URL: finalURL, StatusCode: status, Body: respBody}
		emitHook(ctx, method, finalURL, opts.Headers, body, respBody, status, elapsed, se, attempt)
		return respBody, se
	}

	emitHook(ctx, method, finalURL, opts.Headers, body, respBody, status, elapsed, nil, attempt)
	return respBody, nil
}

func emitHook(ctx context.Context, method, urlStr string, reqHeaders map[string]string, reqBody, resBody []byte, status int, dur time.Duration, err error, attempt int) {
	h := currentHook()
	if h == nil {
		return
	}
	h(Record{
		Ctx:          ctx,
		Method:       method,
		URL:          urlStr,
		Status:       status,
		ResponseTime: dur,
		ReqHeaders:   reqHeaders,
		ReqBody:      reqBody,
		ResBody:      resBody,
		Err:          err,
		Attempt:      attempt,
	})
}

// --- body / url helpers -----------------------------------------------

func formatURL(rawURL string, qs map[string]string) (string, error) {
	if len(qs) == 0 {
		return rawURL, nil
	}
	values := url.Values{}
	for k, v := range qs {
		values.Set(k, v)
	}
	encoded := values.Encode()
	if encoded == "" {
		return rawURL, nil
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + encoded, nil
}

func encodeBody(data any, headers map[string]string) ([]byte, error) {
	if data == nil {
		return nil, nil
	}
	if ct := headers["Content-Type"]; ct == "application/x-www-form-urlencoded" {
		return encodeForm(data)
	}
	return sonic.Marshal(data)
}

func encodeForm(data any) ([]byte, error) {
	values := url.Values{}
	switch m := data.(type) {
	case map[string]string:
		for k, v := range m {
			values.Set(k, v)
		}
	case map[string]any:
		for k, v := range m {
			values.Set(k, fmt.Sprintf("%v", v))
		}
	default:
		return nil, fmt.Errorf("httpclient: form-encoded body must be map[string]string or map[string]any, got %T", data)
	}
	return []byte(values.Encode()), nil
}
