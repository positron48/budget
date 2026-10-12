package tinvest

import (
	"context"
	"encoding/json"
	"fmt"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBondFactsExactMissingAndAtomicFailure(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	q := md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: from, To: from.AddDate(0, 0, 30)}
	mode := "ok"
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		calls = append(calls, method)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if method == "BondBy" {
			if body["id"] != id || body["idType"] != "INSTRUMENT_ID_TYPE_UID" {
				t.Error(body)
			}
		} else if body["instrumentId"] != id || body["from"] != "2024-01-01T00:00:00Z" {
			t.Error(body)
		}
		switch method {
		case "BondBy":
			if _, err := fmt.Fprintf(w, `{"instrument":{"uid":%q,"figi":"TESTFIGI","isin":"TESTISIN","ticker":"TEST","classCode":"TQOB","name":"Bond","currency":"rub","nominal":{"units":"9007199254740993","nano":1,"currency":"rub"},"initialNominal":{"units":"1000","currency":"rub"},"aciValue":{"currency":"rub"},"floatingCouponFlag":false,"couponQuantityPerYear":0}}`, id); err != nil {
				t.Error(err)
			}
		case "GetBondCoupons":
			if mode == "foreign" {
				if _, err := fmt.Fprint(w, `{"events":[{"figi":"OTHER","couponNumber":"1","couponDate":"2024-01-05T00:00:00Z"}]}`); err != nil {
					t.Error(err)
				}
			} else {
				if _, err := fmt.Fprint(w, `{"events":[{"figi":"TESTFIGI","couponNumber":"9007199254740993","couponDate":"2024-01-05T00:00:00Z","couponType":"COUPON_TYPE_FLOATING"}]}`); err != nil {
					t.Error(err)
				}
			}
		case "GetBondEvents":
			if mode == "failure" {
				w.WriteHeader(503)
				if _, err := fmt.Fprint(w, `{"message":"private provider details"}`); err != nil {
					t.Error(err)
				}
			} else {
				if _, err := fmt.Fprintf(w, `{"events":[{"instrumentId":%q,"eventNumber":1,"eventType":"EVENT_TYPE_CALL","eventDate":"2024-01-06T00:00:00Z","payOneBond":{"currency":"rub"},"couponInterestRate":{"units":"5","nano":1}}]}`, id); err != nil {
					t.Error(err)
				}
			}
		case "GetAccruedInterests":
			if _, err := fmt.Fprint(w, `{"accruedInterests":[{"date":"2024-01-07T00:00:00Z","value":{"nano":1},"nominal":{"units":"1000"}},{"date":"2024-01-08T00:00:00Z","value":{}}]}`); err != nil {
				t.Error(err)
			}
		default:
			t.Error(method)
		}
	}))
	defer server.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = server.URL + "/"
	f, err := c.LoadBondFacts(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 || f.Terms.Nominal.Amount != "9007199254740993.000000001" || f.Terms.CurrentAccrued.Amount != "0.000000000" || f.Terms.Floating == nil || *f.Terms.Floating || f.Terms.Perpetual != nil || f.Terms.CouponFrequency == nil || *f.Terms.CouponFrequency != 0 {
		t.Fatal("metadata precision or optionality", f)
	}
	if f.Coupons[0].Number != 9007199254740993 || f.Coupons[0].Pay != nil || f.Events[0].Pay.Amount != "0.000000000" || *f.Events[0].AnnualRate != "5.000000001" || *f.Accrued[0].Value != "0.000000001" || f.Accrued[0].Percent != nil || *f.Accrued[1].Value != "0.000000000" {
		t.Fatal("source values changed", f)
	}
	for _, next := range []string{"foreign", "failure"} {
		mode = next
		calls = nil
		if _, err = c.LoadBondFacts(context.Background(), q); err == nil {
			t.Fatal("accepted", next)
		}
		if len(calls) > 3 {
			t.Fatal("continued after provider failure", calls)
		}
	}
}

func TestBondFutureScheduleDoesNotFetchFutureAccrued(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/BondBy"):
			if _, err := fmt.Fprintf(w, `{"instrument":{"uid":%q,"ticker":"BOND","name":"Bond"}}`, id); err != nil {
				t.Error(err)
			}
		case strings.HasSuffix(r.URL.Path, "/GetAccruedInterests"):
			t.Error("future accrued interest requested")
		default:
			if _, err := fmt.Fprint(w, `{}`); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()
	c, _ := newClient("fake-test-credential")
	c.baseURL = server.URL + "/"
	from := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	f, err := c.LoadBondFacts(context.Background(), md.JobRequest{Kind: "bond_facts", InstrumentID: id, From: from, To: from.Add(24 * time.Hour)})
	if err != nil || calls != 3 || len(f.Accrued) != 0 {
		t.Fatal(calls, err)
	}
}
