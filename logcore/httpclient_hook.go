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
// fields (via LogCtx). For a custom logger, see HookFor.
func HTTPClientHook() httpclient.Hook { return HookFor(nil) }

// HookFor returns the same hook bound to l. Nil falls back to the
// global logger.
func HookFor(l *Logger) httpclient.Hook {
	return func(r httpclient.Record) {
		var logger *zap.Logger
		if l != nil {
			logger = l.LogCtx(r.Ctx)
		} else {
			logger = LogCtx(r.Ctx)
		}

		status := "Ø"
		if r.Status > 0 {
			status = strconv.Itoa(r.Status)
		}

		var errStr *string
		if r.Err != nil {
			s := r.Err.Error()
			errStr = &s
		}

		out := Outgoing{
			Req: &Req{
				Body: DecodeJSONBody(r.ReqBody),
			},
			Res: &Res{
				Body:       DecodeJSONBody(r.ResBody),
				StatusCode: status,
			},
			Error:        errStr,
			ResponseTime: r.ResponseTime.String(),
		}

		msg := fmt.Sprintf("← outgoing ← [%s] %s - %s", r.Method, r.URL, status)
		logger.Info(msg, zap.Any("outgoing", out), zap.Int("attempt", r.Attempt))
	}
}
