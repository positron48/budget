package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func verifyBrokerArchiveSources(ctx context.Context, tx pgx.Tx, tenant string) error {
	rows, err := tx.Query(ctx, `SELECT id,account_id,source_sha256,row_count,original_json FROM investment_broker_snapshots WHERE tenant_id=$1`, tenant)
	if err != nil {
		return err
	}
	type stored struct {
		id, account, hash string
		count             int
		raw               []byte
	}
	snapshots := []stored{}
	for rows.Next() {
		var s stored
		if err = rows.Scan(&s.id, &s.account, &s.hash, &s.count, &s.raw); err != nil {
			rows.Close()
			return err
		}
		snapshots = append(snapshots, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, s := range snapshots {
		prepared, err := imp.PrepareBrokerSnapshot(s.raw)
		if err != nil || prepared.AccountID != s.account || prepared.SHA256 != s.hash || len(prepared.Rows) != s.count {
			return d.ErrInvestmentPrecondition
		}
		rows, err := tx.Query(ctx, `SELECT l.ordinal,o.account_id,o.operation_id,o.content_sha256,o.raw_json FROM investment_broker_snapshot_rows l JOIN investment_broker_observations o ON o.tenant_id=l.tenant_id AND o.id=l.observation_id WHERE l.tenant_id=$1 AND l.snapshot_id=$2 ORDER BY l.ordinal`, tenant, s.id)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var ordinal int
			var account, id, hash string
			var raw []byte
			if err = rows.Scan(&ordinal, &account, &id, &hash, &raw); err != nil {
				rows.Close()
				return err
			}
			if count >= len(prepared.Rows) {
				rows.Close()
				return d.ErrInvestmentPrecondition
			}
			expected := prepared.Rows[count]
			canonical, canonicalErr := json.Marshal(json.RawMessage(raw))
			expectedCanonical, _ := json.Marshal(expected.Raw)
			if canonicalErr != nil || ordinal != count+1 || account != expected.AccountID || id != expected.OperationID || hash != expected.SHA256 || !bytes.Equal(canonical, expectedCanonical) {
				rows.Close()
				return d.ErrInvestmentPrecondition
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count != len(prepared.Rows) {
			return d.ErrInvestmentPrecondition
		}
	}
	var orphan bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM investment_broker_observations o WHERE o.tenant_id=$1 AND NOT EXISTS(SELECT 1 FROM investment_broker_snapshot_rows l WHERE l.tenant_id=o.tenant_id AND l.observation_id=o.id))`, tenant).Scan(&orphan); err != nil {
		return err
	}
	if orphan {
		return d.ErrInvestmentPrecondition
	}
	return nil
}
