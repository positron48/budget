package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"testing"
)

func TestBrokerTradeLinkAtomicClaims(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	tenant, actor, ledger := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Link','RUB')`, []any{tenant}},
		{`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, []any{actor, actor + "@example.test"}},
		{`INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, []any{tenant, actor}},
		{`INSERT INTO investment_ledgers(id,tenant_id,name,started_at,created_by) VALUES($1,$2,'Link','2024-01-01',$3)`, []any{ledger, tenant, actor}},
	} {
		if _, err := pool.DB.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	raw := brokerLinkedTradeFixture(t)
	repo := NewInvestmentImportRepo(pool)
	snapshotID, err := repo.SaveBrokerSnapshot(ctx, tenant, actor, raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := imp.PrepareBrokerSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := imp.PrepareBrokerTrade(snapshot, "trade", ledger)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		op := candidate.Operation
		op.ID = id
		op.TenantID = tenant
		op.Version = 1
		op.Status = "posted"
		op.Sequence = int64(i + 1)
		data, _ := json.Marshal(op)
		if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_operations(id,tenant_id,ledger_id,kind,occurred_at,settlement_at,sequence,status,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, tenant, ledger, op.Kind, op.OccurredAt, op.SettlementAt, op.Sequence, op.Status, data); err != nil {
			t.Fatal(err)
		}
	}
	link := imp.BrokerTradeLink{SnapshotID: snapshotID, BrokerOperationID: "trade", LedgerID: ledger, OperationID: ids[0], ExpectedOperationVersion: 1, Reason: "Exact broker match reviewed"}
	stale := link
	stale.ExpectedOperationVersion = 2
	if _, err = repo.LinkBrokerTrade(ctx, tenant, actor, stale); !errors.Is(err, imp.ErrConflict) {
		t.Fatal(err)
	}
	var partial int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_broker_links`).Scan(&partial); err != nil || partial != 0 {
		t.Fatal(partial, err)
	}

	journal := inv.NewService(NewInvestmentRepo(pool), NewTenantRepo(pool))
	query := inv.OperationsQuery{FromDate: "2024-01-01", ToDate: "2025-12-31", Limit: 1}
	initialPage, err := journal.QueryOperations(ctx, tenant, actor, query)
	if err != nil || initialPage.TotalCount != 2 || initialPage.NextCursor == "" {
		t.Fatal("initial journal", err, initialPage)
	}
	id, err := repo.LinkBrokerTrade(ctx, tenant, actor, link)
	if err != nil {
		t.Fatal(err)
	}

	query.Cursor = initialPage.NextCursor
	if _, err = journal.QueryOperations(ctx, tenant, actor, query); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("source-link cursor not fenced", err)
	}
	query.Cursor = ""
	query.Origin = "broker"
	linkedPage, err := journal.QueryOperations(ctx, tenant, actor, query)
	if err != nil || linkedPage.TotalCount != 1 || linkedPage.Entries[0].Operation.ID != ids[0] {
		t.Fatal("broker journal origin", err, linkedPage)
	}
	retry, err := repo.LinkBrokerTrade(ctx, tenant, actor, link)
	if err != nil || retry != id {
		t.Fatal(retry, err)
	}
	rows, err := repo.BrokerSnapshotRows(ctx, tenant, snapshotID, 20, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.LinkID != id || row.LinkedOperationID != ids[0] || row.LinkedVersion != 1 || row.CurrentVersion != 1 || row.LinkedStatus != "posted" || row.ClaimedDifferentContent {
			t.Fatal(row)
		}
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE investment_operations SET version=2,status='voided',data=jsonb_set(jsonb_set(data,'{Version}','2'::jsonb),'{Status}','"voided"'::jsonb) WHERE tenant_id=$1 AND id=$2`, tenant, ids[0]); err != nil {
		t.Fatal(err)
	}

	stalePage, err := journal.QueryOperations(ctx, tenant, actor, query)
	if err != nil || stalePage.TotalCount != 1 || stalePage.Entries[0].BrokerEvidenceVersion != 1 || stalePage.Entries[0].Operation.Version != 2 || len(stalePage.Entries[0].Cash) != 0 {
		t.Fatal("historical broker evidence falsely current", err, stalePage)
	}
	rows, err = repo.BrokerSnapshotRows(ctx, tenant, snapshotID, 20, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.LinkID != id || row.LinkedVersion != 1 || row.CurrentVersion != 2 || row.LinkedStatus != "voided" {
			t.Fatal(row)
		}
	}
	link.OperationID = ids[1]
	if _, err = repo.LinkBrokerTrade(ctx, tenant, actor, link); !errors.Is(err, imp.ErrConflict) {
		t.Fatal(err)
	}
	var links, claims, operations int
	if err = pool.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM investment_broker_links),(SELECT count(*) FROM investment_broker_claims),(SELECT count(*) FROM investment_operations)`).Scan(&links, &claims, &operations); err != nil {
		t.Fatal(err)
	}
	if links != 1 || claims != 2 || operations != 2 {
		t.Fatal(links, claims, operations)
	}
	if _, err = pool.DB.Exec(ctx, `UPDATE user_tenants SET role='member' WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.LinkBrokerTrade(ctx, tenant, actor, link); !errors.Is(err, imp.ErrForbidden) {
		t.Fatal(err)
	}
}

func brokerLinkedTradeFixture(t *testing.T) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(brokerSnapshotFixture(t, "2", false), &document); err != nil {
		t.Fatal(err)
	}
	money := func(units string) map[string]any { return map[string]any{"currency": "rub", "units": units, "nano": 0} }
	ops := []map[string]any{
		{"id": "trade", "brokerAccountId": "account", "state": "OPERATION_STATE_EXECUTED", "type": "OPERATION_TYPE_BUY", "instrumentType": "share", "instrumentUid": uuid.NewString(), "date": "2024-02-01T00:00:00Z", "quantityDone": "2", "payment": money("-20"), "commission": money("-1"), "tradesInfo": map[string]any{"trades": []map[string]any{{"num": "fill", "quantity": "2", "date": "2024-02-01T00:00:01Z"}}}},
		{"id": "fee", "brokerAccountId": "account", "parentOperationId": "trade", "state": "OPERATION_STATE_EXECUTED", "type": "OPERATION_TYPE_BROKER_FEE", "date": "2024-02-01T00:00:02Z", "payment": money("-1")},
	}
	document["operations"] = ops
	bytes, _ := json.Marshal(ops)
	hash := sha256.Sum256(bytes)
	document["history_evidence"] = map[string]any{"version": 1, "complete": true, "pages": []map[string]any{{"count": 2, "items_sha256": hex.EncodeToString(hash[:]), "has_next": false, "cursor": "", "next_cursor": ""}}}
	raw, _ := json.Marshal(document)
	return raw
}
