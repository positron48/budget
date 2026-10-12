package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"testing"
	"time"
)

func TestBrokerJournalCandidatesScope(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, other, actor := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{tenant, other} {
		if _, err := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Review','RUB')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	ledgers := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, ledger := range ledgers {
		owner := tenant
		if i == 2 {
			owner = other
		}
		if _, err := pool.DB.Exec(ctx, `INSERT INTO investment_ledgers(id,tenant_id,name,started_at,created_by) VALUES($1,$2,'Review',$3,$4)`, ledger, owner, at.Add(-48*time.Hour), actor); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 3; j++ {
			op := d.InvestmentOperation{ID: uuid.NewString(), LedgerID: ledger, TenantID: owner, Kind: "buy", Status: "posted", OccurredAt: at, SettlementAt: at, Sequence: int64(j + 1)}
			if j == 1 {
				op.Status = "voided"
			}
			if j == 2 {
				op.OccurredAt = at.Add(25 * time.Hour)
				op.SettlementAt = op.OccurredAt
			}
			raw, err := json.Marshal(op)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_operations(id,tenant_id,ledger_id,kind,occurred_at,settlement_at,sequence,status,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, op.ID, owner, ledger, op.Kind, op.OccurredAt, op.SettlementAt, op.Sequence, op.Status, raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := NewInvestmentImportRepo(pool)
	operations, err := repo.BrokerTradeJournalCandidates(ctx, tenant, ledgers[0], at)
	if err != nil || len(operations) != 2 {
		t.Fatal(operations, err)
	}
	seen := map[string]bool{}
	for _, op := range operations {
		if op.TenantID != tenant || op.Status != "posted" || !op.OccurredAt.Equal(at) {
			t.Fatal(op)
		}
		seen[op.LedgerID] = true
	}
	if !seen[ledgers[0]] || !seen[ledgers[1]] {
		t.Fatal(seen)
	}
	if _, err = repo.BrokerTradeJournalCandidates(ctx, tenant, ledgers[2], at); !errors.Is(err, imp.ErrNotFound) {
		t.Fatal(err)
	}
}
