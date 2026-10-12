package investmentarchive

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	d "github.com/positron48/budget/internal/domain"
)

type operationRevision struct {
	Tenant    string                 `json:"tenant_id"`
	Ledger    string                 `json:"ledger_id"`
	Operation string                 `json:"operation_id"`
	Version   int64                  `json:"version"`
	Action    string                 `json:"action"`
	Actor     string                 `json:"actor_id"`
	Before    *d.InvestmentOperation `json:"before_data"`
	After     d.InvestmentOperation  `json:"after_data"`
}
type storedTransfer struct {
	Tenant        string               `json:"tenant_id"`
	ID            string               `json:"id"`
	FromLedger    string               `json:"from_ledger_id"`
	ToLedger      string               `json:"to_ledger_id"`
	FromOperation string               `json:"from_operation_id"`
	ToOperation   string               `json:"to_operation_id"`
	Data          d.InvestmentTransfer `json:"data"`
}

// VerifyJournalAudit checks the complete operation history, ledger counters and
// both transfer sides. Configuration and import audit chains are separate.
func VerifyJournalAudit(data Dataset) error {
	bad := func() error { return fmt.Errorf("archived journal audit is inconsistent") }
	heads := map[string]d.InvestmentOperation{}
	chains := map[string][]operationRevision{}
	ledgers := map[string]struct{ Version, Next int64 }{}
	transfers := map[string]storedTransfer{}
	actors := map[string]bool{}
	for _, actor := range data.Actors {
		actors[actor] = true
	}
	for _, table := range data.Tables {
		for _, raw := range table.Rows {
			switch table.Name {
			case "investment_operations":
				var row storedOperation
				if json.Unmarshal(raw, &row) != nil || heads[row.ID].ID != "" {
					return bad()
				}
				heads[row.ID] = row.Data
			case "investment_operation_revisions":
				var row operationRevision
				if json.Unmarshal(raw, &row) != nil {
					return bad()
				}
				chains[row.Operation] = append(chains[row.Operation], row)
			case "investment_ledgers":
				var row struct {
					ID      string `json:"id"`
					Version int64  `json:"version"`
					Next    int64  `json:"next_sequence"`
				}
				if json.Unmarshal(raw, &row) != nil {
					return bad()
				}
				ledgers[row.ID] = struct{ Version, Next int64 }{row.Version, row.Next}
			case "investment_transfers":
				var row storedTransfer
				if json.Unmarshal(raw, &row) != nil || transfers[row.ID].ID != "" {
					return bad()
				}
				transfers[row.ID] = row
			}
		}
	}
	counts, revisions := map[string]int64{}, map[string]int64{}
	sequences := map[string]map[int64]bool{}
	for key, head := range heads {
		chain := chains[key]
		if head.Version < 1 || int64(len(chain)) != head.Version || head.Sequence < 1 {
			return bad()
		}
		sort.Slice(chain, func(i, j int) bool { return chain[i].Version < chain[j].Version })
		for i, row := range chain {
			after := row.After
			if row.Version != int64(i+1) || row.Tenant != data.TenantID || row.Ledger != head.LedgerID || row.Operation != head.ID ||
				after.ID != head.ID || after.TenantID != data.TenantID || after.LedgerID != head.LedgerID ||
				after.Sequence != head.Sequence || after.Version != row.Version || after.ActorID != row.Actor || !actors[row.Actor] ||
				!after.CreatedAt.Equal(head.CreatedAt) || !reflect.DeepEqual(after.Source, head.Source) {
				return bad()
			}
			if i == 0 {
				if row.Action != "create" || row.Before != nil || after.Status != "posted" {
					return bad()
				}
			} else {
				previous := chain[i-1].After
				if row.Before == nil || !reflect.DeepEqual(*row.Before, previous) || previous.Status != "posted" {
					return bad()
				}
				switch row.Action {
				case "amend":
					if after.Status != "posted" {
						return bad()
					}
				case "void":
					expected := previous
					expected.Status, expected.Version, expected.ActorID = "voided", row.Version, row.Actor
					if !reflect.DeepEqual(expected, after) {
						return bad()
					}
				default:
					return bad()
				}
			}
		}
		if !reflect.DeepEqual(chain[len(chain)-1].After, head) {
			return bad()
		}
		if sequences[head.LedgerID] == nil {
			sequences[head.LedgerID] = map[int64]bool{}
		}
		if sequences[head.LedgerID][head.Sequence] {
			return bad()
		}
		sequences[head.LedgerID][head.Sequence] = true
		counts[head.LedgerID]++
		revisions[head.LedgerID] += int64(len(chain))
	}
	for key := range chains {
		if heads[key].ID == "" {
			return bad()
		}
	}
	for key, ledger := range ledgers {
		if ledger.Version != revisions[key]+1 || ledger.Next != counts[key]+1 {
			return bad()
		}
		for seq := range sequences[key] {
			if seq >= ledger.Next {
				return bad()
			}
		}
	}
	linked := map[string]bool{}
	for key, row := range transfers {
		tr := row.Data
		if !id(key) || tr.ID != key || row.Tenant != data.TenantID || tr.TenantID != data.TenantID ||
			row.FromLedger != tr.FromLedgerID || row.ToLedger != tr.ToLedgerID || tr.FromLedgerID == tr.ToLedgerID ||
			row.FromOperation != tr.FromOperationID || row.ToOperation != tr.ToOperationID || tr.FromOperationID == tr.ToOperationID {
			return bad()
		}
		outKind, inKind := "transfer_out", "transfer_in"
		if tr.InstrumentKey != "" {
			outKind, inKind = "security_transfer_out", "security_transfer_in"
		}
		for _, side := range []struct{ ID, Ledger, Other, Kind string }{
			{tr.FromOperationID, tr.FromLedgerID, tr.ToLedgerID, outKind},
			{tr.ToOperationID, tr.ToLedgerID, tr.FromLedgerID, inKind},
		} {
			op := heads[side.ID]
			if linked[side.ID] || op.ID == "" || op.LedgerID != side.Ledger || op.CounterpartyLedgerID != side.Other ||
				op.Kind != side.Kind || op.TransferID != key || op.Version != tr.Version || op.Status != tr.Status ||
				op.Currency != tr.Currency || op.Amount != tr.Amount || op.InstrumentKey != tr.InstrumentKey || tr.InstrumentKey != "" && op.Quantity != tr.Quantity || op.Note != tr.Note || !reflect.DeepEqual(op.Timing, tr.Timing) || !op.OccurredAt.Equal(tr.OccurredAt) {
				return bad()
			}
			linked[side.ID] = true
		}
	}
	for _, op := range heads {
		if (op.Kind == "transfer_in" || op.Kind == "transfer_out" || op.TransferID != "" || op.CounterpartyLedgerID != "") && !linked[op.ID] {
			return bad()
		}
	}
	return nil
}
