package logcore_test

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/adrielcodeco/go-tools/logcore"
)

func TestRedactor_Value_Headers(t *testing.T) {
	r := logcore.DefaultRedactor()
	in := map[string]string{
		"Authorization": "Bearer tok",
		"Cookie":        "session=abc",
		"Content-Type":  "application/json",
	}
	out, ok := r.Value(in).(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", r.Value(in))
	}
	if out["Authorization"] != logcore.RedactMask {
		t.Errorf("Authorization = %q, want mask", out["Authorization"])
	}
	if out["Cookie"] != logcore.RedactMask {
		t.Errorf("Cookie = %q, want mask", out["Cookie"])
	}
	if out["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q, want passthrough", out["Content-Type"])
	}
	// Input must not be mutated.
	if in["Authorization"] != "Bearer tok" {
		t.Errorf("input mutated: Authorization = %q", in["Authorization"])
	}
}

func TestRedactor_Value_NestedBody(t *testing.T) {
	r := logcore.DefaultRedactor()
	in := map[string]any{
		"amount": 100,
		"card": map[string]any{
			"number": "4111111111111111",
			"cvv":    "123",
		},
		"user_password": "hunter2", // substring match
		"items": []any{
			map[string]any{"token": "xyz", "qty": 2},
		},
	}
	out := r.Value(in).(map[string]any)

	if out["amount"] != 100 {
		t.Errorf("amount changed: %v", out["amount"])
	}
	// "card" itself is a sensitive exact key → whole subtree masked.
	if out["card"] != logcore.RedactMask {
		t.Errorf("card = %v, want mask", out["card"])
	}
	if out["user_password"] != logcore.RedactMask {
		t.Errorf("user_password = %v, want mask (substring)", out["user_password"])
	}
	items := out["items"].([]any)
	item0 := items[0].(map[string]any)
	if item0["token"] != logcore.RedactMask {
		t.Errorf("items[0].token = %v, want mask", item0["token"])
	}
	if item0["qty"] != 2 {
		t.Errorf("items[0].qty changed: %v", item0["qty"])
	}
}

func TestRedactor_Disabled_NilPassthrough(t *testing.T) {
	var r *logcore.Redactor // nil redactor is a no-op
	in := map[string]string{"Authorization": "Bearer tok"}
	if got := r.Value(in).(map[string]string)["Authorization"]; got != "Bearer tok" {
		t.Errorf("nil redactor masked value: %q", got)
	}
}

func TestRedactString(t *testing.T) {
	got := logcore.RedactString("call failed: GET https://x with Bearer abc.def-123")
	want := "call failed: GET https://x with Bearer " + logcore.RedactMask
	if got != want {
		t.Errorf("RedactString = %q, want %q", got, want)
	}
}

func TestRedactCore_MasksFlatSensitiveField(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	redacted := logcore.NewRedactCore(core, nil)
	logger := zap.New(redacted)

	logger.Info("login",
		zap.String("authorization", "Bearer tok"),
		zap.String("user", "alice"),
	)

	entry := observed.AllUntimed()[0]
	fields := entry.ContextMap()
	if fields["authorization"] != logcore.RedactMask {
		t.Errorf("authorization = %v, want mask", fields["authorization"])
	}
	if fields["user"] != "alice" {
		t.Errorf("user = %v, want passthrough", fields["user"])
	}
}

func TestRedactCore_MasksNestedReflectedField(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	logger := zap.New(logcore.NewRedactCore(core, nil))

	logger.Info("req", zap.Any("payload", map[string]any{
		"password": "hunter2",
		"keep":     "ok",
	}))

	payload := observed.AllUntimed()[0].ContextMap()["payload"].(map[string]any)
	if payload["password"] != logcore.RedactMask {
		t.Errorf("payload.password = %v, want mask", payload["password"])
	}
	if payload["keep"] != "ok" {
		t.Errorf("payload.keep = %v, want passthrough", payload["keep"])
	}
}

func TestRedactCore_MasksFieldsFromWith(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	logger := zap.New(logcore.NewRedactCore(core, nil)).
		With(zap.String("x-api-key", "secret123"))

	logger.Info("hi")

	if got := observed.AllUntimed()[0].ContextMap()["x-api-key"]; got != logcore.RedactMask {
		t.Errorf("x-api-key = %v, want mask", got)
	}
}

func TestNew_RedactFields_EndToEnd(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	l, err := logcore.New(logcore.Options{
		DisableAPMCore: true,
		RedactFields:   true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("event", zap.String("password", "p"))

	if got := observed.AllUntimed()[0].ContextMap()["password"]; got != logcore.RedactMask {
		t.Errorf("password = %v, want mask", got)
	}
}

func TestRedactor_PartialReveal(t *testing.T) {
	r := logcore.NewRedactor(logcore.RedactorOptions{
		PartialReveal: map[string]int{"card": 4, "cvv": 4},
	})
	out := r.Value(map[string]any{
		"card":     "4111111111111111", // long → reveal last 4
		"cvv":      "123",               // too short for 4 → full mask
		"password": "hunter2longvalue",  // not in PartialReveal → full mask
		"amount":   100,
	}).(map[string]any)

	if got := out["card"]; got != logcore.RedactMask+"…1111" {
		t.Errorf("card = %v, want partial reveal", got)
	}
	if out["cvv"] != logcore.RedactMask {
		t.Errorf("cvv = %v, want full mask (too short)", out["cvv"])
	}
	if out["password"] != logcore.RedactMask {
		t.Errorf("password = %v, want full mask (not configured)", out["password"])
	}
	if out["amount"] != 100 {
		t.Errorf("amount changed: %v", out["amount"])
	}
}

func TestRedactor_PartialReveal_NonString_FullMask(t *testing.T) {
	r := logcore.NewRedactor(logcore.RedactorOptions{
		PartialReveal: map[string]int{"token": 4},
	})
	// token is sensitive + configured, but the value is not a string.
	out := r.Value(map[string]any{"token": 123456789}).(map[string]any)
	if out["token"] != logcore.RedactMask {
		t.Errorf("token = %v, want full mask for non-string", out["token"])
	}
}

func TestRedactCore_PartialReveal_FlatField(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	r := logcore.NewRedactor(logcore.RedactorOptions{
		PartialReveal: map[string]int{"authorization": 4},
	})
	logger := zap.New(logcore.NewRedactCore(core, r))

	logger.Info("call", zap.String("authorization", "Bearer abcdef1234"))

	got := observed.AllUntimed()[0].ContextMap()["authorization"]
	if got != logcore.RedactMask+"…1234" {
		t.Errorf("authorization = %v, want partial reveal", got)
	}
}

func TestRedactor_RemoveKeys(t *testing.T) {
	// Cookie is a default sensitive key; remove it while keeping the rest.
	r := logcore.NewRedactor(logcore.RedactorOptions{
		RemoveKeys: []string{"Cookie"},
	})
	out := r.Value(map[string]string{
		"Cookie":        "session=abc",
		"Authorization": "Bearer tok",
	}).(map[string]string)

	if out["Cookie"] != "session=abc" {
		t.Errorf("Cookie = %q, want passthrough after RemoveKeys", out["Cookie"])
	}
	if out["Authorization"] != logcore.RedactMask {
		t.Errorf("Authorization = %q, want still masked", out["Authorization"])
	}
}

func TestRedactor_RemoveKeys_StillCaughtByPattern(t *testing.T) {
	// "client_secret" is removed from exact keys but still matches the
	// "secret" substring pattern, so it stays masked.
	r := logcore.NewRedactor(logcore.RedactorOptions{
		RemoveKeys: []string{"client_secret"},
	})
	out := r.Value(map[string]any{"client_secret": "x"}).(map[string]any)
	if out["client_secret"] != logcore.RedactMask {
		t.Errorf("client_secret = %v, want still masked via pattern", out["client_secret"])
	}
}

func TestRedactor_CustomExtraKeys(t *testing.T) {
	r := logcore.NewRedactor(logcore.RedactorOptions{
		Extra: []string{"account_balance"},
	})
	out := r.Value(map[string]any{"account_balance": 999, "name": "bob"}).(map[string]any)
	if out["account_balance"] != logcore.RedactMask {
		t.Errorf("account_balance = %v, want mask", out["account_balance"])
	}
	if out["name"] != "bob" {
		t.Errorf("name = %v, want passthrough", out["name"])
	}
}
