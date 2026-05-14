package logcore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.elastic.co/apm/v2/apmtest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/adrielcodeco/go-tools/httpclient"
	"github.com/adrielcodeco/go-tools/logcore"
)

func TestNewWithDefaults(t *testing.T) {
	l, err := logcore.New(logcore.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if l == nil || l.Logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewWithFields(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	l, err := logcore.New(logcore.Options{
		Service:        "ledger",
		Version:        "1.2.3",
		Environment:    "staging",
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("hello")
	if observed.Len() != 1 {
		t.Fatalf("expected one observed entry, got %d", observed.Len())
	}
	entry := observed.AllUntimed()[0]
	got := map[string]string{}
	for _, f := range entry.Context {
		got[f.Key] = f.String
	}
	if got["service.name"] != "ledger" || got["service.version"] != "1.2.3" || got["service.environment"] != "staging" {
		t.Errorf("missing service fields: %+v", got)
	}
}

func TestLogCtxAttachesTraceFields(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	l, _ := logcore.New(logcore.Options{
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})

	apmtest.WithTransaction(func(ctx context.Context) {
		l.LogCtx(ctx).Info("with trace")
	})
	l.LogCtx(nil).Info("nil ctx")
	l.LogCtx(context.Background()).Info("empty ctx")

	entries := observed.AllUntimed()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	hasTraceID := func(fields []zapcore.Field) bool {
		for _, f := range fields {
			if f.Key == "trace.id" {
				return true
			}
		}
		return false
	}
	if !hasTraceID(entries[0].Context) {
		t.Errorf("expected trace.id on first entry, got %+v", entries[0].Context)
	}
	if hasTraceID(entries[1].Context) || hasTraceID(entries[2].Context) {
		t.Error("nil/empty ctx should not have trace.id")
	}
}

func TestGlobalLoggerLazyInit(t *testing.T) {
	logcore.SetGlobal(nil) // restore default
	if l := logcore.Log(); l == nil {
		t.Fatal("expected non-nil global Log")
	}
	if s := logcore.Logy(); s == nil {
		t.Fatal("expected non-nil global Logy")
	}
}

func TestHTTPClientHookEmitsOutgoing(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	l, _ := logcore.New(logcore.Options{
		DisableAPMCore: true,
		Extra: []zap.Option{
			zap.WrapCore(func(zapcore.Core) zapcore.Core { return core }),
		},
	})

	hook := logcore.HookFor(l)
	hook(httpclient.Record{
		Ctx:          context.Background(),
		Method:       "POST",
		URL:          "https://api.example.com/charge",
		Status:       200,
		ResponseTime: 42 * time.Millisecond,
		ReqBody:      []byte(`{"amount":100}`),
		ResBody:      []byte(`{"id":"c-1"}`),
		Attempt:      1,
	})
	hook(httpclient.Record{
		Ctx:          context.Background(),
		Method:       "GET",
		URL:          "https://api.example.com/error",
		Status:       0,
		ResponseTime: 100 * time.Millisecond,
		Err:          errors.New("dial: connection refused"),
		Attempt:      2,
	})

	entries := observed.AllUntimed()
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Message, "outgoing") {
		t.Errorf("unexpected message: %q", entries[0].Message)
	}
	// Status "Ø" for the failed call
	out2 := findOutgoing(entries[1].Context)
	if out2 == nil || out2.Res.StatusCode != "Ø" {
		t.Errorf("expected Ø status, got %+v", out2)
	}
	if out2.Error == nil || *out2.Error == "" {
		t.Error("expected error string on second entry")
	}
}

func findOutgoing(fields []zapcore.Field) *logcore.Outgoing {
	for _, f := range fields {
		if f.Key == "outgoing" {
			if v, ok := f.Interface.(logcore.Outgoing); ok {
				return &v
			}
		}
	}
	return nil
}

func TestFlattenHeadersAndDecodeJSONBody(t *testing.T) {
	if logcore.FlattenHeaders(nil) != nil {
		t.Error("nil headers should flatten to nil")
	}
	if logcore.FlattenHeaders(map[string][]string{}) != nil {
		t.Error("empty headers should flatten to nil")
	}
	got := logcore.FlattenHeaders(map[string][]string{
		"X-A": {"1"},
		"X-B": {"1", "2"},
		"X-C": nil,
	})
	m := got.(map[string]string)
	if m["X-A"] != "1" || m["X-B"] != "1,2" {
		t.Errorf("unexpected flatten: %+v", m)
	}

	if v := logcore.DecodeJSONBody(nil); v != nil {
		t.Error("nil bytes should decode to nil")
	}
	if v := logcore.DecodeJSONBody([]byte(`{"a":1}`)); v == nil {
		t.Error("expected map decode")
	}
	if v := logcore.DecodeJSONBody([]byte(`[1,2]`)); v == nil {
		t.Error("expected array decode")
	}
	if v := logcore.DecodeJSONBody([]byte(`not json`)); v != "not json" {
		t.Errorf("expected raw string fallback, got %v", v)
	}
}
