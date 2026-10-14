package postgres

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	imp "github.com/positron48/budget/internal/usecase/investmentimport"
)

func TestInvestmentCandidateRebuildUsesTargetAccountsWithoutTouchingExistingBudget(t *testing.T) {
	source, done := withPg(t)
	defer done()
	ctx, tenant, actor := acceptanceOwner(t, source)
	repo := NewInvestmentRepo(source)
	svc := inv.NewService(repo, NewTenantRepo(source))
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger, e := svc.CreateLedgerWithDetails(ctx, tenant, actor, d.InvestmentLedger{Name: "Existing account", StartedAt: at, PortfolioTracking: true}, "account")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Mutate(ctx, tenant, actor, inv.Mutation{Action: "create", RequestKey: "old", ExpectedLedgerVersion: 1, Operation: d.InvestmentOperation{LedgerID: ledger.ID, Kind: "buy", InstrumentKey: "OLD", Currency: "RUB", Quantity: "99", Amount: "990", OccurredAt: at}}); e != nil {
		t.Fatal(e)
	}
	if _, e = NewInvestmentImportRepo(source).SaveBrokerSnapshot(ctx, tenant, actor, brokerSnapshotFixture(t, "9007199254740993", true)); e != nil {
		t.Fatal(e)
	}
	before, e := repo.ExportInvestmentArchive(ctx, tenant, actor)
	if e != nil {
		t.Fatal(e)
	}
	csv := []byte("Event,Date,Symbol,Price,Quantity,Currency,FeeTax,Exchange,NKD,FeeCurrency,DoNotAdjustCash,Note\nBUY,2024-01-02 12:00:00,TEST,10,1,RUB,0,MCX,0,,False,\nDIVIDEND,2024-01-03 12:00:00,TEST,0,7,RUB,0,MCX,0,,False,\n")
	staged, e := imp.StageSnowball(bytes.NewReader(csv), true)
	if e != nil {
		t.Fatal(e)
	}
	mapping := imp.FinancialMapping{SourceSHA256: staged.Audit.SHA256, Timezone: "Europe/Moscow", SettlementPolicy: "assume_trade_time", LedgerByRecord: map[int]string{1: ledger.ID, 2: ledger.ID}, Rules: []imp.FinancialRule{{Event: "BUY", Symbol: "TEST", Exchange: "MCX", Currency: "RUB", InstrumentKey: "TEST", Kind: "buy", AmountBasis: "quantity_price", QuantityFactor: "1", AmountFactor: "1", AccruedBasis: "per_security_unit", FeePolicy: "native_currency", Reason: "Synthetic purchase"}, {Event: "DIVIDEND", Symbol: "TEST", Exchange: "MCX", Currency: "RUB", InstrumentKey: "TEST", Kind: "dividend", AmountBasis: "quantity", AmountFactor: "1", FeePolicy: "native_currency", IncomeBasis: "received", Reason: "Synthetic payout"}}}
	mapped, e := imp.MapSnowballFinancial(bytes.NewReader(csv), mapping)
	if e != nil {
		t.Fatal(e)
	}
	ops := []d.InvestmentOperation{}
	for _, row := range mapped.Rows {
		if row.Operation == nil {
			t.Fatal("synthetic source unresolved")
		}
		o := *row.Operation
		o.ID = uuid.NewString()
		o.TenantID = tenant
		o.Status = "posted"
		o.Sequence = int64(row.Record)
		ops = append(ops, o)
	}
	expected, e := inv.Replay(ledger, ops, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	input := PortfolioCandidateInput{OriginalCSV: csv, Sources: []PortfolioCandidateSource{{Filename: "source.csv", LedgerID: ledger.ID, AccountKey: "account", CSV: csv, Mapping: mapping, AcceptedRecords: []int{1, 2}, Lineage: []PortfolioCandidateLineage{{SourceRecord: 1, Reason: "Original row1"}, {SourceRecord: 2, Reason: "Original row2"}}, ExpectedPositions: expected.Snapshot.Positions}}}
	candidate, candidateDone := withPg(t)
	defer candidateDone()
	built, e := NewInvestmentRepo(candidate).BuildPortfolioCandidate(ctx, actor, before, input)
	if e != nil || built.Ledgers != 1 || built.Operations != 2 || built.Payouts != 1 || built.PendingRows != 0 {
		t.Fatal("candidate build", e)
	}
	if archiveRows(built.Archive, "investment_broker_snapshots") != archiveRows(before, "investment_broker_snapshots") {
		t.Fatal("immutable broker sources lost")
	}
	state, e := NewInvestmentRepo(candidate).Snapshot(ctx, tenant, ledger.ID, time.Now().UTC())
	if e != nil || len(state.Positions) != 1 || state.Positions[0].InstrumentKey != "TEST" || state.Positions[0].Quantity != "1" {
		t.Fatal("candidate did not replace old history in isolation", e)
	}
	original, e := svc.Snapshot(ctx, tenant, actor, ledger.ID, time.Now().UTC())
	if e != nil || len(original.Positions) != 1 || original.Positions[0].InstrumentKey != "OLD" || original.Positions[0].Quantity != "99" {
		t.Fatal("original history changed", e)
	}
	if e = repo.SeedPortfolioCandidateScope(ctx, actor, before); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("populated source DB accepted", e)
	}
	var candidatePassword string
	if e = candidate.DB.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, actor).Scan(&candidatePassword); e != nil || candidatePassword != "disabled" {
		t.Fatal("auth data copied to candidate", e)
	}
	invalid := input
	invalid.Sources = append([]PortfolioCandidateSource(nil), input.Sources...)
	invalid.Sources[0].Lineage = []PortfolioCandidateLineage{{SourceRecord: 2, Reason: "Wrong original"}, {SourceRecord: 2, Reason: "Duplicate"}}
	if _, e = NewInvestmentRepo(candidate).BuildPortfolioCandidate(ctx, actor, before, invalid); !errors.Is(e, d.ErrInvestmentPrecondition) {
		t.Fatal("altered original row lineage accepted", fmt.Sprint(e))
	}
}
