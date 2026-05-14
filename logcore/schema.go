package logcore

import (
	"strings"

	"github.com/bytedance/sonic"
)

// Req is the request side of an incoming or outgoing log entry. Fields
// stay omitempty so the JSON in Kibana is compact.
type Req struct {
	Params      any `json:"params,omitempty"`
	QueryString any `json:"queryString,omitempty"`
	Headers     any `json:"headers,omitempty"`
	Body        any `json:"body,omitempty"`
}

// Res is the response side. StatusCode is a string ("Ø" when unknown)
// to match the pattern used in existing dashboards.
type Res struct {
	Headers    any    `json:"headers,omitempty"`
	Body       any    `json:"body,omitempty"`
	StatusCode string `json:"statusCode"`
}

// Incoming is the payload published under the "incoming" key by the
// Fiber middleware.
type Incoming struct {
	Req          *Req   `json:"req"`
	Res          *Res   `json:"res"`
	ResponseTime string `json:"responseTime"`
}

// Outgoing is the payload published under the "outgoing" key by the
// httpclient hook.
type Outgoing struct {
	Req          *Req    `json:"req"`
	Res          *Res    `json:"res"`
	Error        *string `json:"error,omitempty"`
	ResponseTime string  `json:"responseTime"`
}

// FlattenHeaders converts a map[string][]string headers shape into a
// flat map[string]string by joining multi-valued entries with ",".
// Returns nil when the input is empty so the JSON encoder drops it.
func FlattenHeaders(h map[string][]string) any {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		switch len(v) {
		case 0:
		case 1:
			out[k] = v[0]
		default:
			out[k] = strings.Join(v, ",")
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DecodeJSONBody attempts to unmarshal raw as JSON. On failure (non-JSON
// payload, empty, malformed) it returns the raw bytes as a string so
// the log entry still carries something useful. nil bytes → nil.
func DecodeJSONBody(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any)
	if err := sonic.Unmarshal(raw, &out); err == nil {
		return out
	}
	// Try array — APIs sometimes return [{}, {}].
	var arr []any
	if err := sonic.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	return string(raw)
}
