package main

import (
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

type candidateReplay struct {
	SourceSHA256        string
	FinancialReviewHash string
	Candidates          int
	Unresolved          int
	Ledgers             []candidateLedgerReplay
	Limitations         []string
}
type candidateCashFailure struct {
	At      time.Time
	Balance d.InvestmentCash
	Records []int
}
type candidateLedgerReplay struct {
	CashFailure       *candidateCashFailure `json:",omitempty"`
	FailedRecord      int                   `json:",omitempty"`
	FailedInstrument  string                `json:",omitempty"`
	LedgerID          string
	Records           []int
	UnresolvedRecords []int
	State             string
	Error             string `json:",omitempty"`
	StartedAt         time.Time
	AsOf              time.Time
	Snapshot          *d.InvestmentSnapshot `json:",omitempty"`
}

// Replay only the explicitly prepared candidates. Success cannot prove source
// completeness, broker reconciliation or correctness of omitted operations.
func replayCandidates(report imp.FinancialMappingReport) candidateReplay {
	out := candidateReplay{SourceSHA256: report.SourceSHA256, FinancialReviewHash: report.Hash, Candidates: report.Ready, Unresolved: report.Unresolved, Ledgers: []candidateLedgerReplay{}, Limitations: []string{
		"Only prepared candidates are replayed; unresolved source rows are not silently treated as zero.",
		"Ledger opening is assumed at the first candidate; no opening balances are invented.",
		"Equal-time candidate ordering uses source record order; broker execution ordering remains unverified.",
		"No database writes, broker comparison or proof of complete history; passing replay alone does not authorize cutover.",
	}}
	groups := map[string][]d.InvestmentOperation{}
	records := map[string][]int{}
	unresolved := map[string][]int{}
	for _, row := range report.Rows {
		ledger := report.Mapping.LedgerByRecord[row.Record]
		if row.Operation == nil || len(row.Issues) > 0 {
			unresolved[ledger] = append(unresolved[ledger], row.Record)
			continue
		}
		op := *row.Operation
		op.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(report.SourceSHA256+":"+strconv.Itoa(row.Record))).String()
		op.Status = "posted"
		op.Sequence = int64(row.Record)
		groups[op.LedgerID] = append(groups[op.LedgerID], op)
		records[op.LedgerID] = append(records[op.LedgerID], row.Record)
	}
	ids := map[string]bool{}
	for id := range groups {
		ids[id] = true
	}
	for id := range unresolved {
		ids[id] = true
	}
	sorted := []string{}
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		item := candidateLedgerReplay{LedgerID: id, Records: records[id], UnresolvedRecords: unresolved[id], State: "no_candidates"}
		ops := groups[id]
		if len(ops) > 0 {
			item.StartedAt = ops[0].OccurredAt
			item.AsOf = ops[0].OccurredAt
			for _, op := range ops {
				if op.OccurredAt.Before(item.StartedAt) {
					item.StartedAt = op.OccurredAt
				}
				if op.OccurredAt.After(item.AsOf) {
					item.AsOf = op.OccurredAt
				}
				if op.SettlementAt.After(item.AsOf) {
					item.AsOf = op.SettlementAt
				}
			}
			result, err := inv.Replay(d.InvestmentLedger{ID: id, StartedAt: item.StartedAt}, ops, item.AsOf)
			if err != nil {
				item.State = "replay_failed"
				item.Error = err.Error()
				var cashError *inv.ReplayCashError
				if errors.As(err, &cashError) {
					detail := &candidateCashFailure{At: cashError.At, Balance: cashError.Balance, Records: []int{}}
					for _, op := range ops {
						for _, id := range cashError.OperationIDs {
							if id == op.ID {
								detail.Records = append(detail.Records, int(op.Sequence))
							}
						}
					}
					sort.Ints(detail.Records)
					item.CashFailure = detail
				}
				var operationError *inv.ReplayOperationError
				if errors.As(err, &operationError) {
					item.FailedInstrument = operationError.InstrumentKey
					for _, op := range ops {
						if op.ID == operationError.OperationID {
							item.FailedRecord = int(op.Sequence)
							break
						}
					}
				}
			} else {
				item.State = "candidate_replay_passed"
				item.Snapshot = &result.Snapshot
			}
		}
		out.Ledgers = append(out.Ledgers, item)
	}
	return out
}
