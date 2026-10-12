package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

// Validate provenance against the journal revision originally reviewed, not the
// current operation: later amendments and voids must remain restorable.
func verifyBrokerArchiveLinks(ctx context.Context, tx pgx.Tx, tenant string) error {
	rows, err := tx.Query(ctx, `SELECT b.id,b.ledger_id,s.original_json,r.after_data FROM investment_broker_links b JOIN investment_broker_snapshots s ON s.tenant_id=b.tenant_id AND s.id=b.snapshot_id LEFT JOIN investment_operation_revisions r ON r.tenant_id=b.tenant_id AND r.ledger_id=b.ledger_id AND r.operation_id=b.operation_id AND r.version=b.operation_version WHERE b.tenant_id=$1`, tenant)
	if err != nil {
		return err
	}
	type item struct {
		id, ledger       string
		source, revision []byte
	}
	items := []item{}
	for rows.Next() {
		var value item
		if err = rows.Scan(&value.id, &value.ledger, &value.source, &value.revision); err != nil {
			rows.Close()
			return err
		}
		items = append(items, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, value := range items {
		var operation d.InvestmentOperation
		if len(value.revision) == 0 || json.Unmarshal(value.revision, &operation) != nil {
			return d.ErrInvestmentPrecondition
		}
		snapshot, err := imp.PrepareBrokerSnapshot(value.source)
		if err != nil {
			return d.ErrInvestmentPrecondition
		}
		claims, err := tx.Query(ctx, `SELECT account_id,operation_id,content_sha256 FROM investment_broker_claims WHERE tenant_id=$1 AND link_id=$2`, tenant, value.id)
		if err != nil {
			return err
		}
		hashes := map[string]string{}
		for claims.Next() {
			var account, id, hash string
			if err = claims.Scan(&account, &id, &hash); err != nil {
				claims.Close()
				return err
			}
			hashes["tinvest:"+account+":"+id] = hash
		}
		err = claims.Err()
		claims.Close()
		if err != nil {
			return err
		}
		matched := 0
		for _, source := range snapshot.Rows {
			if _, ok := hashes[source.Identity]; !ok {
				continue
			}
			candidate, err := imp.PrepareBrokerTrade(snapshot, source.OperationID, value.ledger)
			if err != nil {
				continue
			}
			matches := imp.PossibleBrokerTradeMatches(candidate.Operation, []d.InvestmentOperation{operation})
			if len(matches) != 1 || !matches[0].SameLedger || !matches[0].SameInstrument || !matches[0].ExactTime || len(candidate.SourceIdentities) != len(hashes) {
				continue
			}
			complete := true
			for i, id := range candidate.SourceIdentities {
				if hashes[id] != candidate.SourceHashes[i] {
					complete = false
				}
			}
			if complete {
				matched++
			}
		}
		if matched != 1 {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}
