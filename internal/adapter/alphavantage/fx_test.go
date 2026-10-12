package alphavantage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fixture = `{"Meta Data":{"1. Information":"Forex Daily Prices","2. From Symbol":"USD","3. To Symbol":"RUB","4. Last Refreshed":"2026-10-12 00:00:00","5. Output Size":"Full size","6. Time Zone":"UTC"},"Time Series FX (Daily)":{"2026-10-12":{"1. open":"80","2. high":"82","3. low":"79","4. close":"81"},"2026-10-09":{"1. open":"80.12345678901","2. high":"82.12345678901","3. low":"79.12345678901","4. close":"81.12345678901"},"2018-01-01":{"1. open":"1","2. high":"1","3. low":"1","4. close":"1"}}}`

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func TestFullResponseFiltersWindowWithoutInventingPublication(t *testing.T) {
	a, err := Parse([]byte(fixture), "USD/RUB", "2019-10-01", "2026-10-11", now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Bars) != 1 || a.Bars[0].Close != "81.12345678901" || a.ExcludedOutsideWindow != 2 || c.FinanciallyApproved || c.PublicationAndValidityVerified || c.TrailingDays != 2 {
		t.Fatal("wrong precision, window or approval")
	}
	for _, text := range []string{fmt.Sprint(&Client{token: "private-token"}), fmt.Sprintf("%#v", &Client{token: "private-token"})} {
		if strings.Contains(text, "private-token") {
			t.Fatal("credential formatting")
		}
	}
}
func TestRejectsInvalidIdentityAmbiguityAndProviderMessages(t *testing.T) {
	for _, body := range []string{
		strings.Replace(fixture, `"RUB"`, `"EUR"`, 1), strings.Replace(fixture, `"Full size"`, `"Compact"`, 1), strings.Replace(fixture, `"UTC"`, `""`, 1),
		strings.Replace(fixture, `"81.12345678901"`, `"0"`, 1), strings.Replace(fixture, `"81.12345678901"`, `"1e2"`, 1), strings.Replace(fixture, `"81.12345678901"`, `"83"`, 1),
		strings.Replace(fixture, `"2026-10-09":`, `"2026-10-12":`, 1), strings.Replace(fixture, `"2. From Symbol":"USD"`, `"2. From Symbol":"EUR","2. From Symbol":"USD"`, 1),
		`{"Information":"private-token denied"}`, `{"Note":"private-token quota"}`, `{"Error Message":"private-token invalid"}`, fixture + "{}", strings.Repeat("x", maxBody+1),
	} {
		_, err := Parse([]byte(body), "USD/RUB", "2019-10-01", "2026-10-11", now)
		if err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("unsafe parse: %v", err)
		}
	}
	a, err := Parse([]byte(fixture), "USD/RUB", "2019-10-01", "2026-10-11", now)
	if err != nil {
		t.Fatal(err)
	}
	a.Bars = append(a.Bars, a.Bars[0])
	if _, err = a.Validate(); err == nil {
		t.Fatal("offline duplicate accepted")
	}
}
func TestTransportAuthenticationAndSecretRedaction(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if q.Get("apikey") != "private-token" || q.Get("function") != "FX_DAILY" || q.Get("outputsize") != "full" {
			t.Error("wrong request")
		}
		_, _ = w.Write([]byte(fixture))
	}))
	defer server.Close()
	c := &Client{token: "private-token", endpoint: server.URL, http: server.Client()}
	if _, err := c.History(context.Background(), "USD/RUB", "2019-10-01", "2026-10-11"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("request count")
	}
	// A redirect must never forward the key to another host.
	next := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") }))
	defer next.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, next.URL+"?apikey=private-token", http.StatusFound)
	}))
	defer redirect.Close()
	c.endpoint = redirect.URL
	c.http = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("private-token redirect") }}
	_, err := c.History(context.Background(), "USD/RUB", "2019-10-01", "2026-10-11")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("unsafe redirect diagnostic")
	}
	c.endpoint = "http://127.0.0.1:1"
	_, err = c.History(context.Background(), "USD/RUB", "2019-10-01", "2026-10-11")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("unsafe transport diagnostic")
	}
}

func TestMetadataOrdinalsDoNotOverrideExplicitFieldNames(t *testing.T) {
	reordered := strings.ReplaceAll(strings.ReplaceAll(fixture, "4. Last Refreshed", "5. Last Refreshed"), "5. Output Size", "4. Output Size")
	if _, err := Parse([]byte(reordered), "USD/RUB", "2019-10-01", "2026-10-11", now); err != nil {
		t.Fatal(err)
	}
	ambiguous := strings.Replace(reordered, `"4. Output Size":"Full size"`, `"4. Output Size":"Full size","7. Output Size":"Full size"`, 1)
	if _, err := Parse([]byte(ambiguous), "USD/RUB", "2019-10-01", "2026-10-11", now); err == nil {
		t.Fatal("ambiguous metadata accepted")
	}
}

func TestDateOnlyRefreshDoesNotInventAnInstant(t *testing.T) {
	body := strings.Replace(fixture, "2026-10-12 00:00:00", "2026-10-12", 1)
	a, err := Parse([]byte(body), "USD/RUB", "2019-10-01", "2026-10-11", now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Validate()
	if err != nil || a.LastRefreshed != "2026-10-12" || c.PublicationAndValidityVerified {
		t.Fatal("invented publication instant")
	}
}

func TestShortWeekdayGapIsReportedWithoutInventingTradingCalendar(t *testing.T) {
	a := Audit{Source: "alphavantage", Pair: "USD/RUB", From: "2026-04-30", To: "2026-05-04", Timezone: "UTC", LastRefreshed: "2026-05-04", BodySHA256: strings.Repeat("a", 64), RetrievedAt: now, Bars: []Bar{{"2026-04-30", "80", "80", "80", "80"}, {"2026-05-04", "81", "81", "81", "81"}}}
	c, err := a.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if c.WeekdayDatesWithoutBar != 1 || len(c.MissingWeekdayExamples) != 1 || c.MissingWeekdayExamples[0] != "2026-05-01" || len(c.GapsOver7Days) != 0 || c.TradingCalendarVerified {
		t.Fatal("short calendar gap omitted or holiday inferred")
	}
}
