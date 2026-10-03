package cbr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUnitRate(t *testing.T) {
	for _, tt := range []struct{ nominal, value, want string }{{"100", "25,6456", "0.25645600"}, {"10", "28,9122", "2.89122000"}, {"1", "101,6797", "101.67970000"}} {
		got, err := unitRate(value{Nominal: tt.nominal, Value: tt.value})
		if err != nil || got != tt.want {
			t.Fatalf("%+v: %s %v", tt, got, err)
		}
	}
	for _, v := range []value{{"0", "1"}, {"-1", "1"}, {"1", "0"}, {"1", "bad"}} {
		if _, err := unitRate(v); err == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
}

func TestFetch(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 9)
	for _, mode := range []string{"ok", "missing", "truncated", "wrong-id", "future", "http-error", "html"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http-error" {
					w.WriteHeader(503)
					return
				}
				if mode == "html" {
					_, _ = fmt.Fprint(w, "<html>oops</html>")
					return
				}
				if strings.Contains(r.URL.Path, "daily") {
					date, amount := "29.12.2024", "250,0000"
					if r.URL.Query().Get("date_req") == "10/01/2025" {
						date, amount = "10.01.2025", "300,0000"
					}
					_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="windows-1251"?><ValCurs Date="%s">`, date)
					for _, code := range currencies {
						if mode == "missing" && code == "USD" {
							continue
						}
						_, _ = fmt.Fprintf(w, `<Valute ID="%s"><CharCode>%s</CharCode><Nominal>100</Nominal><Value>%s</Value></Valute>`, code, code, amount)
					}
					_, _ = fmt.Fprint(w, "</ValCurs>")
					return
				}
				id := r.URL.Query().Get("VAL_NM_RQ")
				if mode == "wrong-id" {
					id = "wrong"
				}
				_, _ = fmt.Fprintf(w, `<ValCurs ID="%s">`, id)
				if mode != "truncated" {
					date := "10.01.2025"
					if mode == "future" {
						date = "11.01.2025"
					}
					_, _ = fmt.Fprintf(w, `<Record Date="%s" Id="%s"><Nominal>100</Nominal><Value>300,0000</Value></Record>`, date, id)
				}
				_, _ = fmt.Fprint(w, "</ValCurs>")
			}))
			defer srv.Close()
			client := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/"}
			quotes, err := client.Fetch(context.Background(), from, to)
			if mode != "ok" {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(quotes) != 18 || quotes[0].Rate != "2.50000000" || quotes[0].Date.Format(time.DateOnly) != "2024-12-29" || quotes[17].Rate != "3.00000000" {
				t.Fatalf("unexpected quotes %+v", quotes)
			}
		})
	}
}
