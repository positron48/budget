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

func TestReadAccountHistoryIncludesTradesAndCommissions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("missing auth")
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "UsersService/GetAccounts"):
			if _, err := fmt.Fprint(w, `{"accounts":[{"id":"account","type":"ACCOUNT_TYPE_TINKOFF_IIS"}]}`); err != nil {
				t.Error(err)
			}
		case strings.HasSuffix(r.URL.Path, "OperationsService/GetOperationsByCursor"):
			if input["accountId"] != "account" || input["cursor"] != "next" || input["withoutCommissions"] != false || input["withoutTrades"] != false || input["withoutOvernights"] != false {
				t.Error("history filters lost records")
			}
			if _, err := fmt.Fprint(w, `{"hasNext":true,"nextCursor":"last","items":[{"id":"operation","unknownFutureField":"preserved"}]}`); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer s.Close()
	c, _ := newClient("synthetic")
	c.baseURL = s.URL + "/"
	a, err := c.Accounts(context.Background())
	if err != nil || len(a) != 1 || a[0].Type != "ACCOUNT_TYPE_TINKOFF_IIS" {
		t.Fatal(a, err)
	}
	p, err := c.Operations(context.Background(), "account", "2020-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "next")
	if err != nil || !p.HasNext || p.NextCursor != "last" || len(p.Items) != 1 || !strings.Contains(string(p.Items[0]), "unknownFutureField") {
		t.Fatal(p, err)
	}
}
