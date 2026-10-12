package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCurrencyMetadataIdentityAndNominal(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	good := `{"instrument":{"uid":"` + id + `","ticker":"TEST","classCode":"CETS","currency":"rub","isoCurrencyName":"jpy","lot":1000,"nominal":{"currency":"jpy","units":"100","nano":1},"realExchange":"REAL_EXCHANGE_MOEX","first1dayCandleDate":"2019-01-01T00:00:00Z"}}`
	response := good
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "InstrumentsService/CurrencyBy") {
			t.Error(r.URL.Path)
		}
		var body map[string]string
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["id"] != id || body["idType"] != "INSTRUMENT_ID_TYPE_UID" {
			t.Error(body)
		}
		if _, err := fmt.Fprint(w, response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = server.URL + "/"
	got, e := c.CurrencyByUID(context.Background(), id)
	if e != nil || got.Currency != "RUB" || got.Nominal.Currency != "JPY" || got.ISOCurrency != "JPY" || got.Lot != 1000 || got.FirstDaily.Year() != 2019 {
		t.Fatal(got, e)
	}
	nominal, _ := got.Nominal.Decimal()
	if nominal != "100.000000001" {
		t.Fatal(nominal)
	}
	for _, bad := range []string{strings.Replace(good, id, "22222222-2222-4222-8222-222222222222", 1), strings.Replace(good, `"lot":1000`, `"lot":0`, 1), strings.Replace(good, `"units":"100","nano":1`, `"units":"0","nano":0`, 1), strings.Replace(good, `"currency":"rub"`, `"currency":"ruble"`, 1)} {
		response = bad
		if _, e = c.CurrencyByUID(context.Background(), id); e == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	before := calls
	if _, e = c.CurrencyByUID(context.Background(), "invalid"); e == nil || calls != before {
		t.Fatal("invalid UID sent")
	}
}

func TestAllCurrencyCatalogIncludesArchivedListings(t *testing.T) {
	good := `{"instruments":[{"uid":"11111111-1111-4111-8111-111111111111","ticker":"ARCHIVED_EUR","classCode":"CETS","currency":"rub","isoCurrencyName":"eur","lot":1000,"nominal":{"currency":"eur","units":"1","nano":0}}]}`
	response := good
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "InstrumentsService/Currencies") {
			t.Error(r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["instrumentStatus"] != "INSTRUMENT_STATUS_ALL" {
			t.Error(body, err)
		}
		if _, err := fmt.Fprint(w, response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = server.URL + "/"
	items, err := c.AllCurrencies(context.Background())
	if err != nil || len(items) != 1 || items[0].Metadata.ISOCurrency != "EUR" || items[0].Metadata.Nominal.Currency != "EUR" {
		t.Fatal(items, err)
	}
	for _, bad := range []string{strings.Replace(good, `"uid":"11111111-1111-4111-8111-111111111111"`, `"uid":"invalid"`, 1), `{"instruments":[` + strings.Repeat(strings.TrimSuffix(strings.TrimPrefix(good, `{"instruments":[`), `]}`)+",", 1000) + strings.TrimSuffix(strings.TrimPrefix(good, `{"instruments":[`), `]}`) + `]}`} {
		response = bad
		if _, err = c.AllCurrencies(context.Background()); err == nil {
			t.Fatal("invalid/oversized catalog accepted")
		}
	}
	response = strings.Replace(good, `"lot":1000`, `"lot":0`, 1)
	items, err = c.AllCurrencies(context.Background())
	if err != nil || len(items) != 1 || items[0].ValidationError == "" || items[0].Metadata.Lot != 0 {
		t.Fatal("unusable archive terms lost or accepted as valid", items, err)
	}
	item := strings.TrimSuffix(strings.TrimPrefix(good, `{"instruments":[`), `]}`)
	response = `{"instruments":[` + item + `,` + item + `]}`
	if _, err = c.AllCurrencies(context.Background()); err == nil {
		t.Fatal("duplicate UID accepted")
	}
}
