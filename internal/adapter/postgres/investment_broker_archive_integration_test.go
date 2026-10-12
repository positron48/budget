package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	"testing"
	"time"
)

func TestBrokerLinkedArchiveRoundTrip(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	target, cleanupTarget := withPg(t)
	defer cleanupTarget()
	ctx := context.Background()
	tenant, actor := uuid.NewString(), uuid.NewString()
	for _, db := range []interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	}{pool.DB, target.DB} {
		if _, err := db.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Archive','RUB')`, tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, actor, actor+"@example.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, err := svc.CreateLedger(ctx, tenant, actor, "Broker", at, "ledger")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "100", OccurredAt: at}}); err != nil {
		t.Fatal(err)
	}
	raw := brokerLinkedTradeFixture(t)
	broker := NewInvestmentImportRepo(pool)
	snapshotID, err := broker.SaveBrokerSnapshot(ctx, tenant, actor, raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := imp.PrepareBrokerSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := imp.PrepareBrokerTrade(snapshot, "trade", ledger.ID)
	if err != nil {
		t.Fatal(err)
	}
	check, err := broker.BrokerTradeReplay(ctx, tenant, candidate.Operation)
	if err != nil || !check.Valid || check.LedgerVersion != 2 {
		t.Fatal(check, err)
	}
	sale := candidate.Operation
	sale.Kind = "sell"
	check, err = broker.BrokerTradeReplay(ctx, tenant, sale)
	if err != nil || check.Valid || check.Issue == "" {
		t.Fatal("oversell accepted", check, err)
	}
	var count int
	if err = pool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_operations WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("preview wrote operations", count, err)
	}
	post := imp.BrokerTradePost{SnapshotID: snapshotID, BrokerOperationID: "trade", LedgerID: ledger.ID, RequestKey: "broker-buy", ExpectedLedgerVersion: 1, ReconciliationConfirmed: true, Reason: "Checked broker cash"}
	if _, err = broker.PostBrokerTrade(ctx, tenant, actor, post); !errors.Is(err, d.ErrInvestmentConflict) {
		t.Fatal("stale ledger accepted", err)
	}
	post.ExpectedLedgerVersion = 2
	conflictID := uuid.NewString()
	if _, err = pool.DB.Exec(ctx, `INSERT INTO investment_broker_observations(id,tenant_id,account_id,operation_id,content_sha256,raw_json) SELECT $1,tenant_id,account_id,operation_id,repeat('0',64),raw_json FROM investment_broker_observations WHERE tenant_id=$2 AND operation_id='fee'`, conflictID, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = broker.PostBrokerTrade(ctx, tenant, actor, post); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("changed fee accepted", err)
	}
	var version, next, opCount, claimCount int
	if err = pool.DB.QueryRow(ctx, `SELECT version,next_sequence,(SELECT count(*) FROM investment_operations WHERE tenant_id=$1),(SELECT count(*) FROM investment_broker_claims WHERE tenant_id=$1) FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, ledger.ID).Scan(&version, &next, &opCount, &claimCount); err != nil || version != 2 || next != 2 || opCount != 1 || claimCount != 0 {
		t.Fatal("partial post", version, next, opCount, claimCount, err)
	}
	if _, err = pool.DB.Exec(ctx, `DELETE FROM investment_broker_observations WHERE id=$1`, conflictID); err != nil {
		t.Fatal(err)
	}
	result, err := broker.PostBrokerTrade(ctx, tenant, actor, post)
	if err != nil {
		t.Fatal(err)
	}
	retryPost, err := broker.PostBrokerTrade(ctx, tenant, actor, post)
	if err != nil || retryPost != result {
		t.Fatal(retryPost, err)
	}
	changed := post
	changed.Reason = "different"
	if _, err = broker.PostBrokerTrade(ctx, tenant, actor, changed); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("changed request accepted", err)
	}
	changed = post
	changed.RequestKey = "another"
	changed.ExpectedLedgerVersion = 3
	if _, err = broker.PostBrokerTrade(ctx, tenant, actor, changed); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("duplicate posted", err)
	}
	operation, err := scanInvestmentOperation(pool.DB.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, tenant, ledger.ID, result.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	link := imp.BrokerTradeLink{SnapshotID: snapshotID, BrokerOperationID: "trade", LedgerID: ledger.ID, OperationID: operation.ID, ExpectedOperationVersion: operation.Version, Reason: "Checked broker cash"}
	linkID, err := broker.LinkBrokerTrade(ctx, tenant, actor, link)
	if err != nil {
		t.Fatal(err)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0044_investment_broker_links")
	groupID := uuid.NewString()
	classification, err := NewMarketDataRepo(pool).SaveInstrumentGroups(ctx, tenant, actor, nav.SaveInstrumentGroups{InstrumentGroups: nav.InstrumentGroups{Groups: []nav.InstrumentGroup{{ID: groupID, Name: "Strategy"}}, Memberships: []nav.GroupMembership{{LedgerID: ledger.ID, InstrumentKey: candidate.Operation.InstrumentKey, GroupID: groupID}}}, RequestKey: "groups", Reason: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if err != nil {
		t.Fatal(err)
	}
	if archiveRows(bundle, "investment_broker_links") != 1 || archiveRows(bundle, "investment_broker_claims") != 2 {
		t.Fatal("missing provenance")
	}
	targetRepo := NewInvestmentRepo(target)
	encoded, err := json.Marshal(bundle.Data)
	if err != nil {
		t.Fatal(err)
	}
	var damaged archive.Dataset
	if err = json.Unmarshal(encoded, &damaged); err != nil {
		t.Fatal(err)
	}
	for i := range damaged.Tables {
		if damaged.Tables[i].Name == "investment_broker_claims" {
			damaged.Tables[i].Rows = damaged.Tables[i].Rows[:1]
		}
	}
	corrupted, err := archive.Seal(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = targetRepo.CheckInvestmentArchiveRestore(ctx, actor, corrupted); !errors.Is(err, d.ErrInvestmentPrecondition) {
		t.Fatal("incomplete claims accepted", err)
	}

	for _, corruption := range []string{"missing_source_row", "source_hash", "observation_bytes", "source_order"} {
		var changed archive.Dataset
		if err = json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		for i := range changed.Tables {
			table := &changed.Tables[i]
			if len(table.Rows) == 0 {
				continue
			}
			if corruption == "missing_source_row" && table.Name == "investment_broker_snapshot_rows" {
				table.Rows = table.Rows[:1]
				continue
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(table.Rows[0], &fields); err != nil {
				t.Fatal(err)
			}
			switch {
			case corruption == "source_hash" && table.Name == "investment_broker_snapshots":
				fields["source_sha256"], _ = json.Marshal("0000000000000000000000000000000000000000000000000000000000000000")
			case corruption == "observation_bytes" && table.Name == "investment_broker_observations":
				fields["raw_json"], _ = json.Marshal(`\x7b7d`)
			case corruption == "source_order" && table.Name == "investment_broker_snapshot_rows":
				fields["ordinal"] = json.RawMessage("99")
			default:
				continue
			}
			table.Rows[0], err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
		}
		bad, err := archive.Seal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = targetRepo.CheckInvestmentArchiveRestore(ctx, actor, bad); !errors.Is(err, d.ErrInvestmentPrecondition) {
			t.Fatal(corruption, err)
		}
	}
	if _, err = targetRepo.CheckInvestmentArchiveRestore(ctx, actor, bundle); err != nil {
		t.Fatal(err)
	}
	if _, err = targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); err != nil {
		t.Fatal(err)
	}
	restoredGroups, _, err := NewMarketDataRepo(target).GetInstrumentGroups(ctx, tenant, actor)
	if err != nil || investmentHash(restoredGroups) != investmentHash(classification) {
		t.Fatal("groups changed after restore", restoredGroups, err)
	}
	restored := NewInvestmentImportRepo(target)
	source, err := restored.BrokerSnapshotBytes(ctx, tenant, snapshotID)
	if err != nil || !bytes.Equal(source, raw) {
		t.Fatal("source changed", err)
	}
	rows, err := restored.BrokerSnapshotRows(ctx, tenant, snapshotID, 20, 0)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.LinkID != linkID || row.LinkedOperationID != operation.ID || row.LinkedVersion != operation.Version || row.CurrentVersion != operation.Version {
			t.Fatal(row)
		}
	}
	retry, err := restored.LinkBrokerTrade(ctx, tenant, actor, link)
	if err != nil || retry != linkID {
		t.Fatal(retry, err)
	}
	targetService := inv.NewService(targetRepo, NewTenantRepo(target))
	duplicate, err := targetService.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "other-buy", ExpectedLedgerVersion: 3, Operation: candidate.Operation})
	if err != nil {
		t.Fatal(err)
	}
	link.OperationID = duplicate.ID
	if _, err = restored.LinkBrokerTrade(ctx, tenant, actor, link); !errors.Is(err, imp.ErrConflict) {
		t.Fatal("restored claim reused", err)
	}
}
