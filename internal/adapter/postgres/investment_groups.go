package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
)

func groupAccess(ctx context.Context, tx pgx.Tx, tenant, actor string, write bool) error {
	var role string
	e := tx.QueryRow(ctx, `SELECT role FROM user_tenants WHERE tenant_id=$1 AND user_id=$2 FOR SHARE`, tenant, actor).Scan(&role)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (role != "owner" && role != "admin" && (write || role != "member")) {
		return d.ErrInvestmentForbidden
	}
	return e
}
func readInstrumentGroups(ctx context.Context, tx pgx.Tx, tenant string) (nav.InstrumentGroups, error) {
	var out nav.InstrumentGroups
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT data FROM investment_groups WHERE tenant_id=$1`, tenant).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(raw, &out)
	return out, e
}
func (r *MarketDataRepo) GetInstrumentGroups(ctx context.Context, tenant, actor string) (nav.InstrumentGroups, []nav.GroupPosition, error) {
	var out nav.InstrumentGroups
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, nil, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, nil, e
	}
	out, e = readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, nil, e
	}
	rows, e := tx.Query(ctx, `SELECT s.ledger_id::text,l.name,s.instrument_key FROM investment_security_legs s JOIN investment_ledgers l ON l.tenant_id=s.tenant_id AND l.id=s.ledger_id WHERE s.tenant_id=$1
 UNION SELECT m->>'LedgerID',l.name,m->>'InstrumentKey' FROM investment_groups g CROSS JOIN LATERAL jsonb_array_elements(g.data->'Memberships') m JOIN investment_ledgers l ON l.tenant_id=g.tenant_id AND l.id=(m->>'LedgerID')::uuid WHERE g.tenant_id=$1 ORDER BY 1,3 LIMIT 10001`, tenant)
	if e != nil {
		return out, nil, e
	}
	defer rows.Close()
	positions := []nav.GroupPosition{}
	for rows.Next() {
		var p nav.GroupPosition
		if e = rows.Scan(&p.LedgerID, &p.LedgerName, &p.InstrumentKey); e != nil {
			return out, nil, e
		}
		positions = append(positions, p)
	}
	if e = rows.Err(); e != nil {
		return out, nil, e
	}
	if len(positions) > 10000 {
		return out, nil, d.ErrInvestmentLimit
	}
	return out, positions, tx.Commit(ctx)
}
func (r *MarketDataRepo) SaveInstrumentGroups(ctx context.Context, tenant, actor string, input nav.SaveInstrumentGroups) (nav.InstrumentGroups, error) {
	var out nav.InstrumentGroups
	p, e := nav.NormalizeInstrumentGroups(input)
	if e != nil {
		return out, e
	}
	tx, e := r.pool.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, true); e != nil {
		return out, e
	}
	if e = lockInvestment(ctx, tx, tenant); e != nil {
		return out, e
	}
	hash := investmentHash(p)
	var oldHash string
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT payload_hash,response FROM investment_group_requests WHERE tenant_id=$1 AND actor_id=$2 AND request_key=$3`, tenant, actor, p.RequestKey).Scan(&oldHash, &raw)
	if e == nil {
		if oldHash != hash {
			return out, d.ErrInvestmentConflict
		}
		e = json.Unmarshal(raw, &out)
		return out, e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	old, e := readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	if old.Version != p.Version {
		return out, d.ErrInvestmentConflict
	}
	// Memberships survive journal corrections and closed positions. Account
	// ownership is mandatory; instrument keys remain analytical identifiers.
	if e = validateGroupAccounts(ctx, tx, tenant, p.Memberships); e != nil {
		return out, e
	}

	out = nav.InstrumentGroups{Version: old.Version + 1, Groups: p.Groups, Memberships: p.Memberships}
	raw, e = json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_groups(tenant_id,version,data) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET version=EXCLUDED.version,data=EXCLUDED.data`, tenant, out.Version, raw); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_group_revisions(tenant_id,version,actor_id,reason,data) VALUES($1,$2,$3,$4,$5)`, tenant, out.Version, actor, p.Reason, raw); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO investment_group_requests(tenant_id,actor_id,request_key,payload_hash,response) VALUES($1,$2,$3,$4,$5)`, tenant, actor, p.RequestKey, hash, raw); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func validateGroupAccounts(ctx context.Context, tx pgx.Tx, tenant string, memberships []nav.GroupMembership) error {
	ids := []string{}
	seen := map[string]bool{}
	for _, m := range memberships {
		if !seen[m.LedgerID] {
			ids = append(ids, m.LedgerID)
			seen[m.LedgerID] = true
		}
	}
	var count int
	e := tx.QueryRow(ctx, `SELECT count(*) FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[])`, tenant, ids).Scan(&count)
	if e != nil {
		return e
	}
	if count != len(ids) {
		return d.ErrInvestmentPrecondition
	}
	return nil
}

// Verify current classification and every audit version before accepting an
// archive. A checksum alone cannot establish tenant ownership of JSON members.
func verifyArchiveGroups(ctx context.Context, tx pgx.Tx, tenant string) error {
	current, e := readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return d.ErrInvestmentPrecondition
	}
	rows, e := tx.Query(ctx, `SELECT version,reason,data FROM investment_group_revisions WHERE tenant_id=$1 ORDER BY version`, tenant)
	if e != nil {
		return e
	}
	type revision struct {
		version int64
		reason  string
		raw     []byte
	}
	revisions := []revision{}
	for rows.Next() {
		var v revision
		if e = rows.Scan(&v.version, &v.reason, &v.raw); e != nil {
			rows.Close()
			return e
		}
		revisions = append(revisions, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if int64(len(revisions)) != current.Version {
		return d.ErrInvestmentPrecondition
	}
	for i, v := range revisions {
		var p nav.InstrumentGroups
		if e = json.Unmarshal(v.raw, &p); e != nil || v.version != int64(i+1) || p.Version != v.version {
			return d.ErrInvestmentPrecondition
		}
		normalized, e := nav.NormalizeInstrumentGroups(nav.SaveInstrumentGroups{InstrumentGroups: p, RequestKey: "archive", Reason: v.reason})
		if e != nil || investmentHash(normalized.InstrumentGroups) != investmentHash(p) {
			return d.ErrInvestmentPrecondition
		}
		if e = validateGroupAccounts(ctx, tx, tenant, p.Memberships); e != nil {
			return e
		}
		if i == len(revisions)-1 && investmentHash(current) != investmentHash(p) {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}

// Read groups and cached historical valuations from one snapshot. Reclassification
// changes the projection at read time and does not enqueue financial replay.
func (r *MarketDataRepo) InvestmentCompositionHistory(ctx context.Context, tenant, actor, key, from, to string, selected []string, dimension string) (nav.CompositionHistory, error) {
	var out nav.CompositionHistory
	dimension, e := nav.NormalizeCompositionDimension(dimension)
	if e != nil {
		return out, e
	}
	out.BreakdownBy = dimension
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if e = groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, e
	}
	q, start, end, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, e
	}
	if _, e = nav.DayCutoff(from); e != nil {
		return out, d.ErrInvestmentInvalid
	}
	if _, e = nav.DayCutoff(to); e != nil || from < start || to > end || to < from {
		return out, d.ErrInvestmentInvalid
	}
	out.Classification, e = readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	selected, e = nav.NormalizeGroupSelection(selected, out.Classification)
	if e != nil {
		return out, e
	}
	days, e := readDailyNAVHistory(ctx, tx, q, from, to)
	if e != nil {
		return out, e
	}
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	for _, day := range days {
		point := nav.CompositionDay{DailyNAV: day}
		if (day.State == "ready" || day.State == "incomplete") && day.Result != nil {
			c, e := nav.ComposeComposition(*day.Result, out.Classification, selected, dimension)
			if e != nil {
				return out, e
			}
			point.Composition = &c
		}
		out.Days = append(out.Days, point)
	}
	return out, tx.Commit(ctx)
}
