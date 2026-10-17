package investment

import (
	"math/big"
	"testing"
	"time"

	d "github.com/positron48/budget/internal/domain"
)

func TestPayoutGrowthCompletedMonthsExactAndPageIndependent(t *testing.T) {
	ledgers, _, q, now := payoutFixture()
	for i := range ledgers {
		ledgers[i].StartedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	q.FromDate = "2024-02-01"
	q.ToDate = "2024-02-29"
	q.Limit = 1
	mk := func(id, kind, amount, currency string, at time.Time) d.InvestmentOperation {
		o := op(id, kind, amount, 1)
		o.LedgerID = payoutLedgerID
		o.Currency = currency
		o.OccurredAt = at
		o.Version = 1
		if kind == "dividend" {
			o.InstrumentKey = "STOCK"
		}
		return o
	}
	old := time.Date(2023, 1, 31, 22, 0, 0, 0, time.UTC)
	current := old.AddDate(1, 0, 0)
	ops := []d.InvestmentOperation{
		mk("old", "dividend", "9007199254740993.1234567891", "USD", old),
		mk("new", "dividend", "18014398509481986.2469135782", "USD", current),
		mk("old-eur", "cash_interest", "10", "EUR", old),
		mk("new-eur", "cash_interest", "15", "EUR", current),
		mk("fee", "fee", "5", "USD", current),
		mk("refund", "tax_refund", "1000", "USD", current),
	}
	report, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || len(report.Growth) != 2 || report.TotalCount != 4 {
		t.Fatal(e, report)
	}
	eur, usd := report.Growth[0], report.Growth[1]
	if usd.Month != "2024-02" || usd.PreviousMonth != "2023-02" || usd.Status != "available" {
		t.Fatal(usd)
	}
	expectPayoutRat(t, usd.Change, "9007199254740993.1234567891")
	expectPayoutRat(t, usd.PercentChange, "100")
	expectPayoutRat(t, eur.PercentChange, "50")
	// Comparison is from the same snapshot, never from only the visible page.
	q.Cursor = report.NextCursor
	second, e := BuildPayoutReport("tenant", ledgers, ops, q, now.Add(time.Hour))
	if e != nil || len(second.Growth) != 2 {
		t.Fatal(e)
	}
	expectPayoutRat(t, second.Growth[1].PercentChange, "100")
	// Existing filters also constrain baseline receipts.
	q.Cursor = ""
	q.Kinds = []string{"cash_interest"}
	filtered, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || len(filtered.Growth) != 1 || filtered.Growth[0].Currency != "EUR" {
		t.Fatal(e, filtered.Growth)
	}
	q.Kinds = []string{"fee"}
	filtered, e = BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || len(filtered.Growth) != 0 {
		t.Fatal("fees became passive income", e)
	}
}

func TestPayoutGrowthPartialMissingZeroAndDecline(t *testing.T) {
	ledgers, ops, q, now := payoutFixture()
	q.LedgerIDs = []string{payoutLedgerID}
	q.FromDate = "2024-02-01"
	q.ToDate = "2024-02-29"
	check := func(status string) {
		t.Helper()
		r, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
		if e != nil || len(r.Growth) != 1 || r.Growth[0].Status != status || r.Growth[0].PercentChange != nil {
			t.Fatal(status, e, r.Growth)
		}
		if status != "zero_baseline" && r.Growth[0].Change != nil {
			t.Fatal("unmatched interval has growth")
		}
	}
	check("incomplete_history")
	ledgers[0].StartedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	check("zero_baseline")
	q.FromDate = "2024-02-02"
	check("partial_interval")
	q.FromDate = "2024-02-01"
	q.ToDate = "2024-02-28"
	check("partial_interval")
	q.ToDate = "2024-02-29"
	now = time.Date(2024, 2, 29, 20, 59, 59, 0, time.UTC)
	check("partial_interval")
	now = time.Date(2024, 2, 29, 21, 0, 0, 0, time.UTC)
	check("zero_baseline")
	// A prior receipt with no current receipt is a real -100%, with the empty
	// current month retained. A voided prior receipt contributes nothing.
	for i := range ops {
		ops[i].OccurredAt = ops[i].OccurredAt.AddDate(-1, 0, 0)
		if !ops[i].SettlementAt.IsZero() {
			ops[i].SettlementAt = ops[i].SettlementAt.AddDate(-1, 0, 0)
		}
	}
	q.InstrumentKey = "STOCK"
	q.Kinds = []string{"dividend"}
	r, e := BuildPayoutReport("tenant", ledgers, ops, q, now)
	if e != nil || r.TotalCount != 0 || len(r.Growth) != 1 {
		t.Fatal(e, r)
	}
	expectPayoutRat(t, r.Growth[0].IncomeNet, "0")
	expectPayoutRat(t, r.Growth[0].PercentChange, "-100")
}

func TestTrackerPayoutGrowthUsesRecordedAmountsBeforeCreation(t *testing.T) {
	ledgers, _, q, now := payoutFixture()
	q.LedgerIDs = []string{payoutLedgerID}
	q.FromDate, q.ToDate = "2024-02-01", "2024-02-29"
	for i := range ledgers {
		ledgers[i].PortfolioTracking = true
		ledgers[i].StartedAt = now
	}
	old := op("tracker-old", "dividend", "100", 1)
	old.LedgerID, old.InstrumentKey, old.Tax = payoutLedgerID, "STOCK", "30"
	old.OccurredAt = time.Date(2023, 2, 10, 12, 0, 0, 0, time.UTC)
	current := old
	current.ID, current.Amount, current.Tax = "tracker-current", "200", "10"
	current.OccurredAt = old.OccurredAt.AddDate(1, 0, 0)
	report, err := BuildPayoutReport("tenant", ledgers, []d.InvestmentOperation{old, current}, q, now)
	if err != nil || len(report.Growth) != 1 || report.Growth[0].Status != "available" {
		t.Fatal(err, report)
	}
	expectPayoutRat(t, report.Growth[0].PercentChange, "100")
	expectPayoutRat(t, report.Totals[0].IncomeNet, "200")
}

func TestPayoutGrowthHistoricalDisplayAndNativeEntries(t *testing.T) {
	ledgers, _, q, now := payoutFixture()
	for i := range ledgers {
		ledgers[i].StartedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	q.FromDate, q.ToDate, q.TargetCurrencyCode = "2024-02-01", "2024-02-29", "EUR"
	q.Limit = 200
	at := time.Date(2024, 2, 3, 12, 0, 0, 0, time.UTC)
	mk := func(id, currency, amount string, date time.Time) d.InvestmentOperation {
		v := op(id, "dividend", amount, 1)
		v.LedgerID = payoutLedgerID
		v.InstrumentKey = "STOCK"
		v.Currency = currency
		v.OccurredAt = date
		v.Version = 1
		return v
	}
	operations := []d.InvestmentOperation{mk("previous", "USD", "10", at.AddDate(-1, 0, 0)), mk("current", "USD", "20", at), mk("euro", "EUR", "5", at)}
	convert := func(e PayoutEntry) (PayoutEntry, error) {
		rate := rational("1")
		if e.Currency == "USD" {
			rate = rational("0.8")
			if e.At.Year() == 2023 {
				rate = rational("0.5")
			}
		}
		e.Net = new(big.Rat).Mul(e.Net, rate)
		if e.Gross != nil {
			e.Gross = new(big.Rat).Mul(e.Gross, rate)
		}
		if e.Tax != nil {
			e.Tax = new(big.Rat).Mul(e.Tax, rate)
		}
		e.Currency = "EUR"
		return e, nil
	}
	out, err := BuildConvertedPayoutReport("tenant", ledgers, operations, q, now, convert)
	if err != nil || len(out.Growth) != 1 || len(out.Totals) != 1 || len(out.Entries) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	expectPayoutRat(t, out.Growth[0].IncomeNet, "21")
	expectPayoutRat(t, out.Growth[0].PreviousIncomeNet, "5")
	expectPayoutRat(t, out.Growth[0].PercentChange, "320")
	if operations[1].Currency != "USD" {
		t.Fatal("mutated journal")
	}
	native := false
	for _, e := range out.Entries {
		if e.Currency == "USD" {
			native = true
		}
	}
	if !native {
		t.Fatal("lost original entry currency")
	}
	if _, err := BuildPayoutReport("tenant", ledgers, operations, q, now); err == nil {
		t.Fatal("target silently ignored without rates")
	}
}
