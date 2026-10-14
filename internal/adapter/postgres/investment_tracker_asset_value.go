package postgres

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	calc "github.com/positron48/budget/internal/usecase/investmentcalc"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

// Asset overview keeps its existing current CBR/manual conversion convention.
// Tracker accounts contribute clean securities only: no funding, cash, taxes,
// hypothetical coupon accrual, acquisition costs or received payouts enter value.
func readAssetLedgerValue(ctx context.Context, tx pgx.Tx, pool *Pool, tenant, actor, ledgerID, currency, convention string, at time.Time) (nav.Result, error) {
	var out nav.Result
	var role string
	if e := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role); errors.Is(e, pgx.ErrNoRows) {
		return out, d.ErrInvestmentForbidden
	} else if e != nil {
		return out, e
	}
	if role != "owner" && role != "admin" && role != "member" {
		return out, d.ErrInvestmentForbidden
	}
	ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, ledgerID))
	if e != nil {
		return out, investmentError(e)
	}
	if !ledger.PortfolioTracking {
		return readInvestmentValuationPolicy(ctx, tx, nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: []string{ledgerID}, At: at, Currency: currency, FXPolicyKey: assetInvestmentFXPolicy, ConventionKey: convention}, nil, pool)
	}
	if convention != "" {
		return out, d.ErrInvestmentInvalid
	}
	ops, e := loadInvestmentOperations(ctx, tx, tenant, ledgerID)
	if e != nil {
		return out, e
	}
	state, e := replayInvestmentLedgerTx(ctx, tx, tenant, ledger, ops, at)
	if e != nil {
		return out, e
	}
	keys := []string{}
	for _, p := range state.Snapshot.Positions {
		q, ok := new(big.Rat).SetString(p.Quantity)
		if !ok || q.Sign() < 0 {
			return out, d.ErrInvestmentPrecondition
		}
		if q.Sign() > 0 {
			keys = append(keys, p.InstrumentKey)
		}
	}
	keys, e = nav.NormalizeQuoteKeys(keys)
	if e != nil {
		return out, e
	}
	quotes, e := readPortfolioQuotes(ctx, tx, tenant, keys, at)
	if e != nil {
		return out, e
	}
	byKey := map[string]nav.PortfolioQuote{}
	for _, q := range quotes.Quotes {
		byKey[q.InstrumentKey] = q
	}
	components := []calc.ExactComponent{}
	codes := map[string]bool{}
	issues := []calc.QualityIssue{}
	out = nav.Result{TenantID: tenant, Currency: currency, At: at, FXPolicy: assetInvestmentFXPolicy, LedgerVersions: map[string]int64{ledger.ID: ledger.Version}, Components: map[string]nav.ComponentIdentity{}, Evidence: map[string]nav.PositionProvenance{}}
	for _, p := range state.Snapshot.Positions {
		quantity, _ := new(big.Rat).SetString(p.Quantity)
		if quantity.Sign() == 0 {
			continue
		}
		quote := byKey[p.InstrumentKey]
		if quote.State != "fresh" || quote.CleanPrice == nil {
			issues = append(issues, calc.QualityIssue{ComponentID: p.InstrumentKey, Code: "price_missing"})
			continue
		}
		codes[quote.Currency] = true
		components = append(components, calc.ExactComponent{ID: p.InstrumentKey, Currency: quote.Currency, Amount: new(big.Rat).Mul(quantity, quote.CleanPrice)})
		out.Components[p.InstrumentKey] = nav.ComponentIdentity{LedgerID: ledger.ID, Kind: "position", Key: p.InstrumentKey, Currency: quote.Currency, Quantity: p.Quantity}
		out.Evidence[p.InstrumentKey] = nav.PositionProvenance{MappingVersion: quote.MappingVersion}
	}
	fx, e := readAssetCurrencyFX(ctx, tx, pool, codes, currency, at)
	if e != nil {
		return out, e
	}
	value, e := calc.ValueExact(components, fx, currency, assetInvestmentFXPolicy, at)
	if e != nil {
		return out, e
	}
	value.Issues = append(value.Issues, issues...)
	value.Complete = value.Complete && len(issues) == 0
	out.Valuation = value
	return out, nil
}
