package sentrycore

import (
	"net/http"
	"time"

	sentry "github.com/getsentry/sentry-go"
)

// breadcrumbTransport records each outgoing request as a Sentry
// breadcrumb on the hub bound to the request context, so a later error
// carries the recent HTTP calls that led up to it.
type breadcrumbTransport struct{ base http.RoundTripper }

// WrapHTTPTransport wraps base so every outgoing request built with
// http.NewRequestWithContext leaves an "http" breadcrumb (method, URL,
// status code) on the request's Sentry hub. It mirrors
// apmcore.WrapHTTPTransport and is meant to compose with it:
//
//	rt := apmcore.WrapHTTPTransport(nil)      // APM spans + traceparent
//	rt = sentrycore.WrapHTTPTransport(rt)     // + Sentry breadcrumbs
//	client := &http.Client{Transport: rt}
//
// If base is nil, http.DefaultTransport is wrapped. When Sentry is
// disabled the wrapper still forwards the request; recording a breadcrumb
// on a no-op hub is harmless and cheap.
func WrapHTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &breadcrumbTransport{base: base}
}

func (t *breadcrumbTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)

	if enabled {
		hub := hubFromContext(req.Context())
		data := map[string]any{
			"method": req.Method,
			"url":    req.URL.String(),
		}
		level := sentry.LevelInfo
		if err != nil {
			level = sentry.LevelError
			data["error"] = err.Error()
		} else if resp != nil {
			data["status_code"] = resp.StatusCode
			if resp.StatusCode >= 500 {
				level = sentry.LevelError
			} else if resp.StatusCode >= 400 {
				level = sentry.LevelWarning
			}
		}
		hub.AddBreadcrumb(&sentry.Breadcrumb{
			Type:      "http",
			Category:  "http",
			Level:     level,
			Data:      data,
			Timestamp: time.Now(),
		}, nil)
	}

	return resp, err
}
