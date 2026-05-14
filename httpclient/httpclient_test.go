package httpclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adrielcodeco/go-tools/httpclient"
)

type charge struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

func TestPOSTAndGETHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /charge":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"id":"c-1","amount":100}`)
		case "GET /charge":
			if r.URL.Query().Get("filter") != "active" {
				t.Errorf("missing query: %v", r.URL.RawQuery)
			}
			fmt.Fprintln(w, `{"id":"c-1","amount":100}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL + "/charge",
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{"amount": 100},
	})
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if out.ID != "c-1" || out.Amount != 100 {
		t.Fatalf("decoded wrong: %+v", out)
	}

	out2, err := httpclient.GET[charge](context.Background(), httpclient.RequestOptions{
		URL:         srv.URL + "/charge",
		QueryString: map[string]string{"filter": "active"},
	})
	if err != nil || out2.ID != "c-1" {
		t.Fatalf("GET: out=%v err=%v", out2, err)
	}
}

func TestStatusErrorContainsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"bad input"}`)
	}))
	defer srv.Close()

	body, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL: srv.URL,
	})
	var se *httpclient.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StatusError, got %T (%v)", err, err)
	}
	if se.StatusCode != 400 {
		t.Errorf("status = %d", se.StatusCode)
	}
	if len(se.Body) == 0 || len(body) == 0 {
		t.Errorf("expected body bytes to be preserved")
	}
}

func TestRetryOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, `{"id":"c-2","amount":7}`)
	}))
	defer srv.Close()

	out, err := httpclient.POST[charge](context.Background(), httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.JSONHeaders(),
		Data:    map[string]any{},
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     20 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
	if out.ID != "c-2" {
		t.Errorf("decoded wrong: %+v", out)
	}
}

func TestHookCapturesAttempts(t *testing.T) {
	var records []httpclient.Record
	httpclient.SetHook(func(r httpclient.Record) { records = append(records, r) })
	defer httpclient.SetHook(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, _ = httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL: srv.URL,
		Retry: httpclient.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
		},
	})
	if len(records) != 2 {
		t.Fatalf("expected 2 hook records, got %d", len(records))
	}
	if records[0].Attempt != 1 || records[1].Attempt != 2 {
		t.Errorf("unexpected attempt numbers: %+v", records)
	}
}

func TestFormEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("name") != "alice" {
			t.Errorf("missing form field: %v", r.PostForm)
		}
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	_, err := httpclient.Do(context.Background(), "POST", httpclient.RequestOptions{
		URL:     srv.URL,
		Headers: httpclient.FormHeaders(),
		Data:    map[string]string{"name": "alice"},
	})
	if err != nil {
		t.Fatalf("POST form: %v", err)
	}
}
