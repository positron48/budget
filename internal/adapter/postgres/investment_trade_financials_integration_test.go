package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
	"testing"
	"time"
)

func TestInvestmentTradeNativeCurrenciesPreviewAuditPayoutsAndRestore(t *testing.T) {
	pool, done := withPg(t)
	defer done()
	ctx := context.Background()
	tenant, user := uuid.NewString(), uuid.NewString()
	if _, e := pool.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Native trades','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'native-trades@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	repo := NewInvestmentRepo(pool)
	svc := inv.NewService(repo, NewTenantRepo(pool))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	l, e := svc.CreateLedger(ctx, tenant, user, "Broker", start, "ledger")
	if e != nil {
		t.Fatal(e)
	}
	version := int64(1)
	for _, v := range []struct{ ccy, amount string }{{"USD", "10"}, {"RUB", "10000"}, {"EUR", "10"}} {
		if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: version, RequestKey: v.ccy, Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: v.ccy, Amount: v.amount, OccurredAt: start}}); e != nil {
			t.Fatal(e)
		}
		version++
	}
	settlement := start.AddDate(0, 0, 3)
	feeAt := settlement.AddDate(0, 0, 1)
	money := func(amount, currency, num, den string, at time.Time) d.InvestmentTradeMoney {
		v := d.InvestmentTradeMoney{Amount: amount, Currency: currency, At: at}
		if num != "" {
			v.FXNumerator = num
			v.FXDenominator = den
			v.FXAt = at
			v.FXSource = "Reviewed contract"
			v.FXEvidence = "Synthetic report rate"
		}
		return v
	}
	f := &d.InvestmentTradeFinancials{Price: "50", PriceUnit: "currency_per_unit", Principal: money("9000", "RUB", "1", "90", settlement), Accrued: money("90", "RUB", "1", "90", settlement), Fee: money("2", "EUR", "1", "3", feeAt), Tax: money("1", "USD", "", "", settlement), ActualSettlement: "9090"}
	m := inv.Mutation{Action: "create", ExpectedLedgerVersion: version, RequestKey: "buy", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "buy", InstrumentKey: "stock", Quantity: "2", Amount: "0", Currency: "USD", OccurredAt: start.AddDate(0, 0, 1), SettlementAt: settlement, TradeFinancials: f}}
	preview, e := svc.PreviewMutation(ctx, tenant, user, m)
	if e != nil || !preview.Valid || preview.Proposed.Fee != "0.666666666667" || len(preview.Comparisons) != 4 || preview.Proposed.TradeFinancials.CalculatedSettlement != "9090" {
		t.Fatal(preview, e)
	}
	m.PreviewHash = preview.Hash
	posted, e := svc.Mutate(ctx, tenant, user, m)
	if e != nil {
		t.Fatal(e)
	}
	version++
	snap, e := svc.Snapshot(ctx, tenant, user, l.ID, settlement)
	if e != nil {
		t.Fatal(e)
	}
	native := map[string]d.InvestmentCash{}
	for _, v := range snap.Cash {
		native[v.Currency] = v
	}
	if native["RUB"].Settled != "910" || native["USD"].Settled != "9" || native["EUR"].Settled != "10" || native["EUR"].Payable != "2" {
		t.Fatal(native)
	}
	if again, e := svc.Mutate(ctx, tenant, user, m); e != nil || again.ID != posted.ID {
		t.Fatal(again, e)
	}
	payouts, e := svc.Payouts(ctx, tenant, user, inv.PayoutQuery{FromDate: "2024-01-01", ToDate: "2024-01-31", Limit: 100})
	if e != nil || payouts.TotalCount != 2 {
		t.Fatal(payouts, e)
	}
	fees, taxes := 0, 0
	for _, v := range payouts.Entries {
		if v.Kind == "fee" {
			fees++
			if v.Currency != "EUR" || v.Net.RatString() != "-2" || !v.At.Equal(feeAt) {
				t.Fatal(v)
			}
		}
		if v.Kind == "tax" {
			taxes++
			if v.Currency != "USD" || v.Net.RatString() != "-1" {
				t.Fatal(v)
			}
		}
	}
	if fees != 1 || taxes != 1 {
		t.Fatal(payouts)
	}
	// EXPLAIN the exact changed payout SELECT against the representative native
	// commission and tax facts, rather than assuming an existing index suffices.
	plan, e := pool.DB.Query(ctx, "EXPLAIN "+investmentPayoutOperationsSQL, tenant, []string{l.ID}, inv.MaxPayoutOperations+1)
	if e != nil {
		t.Fatal(e)
	}
	lines := 0
	for plan.Next() {
		var text string
		if e = plan.Scan(&text); e != nil {
			t.Fatal(e)
		}
		lines++
	}
	e = plan.Err()
	plan.Close()
	if e != nil || lines == 0 {
		t.Fatal("payout query plan", e)
	}
	// Price comparison and statement amount are independent facts. Unexplained
	// changes fail atomically; a reviewed rounding difference changes exact cost.
	amended := posted
	terms := *posted.TradeFinancials
	amended.TradeFinancials = &terms
	terms.ActualSettlement = "9090.000001"
	terms.CalculatedSettlement = ""
	terms.SettlementDifference = ""
	terms.PrincipalDifferenceNumerator = ""
	terms.PrincipalDifferenceDenominator = ""
	amended.Amount = "0"
	change := inv.Mutation{Action: "amend", ExpectedLedgerVersion: version, ExpectedOperationVersion: 1, RequestKey: "rounding", Reason: "Report rounding", Operation: amended}
	if _, e = svc.Mutate(ctx, tenant, user, change); !errors.Is(e, d.ErrInvestmentInvalid) {
		t.Fatal("unexplained discrepancy", e)
	}
	terms.DifferenceReason = "Confirmed rounding allocated to clean principal"
	change.Operation.TradeFinancials = &terms
	preview, e = svc.PreviewMutation(ctx, tenant, user, change)
	if e != nil || !preview.Valid || preview.Proposed.TradeFinancials.SettlementDifference != "0.000001" {
		t.Fatal(preview, e)
	}
	change.PreviewHash = preview.Hash
	fixed, e := svc.Mutate(ctx, tenant, user, change)
	if e != nil || fixed.TradeFinancials.ActualSettlement != "9090.000001" {
		t.Fatal(fixed, e)
	}
	var original string
	if e = pool.DB.QueryRow(ctx, `SELECT after_data->'TradeFinancials'->>'ActualSettlement' FROM investment_operation_revisions WHERE tenant_id=$1 AND operation_id=$2 AND version=1`, tenant, posted.ID).Scan(&original); e != nil || original != "9090" {
		t.Fatal(original, e)
	}
	bundle, e := repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal(e)
	}
	// Rebuild integrity must catch a modified original FX ratio, even when all
	// rounded compatibility summaries and materialized movements were left intact.
	altered := bundle.Data
	altered.Tables = append([]archive.Table(nil), bundle.Data.Tables...)
	for i, table := range altered.Tables {
		if table.Name != "investment_operations" {
			continue
		}
		altered.Tables[i].Rows = append([]json.RawMessage(nil), table.Rows...)
		for j, raw := range table.Rows {
			var row map[string]json.RawMessage
			if e = json.Unmarshal(raw, &row); e != nil {
				t.Fatal(e)
			}
			var op d.InvestmentOperation
			if e = json.Unmarshal(row["data"], &op); e != nil {
				t.Fatal(e)
			}
			if op.ID != posted.ID {
				continue
			}
			op.TradeFinancials.Fee.FXNumerator = "2"
			row["data"], _ = json.Marshal(op)
			altered.Tables[i].Rows[j], _ = json.Marshal(row)
		}
	}
	if _, e = archive.VerifyJournal(altered); e == nil {
		t.Fatal("altered FX proof accepted")
	}
	target, cleanup := withPg(t)
	defer cleanup()
	if _, e = target.DB.Exec(ctx, `INSERT INTO tenants(id,name,default_currency_code) VALUES($1,'Restore','USD')`, tenant); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'native-restore@example.test','x')`, user); e != nil {
		t.Fatal(e)
	}
	if _, e = target.DB.Exec(ctx, `INSERT INTO user_tenants(tenant_id,user_id,role) VALUES($1,$2,'owner')`, tenant, user); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentRepo(target).CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal(e)
	}

	// A manually reviewed source batch retains a quote in a third currency. The
	// payout SELECT must include a trade with tax but no commission.
	if _, e = svc.Mutate(ctx, tenant, user, inv.Mutation{Action: "create", ExpectedLedgerVersion: 6, RequestKey: "extra-fund", Operation: d.InvestmentOperation{LedgerID: l.ID, Kind: "deposit", Currency: "RUB", Amount: "10000", OccurredAt: start}}); e != nil {
		t.Fatal(e)
	}
	prepared, e := imp.PrepareUpload(imp.Upload{Filename: "native-trade.csv", RequestKey: "upload", CSV: []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,stock,150,2,EUR,0,X,0,,False,\n"), Accounts: []imp.ImportAccount{{SourceKey: "other", Name: "Other"}}, Assignments: []imp.Assignment{{Record: 1, AccountKey: "other"}}})
	if e != nil {
		t.Fatal(e)
	}
	imported, e := NewInvestmentImportRepo(pool).SaveImport(ctx, tenant, user, prepared)
	if e != nil {
		t.Fatal(e)
	}
	taxOnly := m.Operation
	taxTerms := *f
	taxOnly.TradeFinancials = &taxTerms
	taxTerms.Price = "150"
	taxTerms.PriceCurrency = "EUR"
	taxTerms.PriceFXNumerator = "1"
	taxTerms.PriceFXDenominator = "3"
	taxTerms.PriceFXSource = "Execution contract"
	taxTerms.PriceFXEvidence = "Original EUR quote"
	taxTerms.PriceFXAt = taxOnly.OccurredAt
	taxTerms.Fee = d.InvestmentTradeMoney{Amount: "0", Currency: "EUR"}
	taxTerms.Tax.Amount = "3"
	batch := inv.ImportBatch{ImportID: imported.ID, PreviewHash: prepared.Preview.Hash, ExpectedImportVersion: 1, RequestKey: "native-batch", ExpectedLedgerVersions: map[string]int64{l.ID: 7}, Entries: []inv.ImportBatchEntry{{Record: 1, ExpectedRowVersion: 1, ReviewReason: "Confirmed original quote and native settlement", Operation: taxOnly}}}
	bp, e := svc.PreviewImportBatch(ctx, tenant, user, batch)
	if e != nil {
		t.Fatal(e)
	}
	committed, e := svc.CommitImportBatch(ctx, tenant, user, batch, bp.Hash)
	if e != nil || committed.Operations[0].TradeFinancials.PriceCurrency != "EUR" || committed.Operations[0].Amount != "100" {
		t.Fatal(committed, e)
	}
	payouts, e = svc.Payouts(ctx, tenant, user, inv.PayoutQuery{FromDate: "2024-01-01", ToDate: "2024-01-31", Limit: 100})
	if e != nil || payouts.TotalCount != 3 {
		t.Fatal("tax-only trade missing from payouts", payouts, e)
	}
	bundle, e = repo.ExportInvestmentArchive(ctx, tenant, user)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = archive.VerifyJournal(bundle.Data); e != nil {
		t.Fatal("batch quote FX archive", e)
	}
	if _, e = NewInvestmentRepo(target).CheckInvestmentArchiveRestore(ctx, user, bundle); e != nil {
		t.Fatal("batch quote FX restore", e)
	}
}
