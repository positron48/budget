package moex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const description = `{"description":{"columns":["name","value"],"data":[["SECID","SBER"],["ISIN","RU0009029540"]]}}`
const columns = `"BOARDID","TRADEDATE","SECID","OPEN","HIGH","LOW","CLOSE","VOLUME","CURRENCYID"`
const firstRow = `["TQBR","2024-06-07","SBER",313.8,320.64,312.73,319.9,44484260,"SUR"]`
const secondRow = `["TQBR","2024-06-10","SBER",320,322,318,321,123,"SUR"]`

func page(rows string, index, total string) string {
	return `{"history":{"columns":[` + columns + `],"data":[` + rows + `]},"history.cursor":{"columns":["INDEX","TOTAL","PAGESIZE"],"data":[[` + index + `,` + total + `,100]]}}`
}

func syntheticClient(t *testing.T, responses []string) *Client {
	t.Helper()
	c := New()
	calls := 0
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Host != "iss.moex.com" || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected ISS request")
		}
		if calls >= len(responses) {
			t.Fatal("unexpected extra request")
		}
		if calls == 0 && r.URL.Path != "/iss/securities/SBER.json" {
			t.Fatal(r.URL.Path)
		}
		if calls > 0 && (r.URL.Query().Get("start") != strconv.Itoa(calls-1) || r.URL.Query().Get("from") != "2024-06-07" || r.URL.Query().Get("till") != "2024-06-10") {
			t.Fatal(r.URL)
		}
		body := responses[calls]
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return c
}

func TestNativeHistoryChecksIdentityAndLoadsAllPages(t *testing.T) {
	c := syntheticClient(t, []string{description, page(firstRow, "0", "2"), page(secondRow, "1", "2")})
	l := Listing{"SBER", "RU0009029540", "TQBR"}
	h, err := c.History(context.Background(), l, "2024-06-07", "2024-06-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Observations) != 2 || len(h.ResponseHashes) != 3 || h.Observations[0].Price != "319.900000000" || h.Observations[1].Price != "321.000000000" || h.Observations[0].Currency != "RUB" || h.Observations[0].InstrumentID != l.ID() || h.Observations[0].Adjustment != "unadjusted" {
		t.Fatal(h)
	}
	other := l
	other.Board = "TQTF"
	if l.ID() == other.ID() {
		t.Fatal("venue identities collided")
	}
}

func TestNativeHistoryRejectsIdentityPaginationAndOHLCDrift(t *testing.T) {
	for name, body := range map[string]string{
		"wrong board":           page(strings.Replace(firstRow, "TQBR", "TQTF", 1), "0", "1"),
		"wrong security":        page(strings.Replace(firstRow, "SBER", "OTHER", 1), "0", "1"),
		"wrong currency":        page(strings.Replace(firstRow, "SUR", "USD", 1), "0", "1"),
		"wrong date":            page(strings.Replace(firstRow, "2024-06-07", "2024-06-06", 1), "0", "1"),
		"bad OHLC":              page(strings.Replace(firstRow, "320.64", "310", 1), "0", "1"),
		"zero equity price":     page(`["TQBR","2024-06-07","SBER",0,0,0,0,1,"SUR"]`, "0", "1"),
		"bad pagination":        page(firstRow, "1", "2"),
		"duplicate session":     page(firstRow+","+firstRow, "0", "2"),
		"missing column":        strings.Replace(page(firstRow, "0", "1"), "CLOSE", "MISSING", 1),
		"empty page before end": page("", "0", "1"),
	} {
		t.Run(name, func(t *testing.T) {
			c := syntheticClient(t, []string{description, body})
			if _, err := c.History(context.Background(), Listing{"SBER", "RU0009029540", "TQBR"}, "2024-06-07", "2024-06-10"); err == nil {
				t.Fatal("corrupt history accepted")
			}
		})
	}
	c := syntheticClient(t, []string{strings.Replace(description, "RU0009029540", "RU0009029557", 1)})
	if _, err := c.History(context.Background(), Listing{"SBER", "RU0009029540", "TQBR"}, "2024-06-07", "2024-06-10"); err == nil {
		t.Fatal("ISIN mismatch accepted")
	}
}

func TestMissingCloseIsExplicitGapAndExactNumberIsPreserved(t *testing.T) {
	gap := strings.Replace(firstRow, "319.9", "null", 1)
	c := syntheticClient(t, []string{description, page(gap, "0", "1")})
	h, err := c.History(context.Background(), Listing{"SBER", "RU0009029540", "TQBR"}, "2024-06-07", "2024-06-10")
	if err != nil || len(h.Observations) != 0 || len(h.MissingSessions) != 1 || h.MissingSessions[0] != "2024-06-07" {
		t.Fatal(h, err)
	}
	exact := `["TQBR","2024-06-07","SBER",9007199254740993,9007199254740994,9007199254740992,9007199254740993.1,1,"SUR"]`
	c = syntheticClient(t, []string{description, page(exact, "0", "1")})
	h, err = c.History(context.Background(), Listing{"SBER", "RU0009029540", "TQBR"}, "2024-06-07", "2024-06-10")
	if err != nil || h.Observations[0].Price != "9007199254740993.100000000" {
		t.Fatal(h, err)
	}
}

func TestISSSafeTransportAndUnsupportedListing(t *testing.T) {
	c := New()
	c.http.Transport = transportFunc(func(_ *http.Request) (*http.Response, error) { return nil, errors.New("private transport diagnostics") })
	if _, err := c.History(context.Background(), Listing{"SBER", "RU0009029540", "TQBR"}, "2024-06-07", "2024-06-10"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	for _, listing := range []Listing{{"../SBER", "RU0009029540", "TQBR"}, {"SBER", "invalid", "TQBR"}, {"SBER", "RU0009029540", "TQOB"}} {
		if _, err := c.History(context.Background(), listing, "2024-06-07", "2024-06-10"); err == nil {
			t.Fatal("unsupported listing accepted")
		}
	}
}

func TestBondHistoryUsesSessionFaceValueAndNativeCurrency(t *testing.T) {
	desc := strings.ReplaceAll(strings.ReplaceAll(description, "SBER", "RU0000000001"), "RU0009029540", "RU0000000001")
	body := `{"history":{"columns":[` + columns + `,"FACEVALUE","FACEUNIT"],"data":[["TQCB","2024-06-07","RU0000000001",105,106,103,103.95,2430,"SUR",100,"USD"],["TQCB","2024-06-10","RU0000000001",104,105,102,103.5,100,"SUR",80,"USD"]]},"history.cursor":{"columns":["INDEX","TOTAL","PAGESIZE"],"data":[[0,2,100]]}}`
	c := New()
	calls := 0
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		text := desc
		if calls > 0 {
			text = body
			if !strings.Contains(r.URL.Path, "/markets/bonds/") || !strings.Contains(r.URL.Query().Get("history.columns"), "FACEVALUE,FACEUNIT") {
				t.Fatal(r.URL)
			}
		}
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text)), Request: r}, nil
	})
	l := Listing{"RU0000000001", "RU0000000001", "TQCB"}
	h, err := c.BondHistory(context.Background(), l, "2024-06-07", "2024-06-10")
	if err != nil || len(h.Observations) != 2 {
		t.Fatal(h, err)
	}
	if h.Observations[0].Currency != "USD" || h.Observations[0].NominalCurrency != "USD" || h.Observations[0].PriceUnit != "percent_of_nominal" || h.Observations[0].Nominal != "100.000000000" || h.Observations[1].Nominal != "80.000000000" || h.Observations[1].InstrumentID != l.BondID() {
		t.Fatal(h)
	}
	// Missing historical nominal remains a gap; current descriptor nominal
	// must never fill it, nor can a settlement-currency conversion be guessed.
	body = strings.Replace(body, `100,"USD"`, `null,"USD"`, 1)
	calls = 0
	h, err = c.BondHistory(context.Background(), l, "2024-06-07", "2024-06-10")
	if err != nil || len(h.Observations) != 1 || len(h.MissingSessions) != 1 {
		t.Fatal(h, err)
	}
}

func TestIMOEXUsesPriceIndexIdentityAndKeepsAbsentVolume(t *testing.T) {
	c := New()
	calls := 0
	c.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"description":{"columns":["name","value"],"data":[["SECID","IMOEX"],["CALCMODE","PR"],["CURRENCYID","RUB"]]}}`
		if calls > 0 {
			if !strings.Contains(r.URL.Path, "markets/index/boards/SNDX/securities/IMOEX.json") {
				t.Fatal(r.URL)
			}
			body = `{"history":{"columns":[` + columns + `],"data":[["SNDX","2024-06-07","IMOEX",3199.75,3268.89,3193.57,3233.22,null,"RUB"]]},"history.cursor":{"columns":["INDEX","TOTAL","PAGESIZE"],"data":[[0,1,100]]}}`
		}
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	h, e := c.IndexHistory(context.Background(), "2024-06-07", "2024-06-10")
	if e != nil || len(h.Observations) != 1 || h.Observations[0].PriceUnit != "index_points" || h.Observations[0].Volume != "" || h.Observations[0].InstrumentID != (Listing{SecurityID: "IMOEX", Board: "SNDX"}).IndexID() {
		t.Fatal(h, e)
	}
}
