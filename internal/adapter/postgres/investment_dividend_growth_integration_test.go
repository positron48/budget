package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	"testing"
	"time"
)

func TestInvestmentDividendGrowthPersistenceRolesRestore(t *testing.T) {
	pool, cleanup := withPg(t)
	defer cleanup()
	ctx := context.Background()
	var tenant, foreign, actor, member string
	for _, v := range []struct {
		sql string
		out *string
	}{{`INSERT INTO tenants(name,default_currency_code) VALUES('Dividend','USD') RETURNING id`, &tenant}, {`INSERT INTO tenants(name,default_currency_code) VALUES('Foreign','USD') RETURNING id`, &foreign}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('dividend-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &actor}, {fmt.Sprintf(`INSERT INTO users(email,password_hash) VALUES('dividend-member-%d@example.test','fixture') RETURNING id`, time.Now().UnixNano()), &member}} {
		if e := pool.DB.QueryRow(ctx, v.sql).Scan(v.out); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner'),($1,$3,'member'),($4,$2,'owner')`, tenant, actor, member, foreign); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	h := inv.DividendHistory{ID: uuid.NewString(), InstrumentKey: "STOCK", Currency: "USD", Timezone: "Europe/Moscow", BasisAt: day.AddDate(1, 1, 0), ReviewedFrom: day.AddDate(-1, 0, 0), SourceReference: "Issuer fixture", Evidence: "Complete split history reviewed", Reason: "Original review", Years: []inv.DividendYearReview{{Year: 2023, Complete: true, SourceReference: "Annual report", Evidence: "Complete"}, {Year: 2024, Complete: true, SourceReference: "Annual report", Evidence: "Complete"}}, Dividends: []inv.DividendDeclaration{{ID: uuid.NewString(), PaymentDate: "2023-06-01", Amount: "10", Class: "regular", Status: "paid", UnitBasisAt: day.AddDate(-1, 0, 0), SourceReference: "Issuer", Evidence: "Old units"}, {ID: uuid.NewString(), PaymentDate: "2024-06-01", Amount: "6", Class: "regular", Status: "paid", UnitBasisAt: day, SourceReference: "Issuer", Evidence: "New units"}}, Splits: []inv.DividendUnitSplit{{ID: uuid.NewString(), NewUnits: "2", OldUnits: "1", EffectiveAt: day, SourceReference: "Issuer", Evidence: "Two for one"}}}
	saved, e := svc.SaveDividendHistory(ctx, tenant, actor, h, 0)
	if e != nil || saved.Version != 1 {
		t.Fatal(e, saved)
	}
	if retry, e := svc.SaveDividendHistory(ctx, tenant, actor, h, 0); e != nil || retry.Version != 1 {
		t.Fatal("retry", e)
	}
	if _, e = repo.SaveDividendHistory(ctx, tenant, member, h, 1); !errors.Is(e, d.ErrInvestmentForbidden) {
		t.Fatal("member write", e)
	}
	report, e := svc.DividendGrowth(ctx, tenant, member)
	if e != nil || len(report.Histories) != 1 || report.Histories[0].Years[1].PercentChange.RatString() != "20" {
		t.Fatal(e, report)
	}
	other, e := svc.DividendGrowth(ctx, foreign, actor)
	if e != nil || len(other.Histories) != 0 {
		t.Fatal("tenant leak", e, other)
	}
	duplicate := h
	duplicate.ID = uuid.NewString()
	if _, e = svc.SaveDividendHistory(ctx, tenant, actor, duplicate, 0); !errors.Is(e, d.ErrInvestmentConflict) {
		t.Fatal("duplicate identity", e)
	}
	// Competing edits of the same reviewed source leave one complete revision.
	results := make(chan error, 2)
	for _, reason := range []string{"Review A", "Review B"} {
		go func(reason string) {
			edit := h
			edit.Reason = reason
			_, e := svc.SaveDividendHistory(ctx, tenant, actor, edit, 1)
			results <- e
		}(reason)
	}
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, d.ErrInvestmentConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(bundle.Data)
	var corrupt archive.Dataset
	_ = json.Unmarshal(encoded, &corrupt)
	for i := range corrupt.Tables {
		if corrupt.Tables[i].Name == "investment_dividend_history_revisions" {
			corrupt.Tables[i].Rows = corrupt.Tables[i].Rows[1:]
		}
	}
	if e = archive.VerifyDividendHistories(corrupt); e == nil {
		t.Fatal("missing revision accepted")
	}
	target, cleanTarget := withPg(t)
	defer cleanTarget()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restored dividend','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'restored-dividend@example.test','fixture')`, actor); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, actor); e != nil {
		t.Fatal(e)
	}
	targetRepo := NewInvestmentRepo(target)
	if _, e = targetRepo.RestoreInvestmentArchive(ctx, actor, bundle, bundle.SHA256); e != nil {
		t.Fatal(e)
	}
	restored, e := targetRepo.DividendGrowth(ctx, tenant, actor)
	if e != nil || len(restored.Histories) != 1 || restored.Histories[0].Years[1].PercentChange.RatString() != "20" {
		t.Fatal(e, restored)
	}
	current := restored.Histories[0].History
	current.Cancelled = true
	current.Reason = "Cancel reviewed source"
	if _, e = targetRepo.SaveDividendHistory(ctx, tenant, actor, current, 2); e != nil {
		t.Fatal(e)
	}
	cancelled, e := targetRepo.DividendGrowth(ctx, tenant, actor)
	if e != nil || cancelled.Histories[0].Years[1].Status != "cancelled" || cancelled.Histories[0].Years[1].PercentChange != nil {
		t.Fatal(e, cancelled)
	}
	assertInvestmentRollbackRefused(t, ctx, pool, "0056_investment_dividend_growth")
	// The exact tenant read shape uses the PK prefix; no journal scan is involved.
	rows, e := pool.DB.Query(ctx, `EXPLAIN SELECT data FROM investment_dividend_histories WHERE tenant_id=$1 ORDER BY id LIMIT 101`, tenant)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var line string
		if e = rows.Scan(&line); e != nil {
			t.Fatal(e)
		}
		t.Log(line)
	}
	rows.Close()
}
