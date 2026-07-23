package sentrycore_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/adrielcodeco/go-tools/sentrycore"
)

func TestRedactHeader(t *testing.T) {
	cases := map[string]struct {
		key, value string
		wantMasked bool
	}{
		"authorization is masked": {"Authorization", "Bearer secret", true},
		"case-insensitive":        {"AUTHORIZATION", "Bearer secret", true},
		"cookie is masked":        {"Cookie", "session=abc", true},
		"set-cookie is masked":    {"Set-Cookie", "session=abc", true},
		"api key is masked":       {"X-Api-Key", "k-123", true},
		"content-type passes":     {"Content-Type", "application/json", false},
		"custom header passes":    {"X-Origin", "test", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sentrycore.RedactHeader(tc.key, tc.value)
			masked := got != tc.value
			if masked != tc.wantMasked {
				t.Errorf("RedactHeader(%q, %q) = %q; wantMasked=%v", tc.key, tc.value, got, tc.wantMasked)
			}
		})
	}
}

func TestRedactQueryString(t *testing.T) {
	got := sentrycore.RedactQueryString("q=hello&access_token=SECRET&page=2")
	values, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("result not parseable: %v", err)
	}
	if v := values.Get("access_token"); v == "SECRET" || v == "" {
		t.Errorf("access_token not redacted: %q", v)
	}
	if values.Get("q") != "hello" {
		t.Errorf("non-sensitive param q was altered: %q", values.Get("q"))
	}
	if values.Get("page") != "2" {
		t.Errorf("non-sensitive param page was altered: %q", values.Get("page"))
	}
	if strings.Contains(got, "SECRET") {
		t.Errorf("secret leaked in output: %q", got)
	}

	if sentrycore.RedactQueryString("") != "" {
		t.Error("empty query should return empty")
	}
}
