package logcore

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// redactCore is a zapcore.Core decorator that masks the values of log fields
// whose key is sensitive, for every log call routed through the logger. It is
// the global complement to the schema-level redaction the HTTP hook and Fiber
// middlewares perform: any zap.String("authorization", …) or
// zap.Any("password", …) added anywhere in the codebase is caught here too,
// so a stray sensitive field never reaches the encoder in clear text.
//
// Fields are redacted both at the call site (the With/Write fields) and in the
// accumulated context carried by With(). String fields with a sensitive key
// are replaced wholesale with the mask. Structured fields (objects/arrays
// added via zap.Any) are walked via the Redactor so nested sensitive keys are
// masked while the surrounding shape is preserved.
type redactCore struct {
	zapcore.Core
	r *Redactor
}

// NewRedactCore wraps c so sensitive field values are masked before encoding.
// When r is nil the package DefaultRedactor is used. Plug it in via
// zap.WrapCore, or let Options.RedactFields do it for you:
//
//	logger, _ := logcore.New(logcore.Options{RedactFields: true})
func NewRedactCore(c zapcore.Core, r *Redactor) zapcore.Core {
	if r == nil {
		r = DefaultRedactor()
	}
	return &redactCore{Core: c, r: r}
}

func (c *redactCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactCore{Core: c.Core.With(c.redactFields(fields)), r: c.r}
}

func (c *redactCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	// Add this core (not the wrapped one) so Write is invoked and the
	// per-call fields are redacted on the way out.
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *redactCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	return c.Core.Write(ent, c.redactFields(fields))
}

// redactFields returns a copy of fields with sensitive entries masked. It does
// not mutate the input slice. Non-sensitive fields are passed through by value
// (zapcore.Field is a small struct), so the common case allocates only the
// outer slice.
func (c *redactCore) redactFields(fields []zapcore.Field) []zapcore.Field {
	if len(fields) == 0 {
		return fields
	}
	out := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		out[i] = c.redactField(f)
	}
	return out
}

func (c *redactCore) redactField(f zapcore.Field) zapcore.Field {
	if c.r.sensitive(f.Key) {
		// String fields may opt into partial reveal; any other encoded type
		// (int, bool, object, …) has no safe tail and is fully masked.
		if f.Type == zapcore.StringType {
			return zap.String(f.Key, c.r.maskString(f.Key, f.String))
		}
		return zap.String(f.Key, c.r.mask)
	}
	// Non-sensitive key: the value may still be a structured payload (the
	// "incoming"/"outgoing" objects, or any map/slice added via zap.Any)
	// that contains sensitive keys nested inside. Walk reflected values.
	if f.Type == zapcore.ReflectType && f.Interface != nil {
		switch f.Interface.(type) {
		case map[string]any, map[string]string, []any:
			return zap.Any(f.Key, c.r.Value(f.Interface))
		}
	}
	return f
}
