package logcore

import (
	"fmt"
	"strconv"

	"go.uber.org/zap"

	"github.com/adrielcodeco/go-tools/httpclient"
)

// HTTPClientHook returns a httpclient.Hook that emits one structured
// log per attempt with the same shape the Fiber middlewares produce
// for incoming requests — Kibana queries like `outgoing.req.body.id`
// match regardless of direction.
//
// Use it during bootstrap:
//
//	httpclient.SetHook(logcore.HTTPClientHook())
//
// The log line uses the global logger decorated with apmcore trace
// fields (via LogCtx). Sensitive headers and body fields are masked
// with DefaultRedactor before logging. For a custom logger, see HookFor;
// for a custom redaction policy, see HookForRedacting.
func HTTPClientHook() httpclient.Hook { return HookForRedacting(nil, DefaultRedactor()) }

// HookFor returns the same hook bound to l, applying DefaultRedactor. Nil
// falls back to the global logger.
func HookFor(l *Logger) httpclient.Hook { return HookForRedacting(l, DefaultRedactor()) }

// HookForRedacting returns the hook bound to l with redaction policy r. A nil
// l falls back to the global logger; a nil r disables redaction (the request
// and response are logged verbatim — use only when the data is known safe).
func HookForRedacting(l *Logger, r *Redactor) httpclient.Hook {
	return func(rec httpclient.Record) {
		var logger *zap.Logger
		if l != nil {
			logger = l.LogCtx(rec.Ctx)
		} else {
			logger = LogCtx(rec.Ctx)
		}

		status := "Ø"
		if rec.Status > 0 {
			status = strconv.Itoa(rec.Status)
		}

		var errStr *string
		if rec.Err != nil {
			s := rec.Err.Error()
			if r != nil {
				s = RedactString(s)
			}
			errStr = &s
		}

		var reqHeaders any
		if len(rec.ReqHeaders) > 0 {
			reqHeaders = rec.ReqHeaders
		}

		out := Outgoing{
			Req: &Req{
				Headers: reqHeaders,
				Body:    DecodeJSONBody(rec.ReqBody),
			},
			Res: &Res{
				Body:       DecodeJSONBody(rec.ResBody),
				StatusCode: status,
			},
			Error:        errStr,
			ResponseTime: rec.ResponseTime.String(),
		}
		if r != nil {
			out = r.Outgoing(out)
		}

		msg := fmt.Sprintf("← outgoing ← [%s] %s - %s", rec.Method, rec.URL, status)
		logger.Info(msg, zap.Any("outgoing", out), zap.Int("attempt", rec.Attempt))
	}
}

// InstallHTTPClientHook adds the global-logger hook to the httpclient hook
// chain. Uses AddHook (not SetHook) so it composes with any previously
// installed hook. Call once at boot.
func InstallHTTPClientHook() {
	httpclient.AddHook(HTTPClientHook())
}

// InstallHTTPClientHookFor is InstallHTTPClientHook bound to l.
func InstallHTTPClientHookFor(l *Logger) {
	httpclient.AddHook(HookFor(l))
}
