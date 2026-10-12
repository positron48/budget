package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func TestDividendProviderContractExactAmountsRecordRange(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	record := "2024-01-05T00:00:00Z"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "GetInstrumentBy") {
			if body["id"] != id {
				t.Error("wrong UID")
			}
			if _, err := fmt.Fprintf(w, `{"instrument":{"uid":%q,"ticker":"TEST","name":"Synthetic","instrumentType":"share","currency":"rub"}}`, id); err != nil {
				t.Error(err)
			}
			return
		}
		if !strings.HasSuffix(r.URL.Path, "GetDividends") || body["instrumentId"] != id || body["from"] != "2024-01-01T00:00:00Z" || body["to"] != "2024-01-31T00:00:00Z" {
			t.Error("wrong dividend request", body)
		}
		if _, err := fmt.Fprintf(w, `{"dividends":[{"recordDate":%q,"paymentDate":"2024-03-01T00:00:00Z","dividendNet":{"units":"9007199254740993","nano":1,"currency":"rub"},"dividendType":"Regular Cash"},{"recordDate":"2024-01-06T00:00:00Z","dividendType":"Cancelled"}]}`, record); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	c, err := newClient("synthetic-credential")
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = server.URL + "/"
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "dividend_facts", InstrumentID: id, From: from, To: from.AddDate(0, 0, 30)}
	f, err := c.LoadDividendFacts(context.Background(), q)
	if err != nil || f.Dividends[0].DividendNet.Amount != "9007199254740993.000000001" || f.Dividends[1].DividendNet != nil || !f.Dividends[1].PaymentDate.IsZero() || !f.Dividends[0].PaymentDate.After(q.To) {
		t.Fatal(f, err)
	}
	record = "2024-02-01T00:00:00Z"
	if _, err = c.LoadDividendFacts(context.Background(), q); err == nil {
		t.Fatal("out of record-date range accepted")
	}
	before := calls
	q.InstrumentID = "invalid"
	if _, err = c.LoadDividendFacts(context.Background(), q); err == nil || calls != before {
		t.Fatal("invalid request used network")
	}
}
