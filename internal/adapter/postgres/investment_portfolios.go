package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func (r *MarketDataRepo) SaveInvestmentPortfolio(ctx context.Context, tenant, actor string, input nav.PortfolioInput) (nav.Portfolio, error) {
	var out nav.Portfolio
	p, e := nav.NormalizePortfolio(input)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	e = tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && role != "owner" && role != "admin" && role != "member" {
		return out, d.ErrInvestmentForbidden
	}
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "investment-portfolios:"+tenant+":"+actor); e != nil {
		return out, e
	}
	hash := investmentHash(p)
	var previousHash string
	var previous []byte
	e = tx.QueryRow(ctx, `SELECT payload_hash,response FROM investment_portfolio_requests WHERE tenant_id=$1 AND actor_id=$2 AND request_key=$3`, tenant, actor, p.RequestKey).Scan(&previousHash, &previous)
	if e == nil {
		if previousHash != hash {
			return out, d.ErrInvestmentConflict
		}
		e = json.Unmarshal(previous, &out)
		return out, e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	id := p.ID
	if id == "" {
		id = uuid.NewString()
	} else {
		var version int64
		e = tx.QueryRow(ctx, `SELECT version FROM investment_portfolios WHERE tenant_id=$1 AND actor_id=$2 AND id=$3 FOR UPDATE`, tenant, actor, id).Scan(&version)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, d.ErrInvestmentNotFound
		}
		if e != nil {
			return out, e
		}
		if version != p.ExpectedVersion {
			return out, d.ErrInvestmentConflict
		}
	}
	if !p.Archived {
		profile, e := readPriceConventions(ctx, tx, tenant, p.ConventionKey)
		if e != nil {
			return out, e
		}
		if !profile.Enabled {
			return out, d.ErrInvestmentPrecondition
		}
		var fxEnabled bool
		e = tx.QueryRow(ctx, `SELECT enabled FROM investment_fx_policies WHERE tenant_id=$1 AND policy_key=$2`, tenant, p.FXPolicyKey).Scan(&fxEnabled)
		if errors.Is(e, pgx.ErrNoRows) || e == nil && !fxEnabled {
			return out, d.ErrInvestmentPrecondition
		}
		if e != nil {
			return out, e
		}
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, tenant, p.LedgerIDs).Scan(&count); e != nil {
			return out, e
		}
		if count != len(p.LedgerIDs) {
			return out, d.ErrInvestmentPrecondition
		}
	}
	q := nav.Request{TenantID: tenant, UserID: actor, LedgerIDs: p.LedgerIDs, Currency: p.Currency, FXPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey}
	key, _, e := navSeries(q)
	if e != nil {
		return out, e
	}
	if p.Archived && p.AutoExtend {
		var completedTo string
		e = tx.QueryRow(ctx, `SELECT to_date::text FROM investment_nav_watches WHERE tenant_id=$1 AND actor_id=$2 AND series_key=$3 FOR UPDATE`, tenant, actor, key).Scan(&completedTo)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if completedTo > p.To {
			p.To = completedTo
		}
	}
	out = nav.Portfolio{AutoExtend: p.AutoExtend, Archived: p.Archived, ID: id, Name: p.Name, LedgerIDs: p.LedgerIDs, Currency: p.Currency, FXPolicyKey: p.FXPolicyKey, ConventionKey: p.ConventionKey, From: p.From, To: p.To, SeriesKey: key, Version: p.ExpectedVersion + 1, UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	raw, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_portfolios(id,tenant_id,actor_id,version,data) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET version=EXCLUDED.version,data=EXCLUDED.data`, id, tenant, actor, out.Version, raw); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_portfolio_revisions(tenant_id,actor_id,portfolio_id,version,data) VALUES($1,$2,$3,$4,$5)`, tenant, actor, id, out.Version, raw); e != nil {
		return out, e
	}
	if !p.Archived {
		if _, e = enqueueDailyNAV(ctx, tx, q, p.From, p.To, true); e != nil {
			return out, e
		}
	}
	if e = reconcilePortfolioWatches(ctx, tx, tenant, actor); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_portfolio_requests(tenant_id,actor_id,request_key,payload_hash,response) VALUES($1,$2,$3,$4,$5)`, tenant, actor, p.RequestKey, hash, raw); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (r *MarketDataRepo) ListInvestmentPortfolios(ctx context.Context, tenant, actor, after string) ([]nav.Portfolio, error) {
	if after != "" {
		if id, e := uuid.Parse(after); e != nil || id.String() != after {
			return nil, d.ErrInvestmentInvalid
		}
	}
	rows, e := r.pool.DB.Query(ctx, `SELECT p.data FROM investment_portfolios p JOIN user_tenants u ON u.tenant_id=p.tenant_id AND u.user_id=p.actor_id WHERE p.tenant_id=$1 AND p.actor_id=$2 AND u.role IN ('owner','admin','member') AND ($3='' OR p.id>NULLIF($3,'')::uuid) ORDER BY p.id LIMIT 100`, tenant, actor, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []nav.Portfolio{}
	for rows.Next() {
		var raw []byte
		var p nav.Portfolio
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *MarketDataRepo) InvestmentPortfolioOptions(ctx context.Context, tenant, actor, priceAfter, fxAfter string) (nav.PortfolioOptions, error) {
	var out nav.PortfolioOptions
	if priceAfter != "" && !nav.ValidConventionKey(priceAfter) || len(fxAfter) > 100 {
		return out, d.ErrInvestmentInvalid
	}
	rows, e := r.pool.DB.Query(ctx, `SELECT p.profile_key,r.data->>'Title',p.version FROM investment_price_conventions p JOIN investment_price_convention_revisions r ON r.tenant_id=p.tenant_id AND r.profile_key=p.profile_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.profile_key>$3 AND r.data->>'Enabled'='true' AND EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member')) ORDER BY p.profile_key LIMIT 100`, tenant, actor, priceAfter)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v nav.ProfileOption
		if e = rows.Scan(&v.Key, &v.Title, &v.Version); e != nil {
			rows.Close()
			return out, e
		}
		out.Price = append(out.Price, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = r.pool.DB.Query(ctx, `SELECT p.policy_key,r.data->>'Mode',p.version FROM investment_fx_policies p JOIN investment_fx_policy_revisions r ON r.tenant_id=p.tenant_id AND r.policy_key=p.policy_key AND r.version=p.version WHERE p.tenant_id=$1 AND p.policy_key>$3 AND p.enabled AND EXISTS(SELECT 1 FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 AND role IN ('owner','admin','member')) ORDER BY p.policy_key LIMIT 100`, tenant, actor, fxAfter)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v nav.ProfileOption
		if e = rows.Scan(&v.Key, &v.Mode, &v.Version); e != nil {
			return out, e
		}
		v.Title = v.Key
		out.FX = append(out.FX, v)
	}
	return out, rows.Err()
}
