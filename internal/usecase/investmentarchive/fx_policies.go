package investmentarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	md "github.com/positron48/budget/internal/usecase/marketdata"
	"sort"
	"time"
)

// Source bindings are portable reviewed configuration; licensed observations
// and derived rates remain excluded. Validate every revision before restore.
func VerifyFXPolicies(data Dataset) error {
	type row struct {
		Tenant   string          `json:"tenant_id"`
		Key      string          `json:"policy_key"`
		Version  int64           `json:"version"`
		Data     json.RawMessage `json:"data"`
		Hash     string          `json:"payload_hash"`
		Actor    string          `json:"actor_id"`
		Recorded time.Time       `json:"recorded_at"`
	}
	heads := map[string]row{}
	chains := map[string][]row{}
	actors := map[string]bool{}
	for _, a := range data.Actors {
		actors[a] = true
	}
	bad := func() error { return fmt.Errorf("archived FX policy evidence is inconsistent") }
	for _, table := range data.Tables {
		if table.Name != "investment_fx_policies" && table.Name != "investment_fx_policy_revisions" {
			continue
		}
		for _, raw := range table.Rows {
			var r row
			if json.Unmarshal(raw, &r) != nil || r.Tenant != data.TenantID || r.Version < 1 {
				return bad()
			}
			if table.Name == "investment_fx_policies" {
				if _, ok := heads[r.Key]; ok {
					return bad()
				}
				heads[r.Key] = r
			} else {
				chains[r.Key] = append(chains[r.Key], r)
			}
		}
	}
	if len(heads) != len(chains) {
		return bad()
	}
	for key, head := range heads {
		chain := chains[key]
		if int64(len(chain)) != head.Version {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		for i, r := range chain {
			var p md.FXPolicy
			if json.Unmarshal(r.Data, &p) != nil || r.Version != int64(i+1) || p.Key != key || !actors[r.Actor] || r.Recorded.IsZero() || md.ValidateFXPolicy(p) != nil {
				return bad()
			}
			raw, _ := json.Marshal(struct {
				Policy md.FXPolicy
				Actor  string
			}{p, r.Actor})
			sum := sha256.Sum256(raw)
			if r.Hash != hex.EncodeToString(sum[:]) {
				return bad()
			}
		}
	}
	return nil
}
