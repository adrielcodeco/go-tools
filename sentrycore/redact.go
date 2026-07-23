package sentrycore

import (
	"net/url"
	"strings"
)

// filtered is the placeholder substituted for a sensitive value.
const filtered = "[Filtered]"

// sensitiveHeaders is the case-insensitive denylist of request/response
// header names whose values must never reach Sentry (or any external
// service). Kept here so both Fiber adapters share one policy.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
	"x-auth-token":        true,
	"x-csrf-token":        true,
	"x-xsrf-token":        true,
	"x-session-token":     true,
}

// sensitiveQueryParams is the case-insensitive denylist of query-string
// parameter names commonly used to carry credentials.
var sensitiveQueryParams = map[string]bool{
	"access_token":  true,
	"token":         true,
	"api_key":       true,
	"apikey":        true,
	"password":      true,
	"secret":        true,
	"code":          true,
	"id_token":      true,
	"refresh_token": true,
}

// RedactHeader reports the value to store for a header, replacing the
// values of sensitive headers with a placeholder. The key match is
// case-insensitive.
func RedactHeader(key, value string) string {
	if sensitiveHeaders[strings.ToLower(key)] {
		return filtered
	}
	return value
}

// RedactQueryString returns raw with the values of sensitive query
// parameters replaced by a placeholder. Parameter names are preserved so
// the shape of the request stays visible. If raw cannot be parsed it is
// dropped entirely (returns ""), since an unparseable query is more
// likely to leak than to inform.
func RedactQueryString(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return ""
	}
	for k := range values {
		if sensitiveQueryParams[strings.ToLower(k)] {
			for i := range values[k] {
				values[k][i] = filtered
			}
		}
	}
	return values.Encode()
}
