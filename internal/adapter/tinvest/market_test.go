package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuotationExact(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"units":"9007199254740993","nano":1}`, "9007199254740993.000000001"},
		{`{"units":-1,"nano":-1}`, "-1.000000001"},
		{`{"nano":-1}`, "-0.000000001"},
		{`{"units":"1","nano":-1}`, ""},
		{`{"nano":1000000000}`, ""},
		{`{"units":"9223372036854775808"}`, ""},
	} {
		var q Quotation
		if err := json.Unmarshal([]byte(tc.raw), &q); err != nil {
			t.Fatal(err)
		}
		got, err := q.Decimal()
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatalf("%s: %s %v", tc.raw, got, err)
		}
	}
}

func TestMarketRequestsAndValidation(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	response := `{"lastPrices":[{"instrumentUid":"` + id + `","price":{"units":"123","nano":1},"time":"2026-10-08T10:00:00Z","lastPriceType":"LAST_PRICE_EXCHANGE"}]}`
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["lastPriceType"] != "LAST_PRICE_EXCHANGE" && (body["candleSourceType"] != "CANDLE_SOURCE_EXCHANGE" || body["interval"] != "CANDLE_INTERVAL_DAY") {
			t.Error("missing exchange-only request")
		}
		if _, err := fmt.Fprint(w, response); err != nil {
			t.Error(err)
		}
	}))
	defer s.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = s.URL + "/"
	if _, err := c.ExchangeLastPrices(context.Background(), nil); err == nil || calls != 0 {
		t.Fatal("empty request can fetch entire catalog")
	}
	prices, err := c.ExchangeLastPrices(context.Background(), []string{id})
	if err != nil || len(prices) != 1 {
		t.Fatalf("%v %v", prices, err)
	}
	response = `{"lastPrices":[{"instrumentUid":"` + id + `","time":"2026-10-08T10:00:00Z","lastPriceType":"LAST_PRICE_DEALER"}]}`
	if _, err = c.ExchangeLastPrices(context.Background(), []string{id}); err == nil {
		t.Fatal("dealer price accepted as exchange")
	}
	response = `{"lastPrices":[{"instrumentUid":"` + id + `","lastPriceType":"LAST_PRICE_UNSPECIFIED"}]}`
	prices, err = c.ExchangeLastPrices(context.Background(), []string{id})
	if err != nil || len(prices) != 0 {
		t.Fatal("missing price placeholder must not become a zero price", err)
	}
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	response = `{"candles":[{"open":{"units":"10"},"high":{"units":"12"},"low":{"units":"9"},"close":{"units":"11"},"time":"2026-10-08T00:00:00Z","isComplete":false,"candleSource":"CANDLE_SOURCE_EXCHANGE"}]}`
	candles, err := c.ExchangeDailyCandles(context.Background(), id, from, from.Add(24*time.Hour))
	if err != nil || len(candles) != 1 || candles[0].Complete {
		t.Fatalf("%v %v", candles, err)
	}
	response = `{"candles":[]}`
	candles, err = c.ExchangeDailyCandles(context.Background(), id, from, from.Add(24*time.Hour))
	if err != nil || len(candles) != 0 {
		t.Fatal("empty history must stay empty")
	}
}
