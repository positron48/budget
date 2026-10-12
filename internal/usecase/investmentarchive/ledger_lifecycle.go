package investmentarchive

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// Financial versions can advance between metadata edits; account details must chain exactly.
func sameStoredMetadata(a, b storedLedger) bool {
	a.Started = a.Started.UTC()
	b.Started = b.Started.UTC()
	a.Created = a.Created.UTC()
	b.Created = b.Created.UTC()
	a.Version = 0
	b.Version = 0
	return reflect.DeepEqual(a, b)
}
func VerifyLedgerLifecycle(data Dataset) error {
	bad := func() error { return fmt.Errorf("archived ledger metadata history is inconsistent") }
	type revision struct {
		Tenant  string        `json:"tenant_id"`
		Ledger  string        `json:"ledger_id"`
		Version int64         `json:"version"`
		Action  string        `json:"action"`
		Actor   string        `json:"actor_id"`
		Before  *storedLedger `json:"before_data"`
		After   storedLedger  `json:"after_data"`
	}
	heads := map[string]storedLedger{}
	chains := map[string][]revision{}
	actors := map[string]bool{}
	for _, a := range data.Actors {
		actors[a] = true
	}
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			switch table.Name {
			case "investment_ledgers":
				var l storedLedger
				if json.Unmarshal(raw, &l) != nil {
					return bad()
				}
				heads[l.ID] = l
			case "investment_ledger_revisions":
				var v revision
				if json.Unmarshal(raw, &v) != nil {
					return bad()
				}
				chains[v.Ledger] = append(chains[v.Ledger], v)
			}
		}
	}
	for id, l := range heads {
		if l.MetadataVersion == 0 {
			continue
		} // Legacy schema fixtures have no metadata lifecycle.
		chain := chains[id]
		if int64(len(chain)) != l.MetadataVersion {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		for i, v := range chain {
			if v.Tenant != data.TenantID || v.Ledger != id || v.Version != int64(i+1) || v.After.ID != id || v.After.Tenant != data.TenantID || v.After.MetadataVersion != v.Version || !actors[v.Actor] {
				return bad()
			}
			if i == 0 {
				if v.Before != nil || v.After.Archived || (v.Action != "create" && v.Action != "baseline") {
					return bad()
				}
			} else {
				if v.Before == nil || !sameStoredMetadata(*v.Before, chain[i-1].After) {
					return bad()
				}
				switch v.Action {
				case "edit":
					if v.Before.Archived != v.After.Archived {
						return bad()
					}
				case "archive":
					if v.Before.Archived || !v.After.Archived {
						return bad()
					}
				case "restore":
					if !v.Before.Archived || v.After.Archived {
						return bad()
					}
				default:
					return bad()
				}
			}
		}
		if !sameStoredMetadata(chain[len(chain)-1].After, l) {
			return bad()
		}
		delete(chains, id)
	}
	for id := range chains {
		if heads[id].MetadataVersion == 0 {
			return bad()
		}
	}
	return nil
}
