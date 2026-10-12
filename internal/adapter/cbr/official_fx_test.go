package cbr

import (
	"context"
	"fmt"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOfficialFXDailyHistoryAndFailures(t *testing.T) {
	from := time.Date(2019, 10, 5, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	for _, mode := range []string{"ok", "truncated", "missing-eur", "wrong-id", "conflict", "future", "http-error", "inexact", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "http-error" && r.URL.Query().Get("VAL_NM_RQ") == "EUR" {
					w.WriteHeader(503)
					return
				}
				if strings.Contains(r.URL.Path, "daily") {
					date, amount := "04.10.2019", "250,1234"
					if r.URL.Query().Get("date_req") == "07/10/2019" {
						date, amount = "07.10.2019", "300,1234"
					}
					_, _ = fmt.Fprintf(w, `<ValCurs Date="%s">`, date)
					for _, code := range []string{"USD", "EUR"} {
						if mode == "missing-eur" && code == "EUR" {
							continue
						}
						_, _ = fmt.Fprintf(w, `<Valute ID="%s"><CharCode>%s</CharCode><Nominal>100</Nominal><Value>%s</Value></Valute>`, code, code, amount)
					}
					_, _ = fmt.Fprint(w, `</ValCurs>`)
					if mode == "trailing" {
						_, _ = fmt.Fprint(w, `<Unexpected/>`)
					}
					return
				}
				id := r.URL.Query().Get("VAL_NM_RQ")
				if mode == "wrong-id" {
					id = "WRONG"
				}
				_, _ = fmt.Fprintf(w, `<ValCurs ID="%s">`, id)
				if mode != "truncated" {
					date := "07.10.2019"
					if mode == "future" {
						date = "08.10.2019"
					}
					nominal := "100"
					if mode == "inexact" {
						nominal = "3"
					}
					_, _ = fmt.Fprintf(w, `<Record Date="%s" Id="%s"><Nominal>%s</Nominal><Value>300,1234</Value></Record>`, date, id, nominal)
				}
				if mode == "conflict" {
					_, _ = fmt.Fprintf(w, `<Record Date="07.10.2019" Id="%s"><Nominal>100</Nominal><Value>301,0000</Value></Record>`, id)
				}
				_, _ = fmt.Fprint(w, `</ValCurs>`)
				if mode == "trailing" {
					_, _ = fmt.Fprint(w, `<Unexpected/>`)
				}
			}))
			defer srv.Close()
			client := &Client{HTTP: srv.Client(), BaseURL: srv.URL + "/"}
			points, e := client.FetchOfficialFX(context.Background(), from, to)
			if mode != "ok" {
				if e == nil {
					t.Fatal("invalid official source accepted", mode)
				}
				return
			}
			if e != nil || len(points) != 6 || points[0].Rate != "2.501234" || points[2].Rate != "3.001234" || !points[0].At.Equal(from.Add(-3*time.Hour)) || !points[0].ValidUntil.Equal(from.Add(21*time.Hour)) || !strings.Contains(points[0].RecordReference, "04/10/2019") {
				t.Fatal(e, points)
			}
			p := md.FXPolicy{Key: "cbr", Mode: "cbr", Source: "cbr", AccessScope: "public", SourceReference: "CBR", TimeConvention: "Effective Moscow date", ValueConvention: "Per unit", ValidityConvention: "Confirmed daily snapshots", Official: &md.OfficialFXPolicy{From: from, Convention: "effective_moscow_date_v1"}}
			if e = md.ValidateOfficialFXPoints(p, points, from, to); e != nil {
				t.Fatal(e)
			}
			duplicate := append([]md.FXPoint(nil), points...)
			duplicate[1] = duplicate[0]
			if md.ValidateOfficialFXPoints(p, duplicate, from, to) == nil {
				t.Fatal("duplicate masking gap accepted")
			}
			retry, e := client.FetchOfficialFX(context.Background(), from, to)
			if e != nil || retry[0].EvidenceSHA256 != points[0].EvidenceSHA256 {
				t.Fatal("unstable source record hash", e)
			}
		})
	}
}
