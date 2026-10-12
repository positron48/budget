package tinvest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryMetadata(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		input string
		want  time.Duration
	}{
		{"3", 3 * time.Second}, {"-1", 0}, {"9223372036854775807", 24 * time.Hour},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-time.Second).Format(http.TimeFormat), 0}, {"credential-must-not-leak", 0},
	} {
		if got := retryAfter(tc.input, now); got != tc.want {
			t.Fatalf("%q: %v", tc.input, got)
		}
	}
	for _, code := range []int{400, 401, 408, 429, 500, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(code)
			if _, err := fmt.Fprint(w, "private-response"); err != nil {
				t.Error(err)
			}
		}))
		client, _ := newClient("fake-test-credential")
		client.baseURL = server.URL + "/"
		_, err := client.FindInstrument(context.Background(), "SBER")
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != code || httpErr.RetryAfter != 7*time.Second || httpErr.Temporary() != (code == 408 || code == 429 || code >= 500) || err.Error() != fmt.Sprintf("T-Invest HTTP %d", code) {
			t.Errorf("unexpected safe error: %v", err)
		}
		server.Close()
	}
}

func TestRateLimitBackoffIsShared(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "90"); w.WriteHeader(429) }))
	defer server.Close()
	client, _ := newClient("fake-test-credential")
	client.baseURL = server.URL + "/"
	var delay time.Duration
	var gateKey, backoffKey string
	client.SetRequestGate(func(_ context.Context, key string) error { gateKey = key; return nil })
	client.SetRequestBackoff(func(_ context.Context, key string, d time.Duration) error { backoffKey = key; delay = d; return nil })
	if _, err := client.FindInstrument(context.Background(), "SBER"); err == nil {
		t.Fatal("429 accepted")
	}
	if gateKey == "" || gateKey != backoffKey || delay != 90*time.Second {
		t.Fatal("429 backoff not propagated")
	}
}
