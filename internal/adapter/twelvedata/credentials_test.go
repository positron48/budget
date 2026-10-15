package twelvedata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeSecretFileGateAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential")
	if e := os.WriteFile(path, []byte("synthetic-secret\n"), 0600); e != nil {
		t.Fatal(e)
	}
	c, e := NewFromSecretFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(fmt.Sprintf("%v %#v", c, c), "synthetic-secret") {
		t.Fatal("credential formatting leak")
	}
	called := false
	c.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not reach transport")
	})
	c.SetRequestGate(func(_ context.Context, key string) error {
		sum := sha256.Sum256([]byte("twelvedata:synthetic-secret"))
		if key != hex.EncodeToString(sum[:]) {
			t.Fatal("unexpected gate identity")
		}
		return context.Canceled
	})
	_, e = c.EquityHistory(context.Background(), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10")
	if !errors.Is(e, context.Canceled) || called {
		t.Fatal("cancelled gate reached provider", e, called)
	}
	if e = os.WriteFile(path, []byte(strings.Repeat("x", 8193)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = NewFromSecretFile(path); e == nil {
		t.Fatal("oversized credential accepted")
	}
}

func TestNativeProviderBackoffAndDailyLimitBody(t *testing.T) {
	for _, status := range []int{429, 200} {
		c, e := newClient("secret-marker")
		if e != nil {
			t.Fatal(e)
		}
		var delay time.Duration
		c.SetRequestBackoff(func(_ context.Context, key string, d time.Duration) error {
			if strings.Contains(key, "secret-marker") {
				t.Fatal("credential leaked")
			}
			delay = d
			return nil
		})
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"120"}}, Body: io.NopCloser(strings.NewReader(`{"status":"error","code":429,"message":"daily quota reached; secret-marker"}`)), Request: r}, nil
		})
		_, e = c.EquityHistory(context.Background(), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10")
		var rejected responseError
		if !errors.As(e, &rejected) || !rejected.Temporary() || delay <= 0 || strings.Contains(e.Error(), "secret-marker") {
			t.Fatal("backoff or redaction", e, delay)
		}
		if status == 429 && delay != 2*time.Minute {
			t.Fatal(delay)
		}
		if status == 200 && delay > 24*time.Hour {
			t.Fatal(delay)
		}
	}
}
