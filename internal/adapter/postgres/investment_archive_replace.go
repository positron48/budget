package postgres

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	archive "github.com/positron48/budget/internal/usecase/investmentarchive"
)

type PortfolioArchiveReplacement struct {
	ExpectedStateSHA256   string
	ExpectedArchiveSHA256 string
	AlreadyApplied        bool
}

// InvestmentArchiveStateSHA256 binds canonical investment facts, independent
// of export time and row order. It excludes ordinary Budget and licensed caches.
func InvestmentArchiveStateSHA256(bundle archive.Bundle) (string, error) {
	sealed, e := archive.Seal(bundle.Data)
	if e != nil || bundle.Format != archive.Format || sealed.SHA256 != bundle.SHA256 {
		return "", d.ErrInvestmentPrecondition
	}
	data := bundle.Data
	data.CreatedAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	data.Tables = append([]archive.Table(nil), data.Tables...)
	data.Actors = append([]string(nil), data.Actors...)
	for i := range data.Tables {
		data.Tables[i].Rows = append([]json.RawMessage(nil), data.Tables[i].Rows...)
		for j, raw := range data.Tables[i].Rows {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			var value any
			if e = decoder.Decode(&value); e != nil {
				return "", e
			}
			data.Tables[i].Rows[j], e = json.Marshal(value)
			if e != nil {
				return "", e
			}
		}
	}
	archive.Canonicalize(&data)
	sealed, e = archive.Seal(data)
	return sealed.SHA256, e
}
func validArchiveSHA(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}

// ReplacePortfolioArchive is operator-only, not exposed as a web RPC. Every
// existing account ID/name must survive; household-connected cash books refuse.
// Dry-run performs the same deletes/restoration then rolls the transaction back.
func (r *InvestmentRepo) ReplacePortfolioArchive(ctx context.Context, actor string, bundle archive.Bundle, options PortfolioArchiveReplacement, apply bool) (archive.JournalVerification, bool, error) {
	if !validArchiveSHA(options.ExpectedStateSHA256) || options.ExpectedArchiveSHA256 != bundle.SHA256 {
		return archive.JournalVerification{}, false, d.ErrInvestmentPrecondition
	}
	result, e := r.restoreInvestmentArchiveWithReplacement(ctx, actor, bundle, apply, &options)
	return result, options.AlreadyApplied, e
}

func samePortfolioAccounts(before, after archive.Dataset) bool {
	accounts := func(data archive.Dataset) (map[string]string, bool) {
		out := map[string]string{}
		for _, table := range data.Tables {
			if table.Name != "investment_ledgers" {
				continue
			}
			for _, raw := range table.Rows {
				var row struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					Tracking bool   `json:"portfolio_tracking"`
				}
				if json.Unmarshal(raw, &row) != nil || !row.Tracking || row.ID == "" {
					return nil, false
				}
				out[row.ID] = row.Name
			}
		}
		return out, len(out) > 0
	}
	a, ok := accounts(before)
	if !ok {
		return false
	}
	b, ok := accounts(after)
	if !ok || len(a) != len(b) {
		return false
	}
	for id, name := range a {
		if b[id] != name {
			return false
		}
	}
	return true
}

// Canonical settings use several distinct advisory lock namespaces. Fence their
// tables together so a concurrent policy/group edit cannot slip between the
// reviewed state check and replacement. Reads remain available; waits are bounded.
func lockPortfolioArchiveTables(ctx context.Context, tx pgx.Tx) error {
	if _, e := tx.Exec(ctx, `SET LOCAL lock_timeout='10s'`); e != nil {
		return e
	}
	names := append([]string(nil), archive.Included...)
	for name := range archive.Excluded {
		names = append(names, name)
	}
	sort.Strings(names)
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = pgx.Identifier{"public", name}.Sanitize()
	}
	_, e := tx.Exec(ctx, "LOCK TABLE "+strings.Join(quoted, ",")+" IN SHARE ROW EXCLUSIVE MODE")
	return e
}

// clearPortfolioArchiveTx only deletes from the fixed archive classification.
// Actual FK dependencies determine child-first order; no CASCADE or TRUNCATE.
func clearPortfolioArchiveTx(ctx context.Context, tx pgx.Tx, tenant string) error {
	for _, name := range []string{"investment_asset_activations", "investment_cash_links"} {
		var connected bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+pgx.Identifier{"public", name}.Sanitize()+` WHERE tenant_id=$1)`, tenant).Scan(&connected); e != nil {
			return e
		}
		if connected {
			return d.ErrInvestmentPrecondition
		}
	}
	names := append([]string(nil), archive.Included...)
	for name := range archive.Excluded {
		if name != "investment_asset_activations" && name != "investment_cash_links" && name != "investment_valuation_generations" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	remaining := map[string]bool{}
	children := map[string][]string{}
	for _, name := range names {
		remaining[name] = true
	}
	rows, e := tx.Query(ctx, `SELECT child.relname,parent.relname FROM pg_constraint c JOIN pg_class child ON child.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=child.relnamespace JOIN pg_class parent ON parent.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=parent.relnamespace WHERE c.contype='f' AND cn.nspname='public' AND pn.nspname='public' AND child.relname=ANY($1::text[]) AND parent.relname=ANY($1::text[])`, names)
	if e != nil {
		return e
	}
	for rows.Next() {
		var child, parent string
		if e = rows.Scan(&child, &parent); e != nil {
			rows.Close()
			return e
		}
		if child != parent {
			children[parent] = append(children[parent], child)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for len(remaining) > 0 {
		progressed := false
		for _, name := range names {
			if !remaining[name] {
				continue
			}
			ready := true
			for _, child := range children[name] {
				if remaining[child] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			predicate := "tenant_id=$1"
			if name == "financial_write_requests" {
				predicate += " AND starts_with(operation,'investment.')"
			}
			if _, e = tx.Exec(ctx, `DELETE FROM `+pgx.Identifier{"public", name}.Sanitize()+` WHERE `+predicate, tenant); e != nil {
				return e
			}
			delete(remaining, name)
			progressed = true
		}
		if !progressed {
			return d.ErrInvestmentPrecondition
		}
	}
	return nil
}
