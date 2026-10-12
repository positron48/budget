package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

func archiveRows(b archive.Bundle, name string) int {
	for _, table := range b.Data.Tables {
		if table.Name == name {
			return len(table.Rows)
		}
	}
	return -1
}
func TestInvestmentArchiveSnapshotAndManifest(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, actor string
	if e := pool.DB.QueryRow(ctx, `INSERT INTO tenants(name,default_currency_code) VALUES('Archive','RUB') RETURNING id`).Scan(&tenant); e != nil {
		t.Fatal(e)
	}
	if e := pool.DB.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES('archive@example.test','test') RETURNING id`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedger(ctx, tenant, actor, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	mutation := inv.Mutation{Action: "create", RequestKey: "deposit", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "deposit", Currency: "RUB", Amount: "9007199254740993.123456789012", OccurredAt: start.Add(time.Hour)}}
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	// Household idempotency records are outside this investment export.
	if _, e = pool.DB.Exec(ctx, `INSERT INTO financial_write_requests(tenant_id,operation,request_key,payload_hash,result) VALUES($1,'asset.create','other','hash','{}')`, tenant); e != nil {
		t.Fatal(e)
	}
	brokerRaw := brokerSnapshotFixture(t, "9007199254740993", true)
	brokerID, e := NewInvestmentImportRepo(pool).SaveBrokerSnapshot(ctx, tenant, actor, brokerRaw)
	if e != nil {
		t.Fatal(e)
	}
	var formatted bytes.Buffer
	if e = json.Indent(&formatted, brokerRaw, "", "  "); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentImportRepo(pool).SaveBrokerSnapshot(ctx, tenant, actor, formatted.Bytes()); e != nil {
		t.Fatal(e)
	}
	first, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if archiveRows(first, "investment_operations") != 1 || archiveRows(first, "investment_operation_revisions") != 1 || archiveRows(first, "financial_write_requests") != 2 || archiveRows(first, "investment_daily_nav") != -1 || len(first.Data.Actors) != 1 || first.Data.Actors[0] != actor {
		t.Fatal("incomplete or overbroad archive")
	}
	if archiveRows(first, "investment_broker_snapshots") != 2 || archiveRows(first, "investment_broker_observations") != 2 || archiveRows(first, "investment_broker_snapshot_rows") != 4 || archiveRows(first, "investment_broker_claims") != 0 {
		t.Fatal("broker sources missing from archive")
	}
	targetPool, targetCleanup := withPg(t)
	defer targetCleanup()
	if _, e = targetPool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','RUB')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = targetPool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restore@example.test','test')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = targetPool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	targetRepo := NewInvestmentRepo(targetPool)
	for attempt := 0; attempt < 2; attempt++ {
		checked, checkErr := targetRepo.CheckInvestmentArchiveRestore(ctx, actor, first)
		if checkErr != nil || checked.Operations != 1 {
			t.Fatal("restore rehearsal", checked, checkErr)
		}
		after, checkErr := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
		if checkErr != nil {
			t.Fatal(checkErr)
		}
		for _, table := range after.Data.Tables {
			if len(table.Rows) != 0 {
				t.Fatal("dry-run persisted rows", table.Name)
			}
		}
	}
	if _, e = targetRepo.CheckInvestmentArchiveRestore(ctx, uuid.NewString(), first); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("restore authorization", e)
	}
	if _, e = repo.CheckInvestmentArchiveRestore(ctx, actor, first); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("nonempty restore accepted", e)
	}
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, first, "wrong"); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("wrong apply checksum", e)
	}
	restoredCheck, e := targetRepo.RestoreInvestmentArchive(ctx, actor, first, first.SHA256)
	if e != nil || restoredCheck.Operations != 1 {
		t.Fatal("apply archive", restoredCheck, e)
	}
	restoredBroker, e := NewInvestmentImportRepo(targetPool).BrokerSnapshotBytes(ctx, tenant, brokerID)
	if e != nil || !bytes.Equal(restoredBroker, brokerRaw) {
		t.Fatal("broker source bytes changed", e)
	}
	restoredArchive, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(restoredArchive.Data); e != nil {
		t.Fatal("restored replay", e)
	}
	if archiveRows(restoredArchive, "investment_archive_restores") != 1 {
		t.Fatal("missing restore receipt")
	}
	var jobs int
	if e = targetPool.DB.QueryRow(ctx, `SELECT count(*) FROM investment_recalculation_jobs WHERE tenant_id=$1`, tenant).Scan(&jobs); e != nil || jobs != 1 {
		t.Fatal("missing journal rebuild", jobs, e)
	}
	targetService := inv.NewService(targetRepo, NewTenantRepo(targetPool))
	followup := mutation
	followup.RequestKey = "after-restore"
	followup.ExpectedLedgerVersion = 2
	followup.Operation.Amount = "1"
	if _, e = targetService.Mutate(ctx, tenant, actor, followup); e != nil {
		t.Fatal("journal write after restore", e)
	}
	retry, e := targetRepo.RestoreInvestmentArchive(ctx, actor, first, first.SHA256)
	if e != nil || retry != restoredCheck {
		t.Fatal("restore retry", retry, e)
	}
	afterRetry, e := targetRepo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil || archiveRows(afterRetry, "investment_operations") != 2 || archiveRows(afterRetry, "investment_archive_restores") != 1 {
		t.Fatal("retry changed journal", e)
	}
	assertInvestmentRollbackRefused(t, ctx, targetPool, "0037_investment_archive_restores")
	for _, corruption := range []string{"missing_revision", "wrong_actor", "wrong_ledger_counter", "wrong_next_sequence", "invalid_create"} {
		encoded, _ := json.Marshal(first.Data)
		var changed archive.Dataset
		if e = json.Unmarshal(encoded, &changed); e != nil {
			t.Fatal(e)
		}
		for i := range changed.Tables {
			table := &changed.Tables[i]
			if len(table.Rows) == 0 {
				continue
			}
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(table.Rows[0], &fields); e != nil {
				t.Fatal(e)
			}
			switch {
			case table.Name == "investment_operation_revisions" && corruption == "missing_revision":
				table.Rows = nil
				continue
			case table.Name == "investment_operation_revisions" && corruption == "wrong_actor":
				fields["actor_id"], _ = json.Marshal(uuid.NewString())
			case table.Name == "investment_operation_revisions" && corruption == "invalid_create":
				fields["action"] = json.RawMessage(`"amend"`)
			case table.Name == "investment_ledgers" && corruption == "wrong_ledger_counter":
				fields["version"] = json.RawMessage("1")
			case table.Name == "investment_ledgers" && corruption == "wrong_next_sequence":
				fields["next_sequence"] = json.RawMessage("1")
			default:
				continue
			}
			table.Rows[0], _ = json.Marshal(fields)
		}
		if _, err := archive.VerifyJournal(changed); err == nil {
			t.Fatal("accepted archive corruption", corruption)
		}
	}
	raw, e := json.Marshal(first)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.Decode(bytes.NewReader(raw)); e != nil {
		t.Fatal(e)
	}
	verification, e := archive.VerifyJournal(first.Data)
	if e != nil || verification.Operations != 1 || verification.CashLegs != 1 {
		t.Fatal("replay verification", verification, e)
	}
	if !bytes.Contains(raw, []byte("9007199254740993.123456789012")) {
		t.Fatal("money changed")
	}
	if _, e = repo.ExportInvestmentArchive(ctx, tenant, uuid.NewString()); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal(e)
	}
	tx, e := pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, e := readInvestmentArchive(ctx, tx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	mutation.RequestKey = "second"
	mutation.ExpectedLedgerVersion = 2
	mutation.Operation.Amount = "1"
	if _, e = svc.Mutate(ctx, tenant, actor, mutation); e != nil {
		t.Fatal(e)
	}
	stable, e := readInvestmentArchive(ctx, tx, tenant, actor)
	if e != nil || stable.SHA256 != old.SHA256 {
		t.Fatal("mixed snapshots", e)
	}
	fresh, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil || archiveRows(fresh, "investment_operations") != 2 {
		t.Fatal(e)
	}
	if _, e = pool.DB.Exec(ctx, `CREATE TABLE investment_unclassified_domain(tenant_id uuid)`); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ExportInvestmentArchive(ctx, tenant, actor); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("new domain table silently omitted", e)
	}
}
