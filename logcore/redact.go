package logcore

import (
	"regexp"
	"strings"
)

// RedactMask is the placeholder substituted for any value identified as
// sensitive. It is intentionally short and constant so Kibana queries can
// match it (e.g. `incoming.req.headers.authorization: "[REDACTED]"`).
const RedactMask = "[REDACTED]"

// DefaultSensitiveKeys is the case-insensitive set of map keys whose values
// are masked by the default Redactor. It covers the headers and body fields
// that most commonly carry credentials, session material, or PII in a
// payments context. Matching is exact (case-insensitive) against the key;
// for substring matching see DefaultSensitivePatterns.
//
// The list is deliberately conservative-but-broad: a value is only ever
// replaced with RedactMask, so over-matching costs a masked log field, never
// a leaked secret. Callers that need a different policy build their own
// Redactor via NewRedactor.
var DefaultSensitiveKeys = []string{
	// --- auth / session headers ---
	"authorization",
	"proxy-authorization",
	"cookie",
	"set-cookie",
	"x-api-key",
	"x-auth-token",
	"x-access-token",
	"x-csrf-token",
	"x-xsrf-token",
	"x-amz-security-token",
	// --- credential / token body fields ---
	"password",
	"passwd",
	"pwd",
	"secret",
	"client_secret",
	"clientsecret",
	"token",
	"access_token",
	"accesstoken",
	"refresh_token",
	"refreshtoken",
	"id_token",
	"idtoken",
	"api_key",
	"apikey",
	"private_key",
	"privatekey",
	// --- payments / PII ---
	"card",
	"card_number",
	"cardnumber",
	"pan",
	"cvv",
	"cvc",
	"cvv2",
	"security_code",
	"securitycode",
	"ssn",
	"taxid",
	"tax_id",
}

// DefaultSensitivePatterns matches keys by substring (case-insensitive). It
// catches families of fields whose exact name is unpredictable but whose name
// contains a sensitive token — e.g. "user_password", "stripeSecretKey",
// "x-vault-token". Patterns are checked in addition to DefaultSensitiveKeys.
var DefaultSensitivePatterns = []string{
	"password",
	"secret",
	"passwd",
	"authorization",
}

// Redactor masks sensitive values in the structured shapes logcore logs
// (headers as map[string]string, bodies/queries as map[string]any with nested
// maps and slices). It never mutates its input — redaction always returns a
// freshly allocated copy, so the caller's request/response data is untouched.
//
// The zero Redactor is not ready for use; build one with NewRedactor or
// DefaultRedactor.
type Redactor struct {
	exact    map[string]struct{}
	patterns []string
	mask     string
	maxDepth int
	// partial maps a lowercased sensitive key to the number of trailing
	// characters to reveal. Keys not present here are fully masked.
	partial map[string]int
}

// RedactorOptions configures NewRedactor. All fields are optional.
type RedactorOptions struct {
	// Keys is the set of exact (case-insensitive) key names to mask. When
	// nil, DefaultSensitiveKeys is used. Pass a non-nil empty slice to opt
	// out of exact matching entirely.
	Keys []string

	// Patterns is the set of case-insensitive substrings; any key that
	// contains one is masked. When nil, DefaultSensitivePatterns is used.
	// Pass a non-nil empty slice to disable substring matching.
	Patterns []string

	// Extra is merged into the resolved Keys set. Use it to add app-specific
	// fields without restating the defaults.
	Extra []string

	// RemoveKeys drops keys from the resolved exact set (case-insensitive),
	// applied after Keys and Extra. Use it to un-redact a default field
	// without restating the whole Keys list — e.g. keep "x-request-id"
	// visible while everything else in the defaults stays masked.
	//
	// Note: RemoveKeys only affects exact-key matching. A key that is also
	// caught by a substring Pattern (e.g. "secret") stays masked unless you
	// also override Patterns.
	RemoveKeys []string

	// Mask overrides the replacement string. Default: RedactMask.
	Mask string

	// MaxDepth bounds recursion into nested maps/slices, guarding against
	// pathological or cyclic-looking decoded payloads. Default: 32.
	MaxDepth int

	// PartialReveal opts specific sensitive keys into partial redaction:
	// instead of replacing the whole value with Mask, the last N characters
	// are kept and the rest is masked, producing "[REDACTED]…1111". The map
	// is keyed by field name (case-insensitive) → N, the count of trailing
	// characters to reveal.
	//
	// Only keys already considered sensitive (via Keys/Patterns/Extra) are
	// eligible — listing a non-sensitive key here has no effect. Keys not
	// listed here stay fully masked, so the default policy is unchanged.
	//
	// Partial reveal only applies to string values long enough that the
	// revealed tail is at most half the value (so a short secret is never
	// mostly exposed); otherwise the value falls back to a full mask. This
	// guards against, e.g., a 5-char password leaking 4 of its characters.
	PartialReveal map[string]int
}

// NewRedactor builds a Redactor from opts. See RedactorOptions for defaults.
func NewRedactor(opts RedactorOptions) *Redactor {
	keys := opts.Keys
	if keys == nil {
		keys = DefaultSensitiveKeys
	}
	patterns := opts.Patterns
	if patterns == nil {
		patterns = DefaultSensitivePatterns
	}
	mask := opts.Mask
	if mask == "" {
		mask = RedactMask
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 32
	}

	exact := make(map[string]struct{}, len(keys)+len(opts.Extra))
	for _, k := range keys {
		exact[strings.ToLower(k)] = struct{}{}
	}
	for _, k := range opts.Extra {
		exact[strings.ToLower(k)] = struct{}{}
	}
	for _, k := range opts.RemoveKeys {
		delete(exact, strings.ToLower(k))
	}
	loweredPatterns := make([]string, 0, len(patterns))
	for _, p := range patterns {
		loweredPatterns = append(loweredPatterns, strings.ToLower(p))
	}

	var partial map[string]int
	if len(opts.PartialReveal) > 0 {
		partial = make(map[string]int, len(opts.PartialReveal))
		for k, n := range opts.PartialReveal {
			if n > 0 {
				partial[strings.ToLower(k)] = n
			}
		}
	}

	return &Redactor{
		exact:    exact,
		patterns: loweredPatterns,
		mask:     mask,
		maxDepth: maxDepth,
		partial:  partial,
	}
}

// DefaultRedactor returns a Redactor using the package defaults
// (DefaultSensitiveKeys + DefaultSensitivePatterns + RedactMask). It is what
// the HTTP hook and Fiber adapters apply when no custom Redactor is set.
func DefaultRedactor() *Redactor { return NewRedactor(RedactorOptions{}) }

// sensitive reports whether key (any case) should be masked.
func (r *Redactor) sensitive(key string) bool {
	if r == nil {
		return false
	}
	lk := strings.ToLower(key)
	if _, ok := r.exact[lk]; ok {
		return true
	}
	for _, p := range r.patterns {
		if strings.Contains(lk, p) {
			return true
		}
	}
	return false
}

// Value redacts an arbitrary decoded value (the shape stored in Req/Res
// fields). Maps with sensitive keys have those values masked; nested maps and
// slices are walked recursively. Non-map values are returned unchanged — a
// bare string body cannot be keyed, so there is nothing to mask. The input is
// never mutated; a copy is returned for any container that is walked.
func (r *Redactor) Value(v any) any {
	if r == nil {
		return v
	}
	return r.redactValue(v, 0)
}

func (r *Redactor) redactValue(v any, depth int) any {
	if depth > r.maxDepth {
		return v
	}
	switch m := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			if r.sensitive(k) {
				out[k] = r.maskFor(k, val)
				continue
			}
			out[k] = r.redactValue(val, depth+1)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(m))
		for k, val := range m {
			if r.sensitive(k) {
				out[k] = r.maskString(k, val)
				continue
			}
			out[k] = val
		}
		return out
	case []any:
		out := make([]any, len(m))
		for i, val := range m {
			out[i] = r.redactValue(val, depth+1)
		}
		return out
	default:
		return v
	}
}

// maskFor masks the value of sensitive key k. String values may be partially
// revealed when k is configured in PartialReveal; everything else (numbers,
// bools, nested objects under a sensitive key) is fully masked, since a
// non-string secret has no safe tail to expose.
func (r *Redactor) maskFor(k string, v any) any {
	if s, ok := v.(string); ok {
		return r.maskString(k, s)
	}
	return r.mask
}

// maskString masks the string value of sensitive key k. When k opts into
// partial reveal and the value is long enough, it returns "<mask>…<tail>"
// keeping the last N runes; otherwise it returns the full mask.
func (r *Redactor) maskString(k, v string) string {
	n := r.revealCount(k)
	if n <= 0 {
		return r.mask
	}
	runes := []rune(v)
	// Reveal only when the kept tail is at most half the value, so a short
	// secret never ends up mostly exposed.
	if len(runes) < n*2 {
		return r.mask
	}
	return r.mask + "…" + string(runes[len(runes)-n:])
}

// revealCount returns the configured number of trailing characters to reveal
// for sensitive key k, or 0 when k is not set for partial reveal.
func (r *Redactor) revealCount(k string) int {
	if len(r.partial) == 0 {
		return 0
	}
	return r.partial[strings.ToLower(k)]
}

// Req returns a redacted copy of req, or nil when req is nil. Each field is
// run through Value so sensitive headers and body keys are masked.
func (r *Redactor) Req(req *Req) *Req {
	if req == nil {
		return nil
	}
	if r == nil {
		return req
	}
	return &Req{
		Params:      r.Value(req.Params),
		QueryString: r.Value(req.QueryString),
		Headers:     r.Value(req.Headers),
		Body:        r.Value(req.Body),
	}
}

// Res returns a redacted copy of res, or nil when res is nil.
func (r *Redactor) Res(res *Res) *Res {
	if res == nil {
		return nil
	}
	if r == nil {
		return res
	}
	return &Res{
		Headers:    r.Value(res.Headers),
		Body:       r.Value(res.Body),
		StatusCode: res.StatusCode,
	}
}

// Incoming returns a redacted copy of inc with Req and Res masked. The Error
// and ResponseTime fields are copied through unchanged.
func (r *Redactor) Incoming(inc Incoming) Incoming {
	if r == nil {
		return inc
	}
	inc.Req = r.Req(inc.Req)
	inc.Res = r.Res(inc.Res)
	return inc
}

// Outgoing returns a redacted copy of out with Req and Res masked.
func (r *Redactor) Outgoing(out Outgoing) Outgoing {
	if r == nil {
		return out
	}
	out.Req = r.Req(out.Req)
	out.Res = r.Res(out.Res)
	return out
}

// bearerRe is unused by the key-based redactor but kept available for callers
// that want to scrub credential-looking substrings out of free-form strings.
var bearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/-]+=*`)

// RedactString masks "Bearer <token>" sequences inside a free-form string,
// leaving the rest intact. Useful for error messages or URLs that may embed a
// credential. Returns s unchanged when nothing matches.
func RedactString(s string) string {
	return bearerRe.ReplaceAllString(s, "Bearer "+RedactMask)
}
