package twelvedata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var checkedAt = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func payload() map[string]any {
	return map[string]any{"status": "ok", "meta": map[string]any{"symbol": "USD/RUB", "interval": "1day", "currency_base": "USD", "currency_quote": "RUB", "exchange_timezone": "UTC", "type": "Physical Currency"}, "values": []Bar{{Date: "2024-06-12", Open: "89.12345678901", High: "90.00000000000", Low: "88.00000000000", Close: "89.98765432109"}, {Date: "2024-06-24", Open: "90", High: "92", Low: "90", Close: "91"}}}
}
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestAuditPreservesPrecisionAndReportsCalendarCoverage(t *testing.T) {
	a, e := Parse(encode(t, payload()), "USD/RUB", "2024-06-10", "2024-06-28", checkedAt)
	if e != nil {
		t.Fatal(e)
	}
	if a.Bars[0].Close != "89.98765432109" || len(a.BodySHA256) != 64 {
		t.Fatal(a)
	}
	c, e := a.Validate()
	if e != nil {
		t.Fatal(e)
	}
	if c.Count != 2 || c.LeadingDays != 2 || c.TrailingDays != 4 || len(c.Gaps) != 1 || c.Gaps[0].Days != 12 || c.FinanciallyApproved {
		t.Fatal(c)
	}
}
func TestRejectsAmbiguousOrCorruptSeries(t *testing.T) {
	cases := map[string]func(map[string]any){
		"provider_error":   func(p map[string]any) { p["status"] = "error"; p["message"] = "secret" },
		"symbol":           func(p map[string]any) { p["meta"].(map[string]any)["symbol"] = "EUR/RUB" },
		"missing_timezone": func(p map[string]any) { delete(p["meta"].(map[string]any), "exchange_timezone") },
		"timezone":         func(p map[string]any) { p["meta"].(map[string]any)["exchange_timezone"] = "Unknown/Zone" },
		"asset_type":       func(p map[string]any) { p["meta"].(map[string]any)["type"] = "ETF" },
		"interval":         func(p map[string]any) { p["meta"].(map[string]any)["interval"] = "1min" },
		"duplicate":        func(p map[string]any) { b := p["values"].([]Bar); b[1].Date = b[0].Date },
		"date_time":        func(p map[string]any) { p["values"].([]Bar)[0].Date = "2024-06-12 00:00:00" },
		"outside":          func(p map[string]any) { p["values"].([]Bar)[0].Date = "2024-06-09" },
		"scientific":       func(p map[string]any) { p["values"].([]Bar)[0].Close = "9e1" },
		"rounded":          func(p map[string]any) { p["values"].([]Bar)[0].Close = "89.123456789012" },
		"zero":             func(p map[string]any) { p["values"].([]Bar)[0].Low = "0" },
		"ohlc":             func(p map[string]any) { p["values"].([]Bar)[0].High = "89" },
		"empty":            func(p map[string]any) { p["values"] = []Bar{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := payload()
			mutate(p)
			_, e := Parse(encode(t, p), "USD/RUB", "2024-06-10", "2024-06-28", checkedAt)
			if e == nil || strings.Contains(e.Error(), "secret") {
				t.Fatalf("error=%v", e)
			}
		})
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAuthenticatedReadAndSafeErrors(t *testing.T) {
	for _, kind := range []string{"ok", "http", "status", "transport", "oversized", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			c, e := newClient("secret-marker")
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/time_series" || strings.Contains(r.URL.String(), "secret-marker") || r.Header.Get("Authorization") != "apikey secret-marker" {
					t.Fatal("unsafe request")
				}
				q := r.URL.Query()
				if q.Get("symbol") != "USD/RUB" || q.Get("outputsize") != "5000" || q.Get("dp") != "11" || q.Get("adjust") != "none" || q.Get("timezone") != "UTC" {
					t.Fatal(q)
				}
				code := 200
				body := string(encode(t, payload()))
				headers := http.Header{}
				switch kind {
				case "http":
					code = 429
					body = "secret-marker"
				case "status":
					body = `{"status":"error","message":"secret-marker"}`
				case "transport":
					return nil, errors.New("secret-marker")
				case "oversized":
					body = strings.Repeat("x", maxBody+1)
				case "redirect":
					code = 302
					headers.Set("Location", "https://attacker.invalid/secret-marker")
				}
				return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			_, e = c.History(context.Background(), "USD/RUB", "2024-06-10", "2024-06-28")
			if kind == "ok" && e != nil {
				t.Fatal(e)
			}
			if kind != "ok" && (e == nil || strings.Contains(e.Error(), "secret-marker")) {
				t.Fatalf("unsafe error: %v", e)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
			if strings.Contains(fmt.Sprintf("%v %#v", c, c), "secret-marker") {
				t.Fatal("client format leaked")
			}
		})
	}
}
func TestInvalidWindowDoesNotRequest(t *testing.T) {
	c, _ := newClient("fake")
	c.http.Transport = roundTrip(func(_ *http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })
	for _, args := range [][3]string{{"GBP/RUB", "2024-01-01", "2024-01-02"}, {"USD/RUB", "2024-02-30", "2024-03-01"}, {"USD/RUB", "2024-02-02", "2024-01-01"}, {"USD/RUB", "2024-01-01", "2099-01-01"}, {"USD/RUB", "2000-01-01", "2024-01-01"}} {
		if _, e := c.History(context.Background(), args[0], args[1], args[2]); e == nil {
			t.Fatal(args)
		}
	}
}

func TestNativeSydneyDailyDateIsNotConvertedToUTC(t *testing.T) {
	p := payload()
	m := p["meta"].(map[string]any)
	m["exchange_timezone"] = "Australia/Sydney"
	m["currency_base"] = "US Dollar"
	m["currency_quote"] = "Russian Ruble"
	a, e := Parse(encode(t, p), "USD/RUB", "2024-06-10", "2024-06-28", checkedAt)
	if e != nil {
		t.Fatal(e)
	}
	c, e := a.Validate()
	if e != nil {
		t.Fatal(e)
	}
	if c.SourceTimezone != "Australia/Sydney" || c.PublicationAndValidityVerified || a.Bars[0].Date != "2024-06-12" {
		t.Fatal(c)
	}
}

func TestLiveResponseShapeDoesNotInferTimezoneOrDeduplicate(t *testing.T) {
	p := payload()
	m := p["meta"].(map[string]any)
	m["currency_base"] = "US Dollar"
	m["currency_quote"] = "Russian Ruble"
	delete(m, "exchange_timezone")
	bars := p["values"].([]Bar)
	bars[1].Date = bars[0].Date
	_, err := Parse(encode(t, p), "USD/RUB", "2024-06-10", "2024-06-28", checkedAt)
	if err == nil || !strings.Contains(err.Error(), "omits daily bar timezone") {
		t.Fatalf("missing timezone diagnostic: %v", err)
	}
	// Even explicit timezone metadata would not make a duplicate-date row safe.
	m["exchange_timezone"] = "Australia/Sydney"
	_, err = Parse(encode(t, p), "USD/RUB", "2024-06-10", "2024-06-28", checkedAt)
	if err == nil || !strings.Contains(err.Error(), "duplicate or out-of-range") {
		t.Fatalf("duplicate diagnostic: %v", err)
	}
}
