package twelvedata

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func equityPayload() map[string]any {
	return map[string]any{"status": "ok", "meta": map[string]any{"symbol": "NVDA", "exchange": "NASDAQ", "mic_code": "XNGS", "currency": "USD", "interval": "1day", "type": "Common Stock", "exchange_timezone": "America/New_York"}, "values": []EquityBar{
		{Bar: Bar{Date: "2024-06-10", Open: "120.37", High: "123.10", Low: "117.01", Close: "121.79"}, Volume: "123"},
		{Bar: Bar{Date: "2024-06-07", Open: "1197.69997", High: "1216.91645", Low: "1180.22000", Close: "1208.88000"}, Volume: "41238600"},
	}}
}

func TestEquityRawSplitBoundaryAndPaddedEnd(t *testing.T) {
	c, err := newClient("secret-marker")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("adjust") != "none" || q.Get("end_date") != "2024-06-11" || q.Get("start_date") != "2024-06-07" || q.Get("exchange") != "NASDAQ" || q.Get("dp") != "9" || q.Get("timezone") != "" || r.Header.Get("Authorization") != "apikey secret-marker" || strings.Contains(r.URL.String(), "secret-marker") {
			t.Fatal("unexpected request contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(encode(t, equityPayload())))), Request: r}, nil
	})
	a, observations, err := c.LoadEquityHistory(context.Background(), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10")
	if err != nil {
		t.Fatal(err)
	}
	if a.Adjustment != "none" || a.Meta.Timezone != "America/New_York" || len(a.Bars) != 2 || a.Bars[0].Close != "1208.88000" || a.Bars[1].Close != "121.79" || len(a.BodySHA256) != 64 {
		t.Fatal(a)
	}
	if len(observations) != 2 || observations[0].Price != "1208.880000000" || observations[0].InstrumentID != (EquityListing{"NVDA", "NASDAQ", "XNGS"}).ID() || observations[0].Adjustment != "unadjusted" || observations[0].Currency != "USD" || observations[0].Volume != "41238600" {
		t.Fatal(observations)
	}
	// An inclusive provider response may include the padded session. It must
	// never enter the requested series or cause an endpoint day to be omitted.
	a, err = parseEquity(encode(t, equityPayload()), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-09", checkedAt)
	if err != nil || len(a.Bars) != 1 || a.Bars[0].Date != "2024-06-07" {
		t.Fatalf("audit=%+v err=%v", a, err)
	}
}

func TestEquityRejectsWrongListingAndCorruptBars(t *testing.T) {
	for _, field := range []string{"symbol", "exchange", "mic_code", "currency", "interval", "type", "exchange_timezone", "duplicate", "outside", "negative", "status"} {
		t.Run(field, func(t *testing.T) {
			p := equityPayload()
			bars := p["values"].([]EquityBar)
			switch field {
			case "duplicate":
				bars[1].Date = bars[0].Date
			case "outside":
				bars[1].Date = "2024-06-06"
			case "negative":
				bars[1].Close = "-1"
			case "ohlc":
				bars[1].Low = "1210"
			case "status":
				p["status"] = "error"
				p["message"] = "secret-marker"
			default:
				p["meta"].(map[string]any)[field] = "secret-marker"
			}
			_, err := parseEquity(encode(t, p), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10", checkedAt)
			if err == nil || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestEquityRetainsBadSourceBarWithoutDiscardingOtherDates(t *testing.T) {
	p := equityPayload()
	p["values"].([]EquityBar)[0].Low = "121"
	a, err := parseEquity(encode(t, p), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10", checkedAt)
	if err != nil || len(a.Bars) != 2 || a.Bars[0].QualityIssue != "" || a.Bars[1].QualityIssue != "provider_ohlc_conflict" {
		t.Fatal(a, err)
	}
	o, err := equityObservation((EquityListing{"NVDA", "NASDAQ", "XNGS"}).ID(), a.Bars[1])
	if err != nil || o.Low != "121.000000000" || o.Open != "120.370000000" || o.QualityIssue != "provider_ohlc_conflict" {
		t.Fatal("source evidence changed", o, err)
	}
}

func TestEquityAcceptsREITCommonShareUnits(t *testing.T) {
	p := equityPayload()
	p["meta"].(map[string]any)["type"] = "REIT"
	a, err := parseEquity(encode(t, p), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10", checkedAt)
	if err != nil || a.Meta.Type != "REIT" || len(a.Bars) != 2 {
		t.Fatal(a, err)
	}
}

func TestEquitySafeTransportErrors(t *testing.T) {
	for _, mode := range []string{"transport", "http", "body"} {
		t.Run(mode, func(t *testing.T) {
			c, err := newClient("secret-marker")
			if err != nil {
				t.Fatal(err)
			}
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if mode == "transport" {
					return nil, errors.New("secret-marker")
				}
				status := 200
				if mode == "http" {
					status = 429
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret-marker")), Request: r}, nil
			})
			_, err = c.EquityHistory(context.Background(), EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2024-06-07", "2024-06-10")
			if err == nil || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestEquityRejectsOpenWindowBeforeProviderCall(t *testing.T) {
	if _, _, err := equityWindow(EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2026-10-12", "2026-10-12", checkedAt); err == nil {
		t.Fatal("open exchange date accepted")
	}
	if _, _, err := equityWindow(EquityListing{"NVDA", "NYSE", "XNGS"}, "2024-06-07", "2024-06-10", checkedAt); err == nil {
		t.Fatal("mixed listing identity accepted")
	}
	// A new UTC date does not close the current New York exchange date.
	earlyUTC := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
	if _, _, err := equityWindow(EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2026-10-11", "2026-10-11", earlyUTC); err == nil {
		t.Fatal("UTC midnight was mistaken for exchange midnight")
	}
	if _, _, err := equityWindow(EquityListing{"NVDA", "NASDAQ", "XNGS"}, "2026-10-10", "2026-10-10", earlyUTC); err != nil {
		t.Fatal(err)
	}
}

func TestUSArcaETFRequiresMatchingMICAndType(t *testing.T) {
	payload := equityPayload()
	meta := payload["meta"].(map[string]any)
	meta["symbol"], meta["exchange"], meta["mic_code"], meta["type"] = "SPY", "NYSE", "ARCX", "ETF"
	listing := EquityListing{"SPY", "NYSE", "ARCX"}
	if _, e := parseEquity(encode(t, payload), listing, "2024-06-07", "2024-06-10", checkedAt); e != nil {
		t.Fatal(e)
	}
	meta["type"] = "Common Stock"
	if _, e := parseEquity(encode(t, payload), listing, "2024-06-07", "2024-06-10", checkedAt); e == nil {
		t.Fatal("stock silently accepted as ARCA ETF")
	}
	meta["type"] = "ETF"
	meta["mic_code"] = "XNYS"
	if _, e := parseEquity(encode(t, payload), listing, "2024-06-07", "2024-06-10", checkedAt); e == nil {
		t.Fatal("same ticker on another venue accepted")
	}
}
