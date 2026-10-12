package investmentarchive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	inv "github.com/positron48/budget/internal/usecase/investment"
)

type archivedForecastRow struct {
	Tenant    string          `json:"tenant_id"`
	ID        string          `json:"id"`
	Event     string          `json:"event_id"`
	Operation string          `json:"operation_id"`
	Ledger    string          `json:"ledger_id"`
	Version   int64           `json:"version"`
	Data      json.RawMessage `json:"data"`
	Actor     string          `json:"actor_id"`
	Reason    string          `json:"reason"`
	Hash      string          `json:"payload_hash"`
	Recorded  time.Time       `json:"recorded_at"`
}

// Forecast evidence is portable personal data. Validate its version chains as
// well as FK targets before restore; a resealed malformed schedule is not safe.
func VerifyForecastEvidence(data Dataset) error {
	bad := func() error { return fmt.Errorf("archived forecast evidence is inconsistent") }
	events := map[string]archivedForecastRow{}
	links := map[string]archivedForecastRow{}
	eventRevisions := map[string][]archivedForecastRow{}
	linkRevisions := map[string][]archivedForecastRow{}
	ops := map[string]storedOperation{}
	type operationKey struct {
		id      string
		version int64
	}
	opHistory := map[operationKey]operationRevision{}
	actors := map[string]bool{}
	for _, a := range data.Actors {
		actors[a] = true
	}
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			if table.Name == "investment_operation_revisions" {
				var revision operationRevision
				if json.Unmarshal(raw, &revision) != nil {
					return bad()
				}
				opHistory[operationKey{revision.Operation, revision.Version}] = revision
				continue
			}
			if table.Name == "investment_operations" {
				var o storedOperation
				if json.Unmarshal(raw, &o) != nil {
					return bad()
				}
				ops[o.ID] = o
				continue
			}
			if table.Name != "investment_payout_events" && table.Name != "investment_payout_event_revisions" && table.Name != "investment_payout_links" && table.Name != "investment_payout_link_revisions" {
				continue
			}
			var row archivedForecastRow
			if json.Unmarshal(raw, &row) != nil || row.Tenant != data.TenantID || row.Version < 1 {
				return bad()
			}
			switch table.Name {
			case "investment_payout_events":
				if _, ok := events[row.ID]; ok {
					return bad()
				}
				events[row.ID] = row
			case "investment_payout_links":
				if _, ok := links[row.Operation]; ok {
					return bad()
				}
				links[row.Operation] = row
			case "investment_payout_event_revisions":
				eventRevisions[row.Event] = append(eventRevisions[row.Event], row)
			case "investment_payout_link_revisions":
				linkRevisions[row.Operation] = append(linkRevisions[row.Operation], row)
			}
		}
	}
	if len(events) != len(eventRevisions) || len(links) != len(linkRevisions) {
		return bad()
	}
	hash := func(v any) string {
		raw, _ := json.Marshal(v)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	for key, head := range events {
		chain := eventRevisions[key]
		if int64(len(chain)) != head.Version {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		for i, row := range chain {
			var event inv.ForecastEvent
			if json.Unmarshal(row.Data, &event) != nil || row.Version != int64(i+1) || !actors[row.Actor] || row.Recorded.IsZero() || row.Reason != event.Reason || row.Event != key || event.ID != key || event.Version != row.Version {
				return bad()
			}
			normalized, e := inv.NormalizeForecastEvent(event)
			if e != nil || !reflect.DeepEqual(event, normalized) {
				return bad()
			}
			if i == len(chain)-1 {
				var current inv.ForecastEvent
				if json.Unmarshal(head.Data, &current) != nil || !reflect.DeepEqual(event, current) || head.Hash != hash(struct {
					Event inv.ForecastEvent
					Actor string
				}{event, row.Actor}) {
					return bad()
				}
			}
		}
	}
	for key, head := range links {
		chain := linkRevisions[key]
		if int64(len(chain)) != head.Version {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		for i, row := range chain {
			var link inv.ForecastLink
			if json.Unmarshal(row.Data, &link) != nil || row.Version != int64(i+1) || !actors[row.Actor] || row.Recorded.IsZero() || row.Reason != link.Reason || row.Operation != key || link.OperationID != key || link.Version != row.Version {
				return bad()
			}
			if _, e := inv.NormalizeForecastLink(link); e != nil {
				return bad()
			}
			event, ok := events[link.EventID]
			if !ok || link.EventVersion > event.Version {
				return bad()
			}
			operation, ok := ops[link.OperationID]
			if !ok || operation.Ledger != link.LedgerID || operation.Version < link.OperationVersion {
				return bad()
			}
			if !link.Cancelled {
				var definition inv.ForecastEvent
				revision := eventRevisions[link.EventID][link.EventVersion-1]
				operationRevision, ok := opHistory[operationKey{link.OperationID, link.OperationVersion}]
				if json.Unmarshal(revision.Data, &definition) != nil || !ok || definition.Cancelled || operationRevision.After.Status != "posted" || operationRevision.After.Kind != definition.Kind || operationRevision.After.InstrumentKey != definition.InstrumentKey || operationRevision.After.Currency != definition.Currency || operationRevision.After.LedgerID != link.LedgerID {
					return bad()
				}
			}
			if i == len(chain)-1 {
				var current inv.ForecastLink
				if json.Unmarshal(head.Data, &current) != nil || !reflect.DeepEqual(link, current) || head.Ledger != link.LedgerID || head.Event != link.EventID || head.Hash != hash(struct {
					Link  inv.ForecastLink
					Actor string
				}{link, row.Actor}) {
					return bad()
				}
			}
		}
	}
	return nil
}
