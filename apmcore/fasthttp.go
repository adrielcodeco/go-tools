package apmcore

import (
	"context"
	"net/http"
	"net/url"

	"github.com/valyala/fasthttp"
	apmhttp "go.elastic.co/apm/module/apmhttp/v2"
	apm "go.elastic.co/apm/v2"
)

// TraceFastHTTPCall wraps a single fasthttp client call with an APM exit
// span. It injects the W3C traceparent header on req, runs do, records
// the HTTP status code, captures errors, and ends the span.
//
// Use it with any *fasthttp.Client / *fasthttp.HostClient / *PipelineClient
// method that accepts (*Request, *Response):
//
//	err := apmcore.TraceFastHTTPCall(ctx, req, resp, func() error {
//	    return client.Do(req, resp)
//	})
//
// For DoTimeout / DoDeadline / DoRedirects, pass the matching closure.
// Inside a retry loop, call TraceFastHTTPCall on the *innermost* attempt
// so each retry produces its own sibling span (matching the apmhttp
// retry-pattern guidance).
//
// If ctx has no active APM transaction the call still runs; the span
// is silently dropped.
func TraceFastHTTPCall(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response, do func() error) error {
	if req == nil || do == nil {
		if do != nil {
			return do()
		}
		return nil
	}

	name := string(req.Header.Method()) + " " + string(req.URI().Host())
	span, ctx := apm.StartSpanOptions(ctx, name, "external.http", apm.SpanOptions{ExitSpan: true})
	if span == nil || span.Dropped() {
		// No active transaction or sampler dropped — inject nothing, just call.
		if span != nil {
			span.End()
		}
		return do()
	}
	defer span.End()

	span.Context.SetHTTPRequest(buildStdlibHTTPRequest(req))
	injectTraceparent(req, span.TraceContext())

	err := do()

	if resp != nil {
		span.Context.SetHTTPStatusCode(resp.StatusCode())
	}
	if err != nil {
		if e := apm.CaptureError(ctx, err); e != nil {
			e.Send()
		}
	}
	return err
}

// InjectFastHTTPTraceContext writes the W3C traceparent header on req
// from the currently active APM span (or transaction) in ctx. Use this
// when you must hand the *fasthttp.Request off to a library you do not
// control — so you can't wrap the call site with TraceFastHTTPCall.
//
// No-op when ctx has neither span nor transaction.
func InjectFastHTTPTraceContext(ctx context.Context, req *fasthttp.Request) {
	if ctx == nil || req == nil {
		return
	}
	if span := apm.SpanFromContext(ctx); span != nil {
		injectTraceparent(req, span.TraceContext())
		return
	}
	if tx := apm.TransactionFromContext(ctx); tx != nil {
		injectTraceparent(req, tx.TraceContext())
	}
}

// buildStdlibHTTPRequest constructs a minimal *http.Request that
// SetHTTPRequest can consume — apm only reads URL, Host, Method, and a
// few headers. Body is not copied.
func buildStdlibHTTPRequest(req *fasthttp.Request) *http.Request {
	u := &url.URL{
		Scheme: string(req.URI().Scheme()),
		Host:   string(req.URI().Host()),
		Path:   string(req.URI().Path()),
	}
	if rq := string(req.URI().QueryString()); rq != "" {
		u.RawQuery = rq
	}
	h := http.Header{}
	req.Header.VisitAll(func(k, v []byte) {
		h.Set(string(k), string(v))
	})
	return &http.Request{
		Method: string(req.Header.Method()),
		URL:    u,
		Host:   string(req.URI().Host()),
		Header: h,
	}
}

func injectTraceparent(req *fasthttp.Request, tc apm.TraceContext) {
	if tc.Trace.Validate() != nil {
		return
	}
	value := apmhttp.FormatTraceparentHeader(tc)
	req.Header.Set(apmhttp.W3CTraceparentHeader, value)
	req.Header.Set(apmhttp.ElasticTraceparentHeader, value)
	if ts := tc.State.String(); ts != "" {
		req.Header.Set(apmhttp.TracestateHeader, ts)
	}
}
