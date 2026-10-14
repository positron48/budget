package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	d "github.com/positron48/budget/internal/domain"
	"github.com/positron48/budget/internal/pkg/ctxutil"
)

// TrackerAssetLink identifies an existing manual estimate reviewed by its owner.
// Empty portfolios may replace a manual amount: unassigned history lives in the
// owner's chosen residual portfolio, and retaining that amount would count twice.
type TrackerAssetLink struct {
	AccountID, AccountName, LedgerID, LedgerName, Currency string
	ExpectedAccountVersion, ExpectedManualMinor            int64
}

type TrackerAssetLinkResult struct {
	AccountID, LedgerID          string
	ManualMinor, SecuritiesMinor int64
	Complete, Reused             bool
}

func TrackerAssetLinksSHA256(links []TrackerAssetLink) string { return investmentHash(links) }

// LinkTrackerAssets exercises the normal activation contract for the entire
// reviewed batch, including audit records. Default rehearsal rolls back everything.
// Historical manual facts and all operations remain intact. Retries reuse links.
func (r *InvestmentRepo) LinkTrackerAssets(ctx context.Context, tenant, actor string, links []TrackerAssetLink, expected string, apply bool) ([]TrackerAssetLinkResult, error) {
	if len(links) == 0 || len(links) > 20 || expected != TrackerAssetLinksSHA256(links) {
		return nil, d.ErrInvestmentInvalid
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, e = tx.Exec(ctx, `SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='45s'`); e != nil {
		return nil, e
	}
	if e = lockInvestment(ctx, tx, tenant); e != nil {
		return nil, e
	}
	if e = groupAccess(ctx, tx, tenant, actor, true); e != nil {
		return nil, e
	}
	var role string
	if e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2`, tenant, actor).Scan(&role); e != nil {
		return nil, e
	}
	if role != "owner" && role != "admin" {
		return nil, d.ErrInvestmentForbidden
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	ctx = ctxutil.WithUserID(context.WithValue(ctx, financialTxKey{}, tx), actor)
	seenAccounts, seenLedgers := map[string]bool{}, map[string]bool{}
	result := []TrackerAssetLinkResult{}
	for _, link := range links {
		if _, e = uuid.Parse(link.AccountID); e != nil {
			return nil, d.ErrInvestmentInvalid
		}
		if _, e = uuid.Parse(link.LedgerID); e != nil {
			return nil, d.ErrInvestmentInvalid
		}
		if seenAccounts[link.AccountID] || seenLedgers[link.LedgerID] || link.ExpectedAccountVersion < 1 || link.AccountName == "" || link.LedgerName == "" || link.Currency == "" {
			return nil, d.ErrInvestmentInvalid
		}
		seenAccounts[link.AccountID], seenLedgers[link.LedgerID] = true, true
		a, e := scanAccount(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM asset_accounts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, link.AccountID))
		if e != nil {
			return nil, investmentError(e)
		}
		l, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, link.LedgerID))
		if e != nil {
			return nil, investmentError(e)
		}
		if a.Kind != "investment" || a.Archived || l.Archived || !l.PortfolioTracking || a.Name != link.AccountName || l.Name != link.LedgerName || a.FixedCurrencyCode != link.Currency {
			return nil, d.ErrInvestmentPrecondition
		}
		reused := a.InvestmentMode == "ledger" && a.InvestmentLedgerID == l.ID && a.Version == link.ExpectedAccountVersion+1
		if !reused {
			if a.InvestmentMode != "manual" || a.Version != link.ExpectedAccountVersion {
				return nil, d.ErrInvestmentConflict
			}
			m := d.InvestmentAssetActivation{AccountID: a.ID, LedgerID: l.ID, ValuationFrom: at, RequestKey: "tracker-assets:" + expected + ":" + a.ID, Evidence: "Owner reviewed replacement of manual estimate with tracker securities; residual history remains in the designated portfolio"}
			p, e := previewAssetActivationTx(ctx, tx, r.pool, tenant, actor, m)
			if e != nil {
				return nil, e
			}
			if p.ManualValue.CurrencyCode != link.Currency || p.ManualValue.MinorUnits != link.ExpectedManualMinor {
				return nil, d.ErrInvestmentConflict
			}
			m.PreviewHash, m.ExpectedAccountVersion, m.ExpectedLedgerVersion = p.PreviewHash, p.Account.Version, p.Ledger.Version
			if _, e = activateAssetTx(ctx, tx, r.pool, tenant, actor, m); e != nil {
				return nil, e
			}
		}
		current, e := NewAssetRepo(r.pool).GetAccount(ctx, tenant, a.ID, at)
		if e != nil {
			return nil, e
		}
		if current.InvestmentLedgerID != l.ID || len(current.Balances) != 1 {
			return nil, d.ErrInvestmentPrecondition
		}
		result = append(result, TrackerAssetLinkResult{AccountID: a.ID, LedgerID: l.ID, ManualMinor: link.ExpectedManualMinor, SecuritiesMinor: current.Balances[0].Amount.MinorUnits, Complete: !current.ValuationIncomplete, Reused: reused})
	}
	if apply {
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
	}
	return result, nil
}
