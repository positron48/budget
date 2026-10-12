package investmentarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	inv "github.com/positron48/budget/internal/usecase/investment"
	"reflect"
	"sort"
	"time"
)

func VerifyDividendHistories(data Dataset) error {
	type row struct {
		Tenant   string          `json:"tenant_id"`
		ID       string          `json:"id"`
		History  string          `json:"history_id"`
		Version  int64           `json:"version"`
		Data     json.RawMessage `json:"data"`
		Hash     string          `json:"payload_hash"`
		Actor    string          `json:"actor_id"`
		Reason   string          `json:"reason"`
		Recorded time.Time       `json:"recorded_at"`
	}
	bad := func() error { return fmt.Errorf("archived dividend history is inconsistent") }
	heads := map[string]row{}
	chains := map[string][]row{}
	actors := map[string]bool{}
	for _, a := range data.Actors {
		actors[a] = true
	}
	for _, t := range data.Tables {
		if t.Name != "investment_dividend_histories" && t.Name != "investment_dividend_history_revisions" {
			continue
		}
		for _, raw := range t.Rows {
			var r row
			if json.Unmarshal(raw, &r) != nil || r.Tenant != data.TenantID || r.Version < 1 {
				return bad()
			}
			if t.Name == "investment_dividend_histories" {
				if _, ok := heads[r.ID]; ok {
					return bad()
				}
				heads[r.ID] = r
			} else {
				chains[r.History] = append(chains[r.History], r)
			}
		}
	}
	if len(heads) != len(chains) {
		return bad()
	}
	identities := map[string]bool{}
	for key, head := range heads {
		chain := chains[key]
		if int64(len(chain)) != head.Version {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		identity := ""
		for i, r := range chain {
			var h inv.DividendHistory
			if json.Unmarshal(r.Data, &h) != nil || r.History != key || h.ID != key || h.Version != r.Version || r.Version != int64(i+1) || !actors[r.Actor] || r.Recorded.IsZero() || h.BasisAt.After(r.Recorded) || r.Reason != h.Reason {
				return bad()
			}
			normalized, e := inv.NormalizeDividendHistory(h)
			if e != nil || !reflect.DeepEqual(h, normalized) {
				return bad()
			}
			pair := h.InstrumentKey + "\x00" + h.Currency
			if i == 0 {
				identity = pair
				if identities[pair] {
					return bad()
				}
				identities[pair] = true
			} else if identity != pair {
				return bad()
			}
			if i == len(chain)-1 {
				var current inv.DividendHistory
				if json.Unmarshal(head.Data, &current) != nil || !reflect.DeepEqual(current, h) {
					return bad()
				}
				raw, _ := json.Marshal(struct {
					History inv.DividendHistory
					Actor   string
				}{h, r.Actor})
				sum := sha256.Sum256(raw)
				if head.Hash != hex.EncodeToString(sum[:]) {
					return bad()
				}
			}
		}
	}
	return nil
}
