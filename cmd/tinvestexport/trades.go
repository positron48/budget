package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

type tradeReviewRow struct {
	SourceIdentity string
	State          string
	Issue          string                    `json:",omitempty"`
	Candidate      *imp.BrokerTradeCandidate `json:",omitempty"`
}
type tradeReview struct {
	SnapshotSHA256 string
	After          time.Time
	PostingAllowed bool
	Rows           []tradeReviewRow
}

func reviewTrades(snapshot imp.BrokerSnapshot, after time.Time, ledger string) (tradeReview, error) {
	result := tradeReview{SnapshotSHA256: snapshot.SHA256, After: after, Rows: []tradeReviewRow{}}
	if after.IsZero() {
		return result, fmt.Errorf("explicit cutoff required")
	}
	if id, err := uuid.Parse(ledger); err != nil || id.String() != ledger {
		return result, fmt.Errorf("canonical ledger UUID required")
	}
	covered := map[string]bool{}
	for _, row := range snapshot.Rows {
		var event struct {
			Date        time.Time
			Type, State string
		}
		if err := json.Unmarshal(row.Raw, &event); err != nil {
			return result, err
		}
		if !event.Date.After(after) {
			continue
		}
		item := tradeReviewRow{SourceIdentity: row.Identity, State: "requires_review"}
		switch event.Type {
		case "OPERATION_TYPE_BUY", "OPERATION_TYPE_BUY_CARD", "OPERATION_TYPE_SELL", "OPERATION_TYPE_SELL_CARD":
			candidate, err := imp.PrepareBrokerTrade(snapshot, row.OperationID, ledger)
			if err != nil {
				item.Issue = err.Error()
			} else {
				item.State = "candidate"
				item.Candidate = &candidate
				for _, identity := range candidate.SourceIdentities {
					covered[identity] = true
				}
			}
		}
		result.Rows = append(result.Rows, item)
	}
	for i := range result.Rows {
		row := &result.Rows[i]
		if row.State != "candidate" && covered[row.SourceIdentity] {
			row.State = "included_component"
		}
	}
	return result, nil
}
