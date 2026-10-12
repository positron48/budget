package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstrumentMetadataIdentity(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	responseID := id
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["id"] != id || request["idType"] != "INSTRUMENT_ID_TYPE_UID" {
			t.Error("incorrect metadata request")
		}
		if _, err := fmt.Fprintf(w, `{"instrument":{"uid":%q,"ticker":"TEST","classCode":"TQBR","name":"Test listing","instrumentType":"share","currency":"rub"}}`, responseID); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, _ := newClient("fake-test-credential")
	client.baseURL = server.URL + "/"
	item, err := client.LoadMarketInstrument(context.Background(), id)
	if err != nil || item.ID != id || item.Currency != "RUB" || item.Kind != "share" {
		t.Fatal(item, err)
	}
	responseID = "22222222-2222-4222-8222-222222222222"
	if _, err = client.LoadMarketInstrument(context.Background(), id); err == nil {
		t.Fatal("wrong UID accepted")
	}
}

func TestCatalogSearchBoundsAndExactRanking(t *testing.T) {
	const exactID = "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]string
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["query"] != "test" || request["instrumentKind"] != "INSTRUMENT_TYPE_SHARE" {
			t.Error("invalid scoped search")
		}
		items := make([]Instrument, 0, 1002)
		for n := 1; n <= 1001; n++ {
			items = append(items, Instrument{UID: fmt.Sprintf("%08x-2222-4222-8222-222222222222", n), Ticker: fmt.Sprintf("A%d", n), Name: "Test company", InstrumentType: "share"})
		}
		items = append(items, Instrument{UID: exactID, Ticker: "TEST", Name: "Exact listing", InstrumentType: "share"})
		if err := json.NewEncoder(w).Encode(map[string]any{"instruments": items}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, _ := newClient("fake-test-credential")
	client.baseURL = server.URL + "/"
	result, err := client.SearchMarketCatalog(context.Background(), md.JobRequest{Kind: "catalog_search", SearchQuery: " TEST ", InstrumentKind: "share"})
	if err != nil || len(result.Instruments) != 1000 || !result.Truncated || result.Instruments[0].ID != exactID || result.Instruments[0].Detailed {
		t.Fatal("incorrect bounded candidates", len(result.Instruments), err)
	}
}
