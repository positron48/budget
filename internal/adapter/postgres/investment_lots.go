package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

type lotCursor struct {
	Fingerprint string
	Offset      int
}

func (r *MarketDataRepo) InvestmentLots(ctx context.Context, tenant, actor, key, date, instrument, cursor string, selected []string) (nav.LotDetails, error) {
	out := nav.LotDetails{Date: date}
	if strings.TrimSpace(instrument) != instrument || instrument == "" || len(instrument) > 200 || strings.ContainsAny(instrument, "\x00\r\n") || len(cursor) > 512 {
		return out, d.ErrInvestmentInvalid
	}
	page := lotCursor{}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &page) != nil || page.Offset < 1 || page.Offset > 10000 || len(page.Fingerprint) != 64 {
			return out, d.ErrInvestmentInvalid
		}
	}
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
	at, e := nav.DayCutoff(date)
	if e != nil || date < start || date > end || at.After(time.Now()) {
		return out, d.ErrInvestmentInvalid
	}
	groups, e := readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	normalized, e := nav.NormalizeGroupSelection(selected, groups)
	if e != nil {
		return out, e
	}
	allowed := instrumentGroupLedgers(q.LedgerIDs, instrument, groups, normalized)
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	policy, points, fxErr := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, at, time.Time{})
	if fxErr != nil && !errors.Is(fxErr, md.ErrNotFound) {
		return out, fxErr
	}
	converter := nav.LotCostConverter{Policy: q.FXPolicyKey}
	if fxErr == nil {
		converter.Policy = md.FXPolicyRef(policy.Key, policy.Version)
	}
	for _, p := range points {
		converter.Fixings = append(converter.Fixings, p.Fixing())
	}
	revisions := []any{}

	for _, id := range allowed {
		ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id))
		if e != nil {
			return out, e
		}
		revisions = append(revisions, []any{id, ledger.Version})
		if at.Before(ledger.StartedAt) {
			continue
		}
		ops, e := loadInvestmentOperations(ctx, tx, tenant, id)
		if e != nil {
			return out, e
		}
		replay, e := replayInvestmentLedgerTx(ctx, tx, tenant, ledger, ops, at)
		if e != nil {
			return out, e
		}
		origins := map[string]d.InvestmentOperation{}
		for _, o := range ops {
			origins[o.ID] = o
		}
		currency := ""
		for _, p := range replay.Snapshot.Positions {
			if p.InstrumentKey == instrument {
				currency = p.Currency
				break
			}
		}
		for _, lot := range replay.ExactLots[instrument] {
			if len(out.Rows) >= 10000 {
				return out, d.ErrInvestmentLimit
			}
			out.Rows = append(out.Rows, nav.LotView{LedgerID: id, LedgerName: ledger.Name, Currency: func() string {
				if lot.Currency != "" {
					return lot.Currency
				}
				return currency
			}(), Lot: lot, Origin: origins[lot.OriginID]})
		}
	}
	// Keep the exact replay FIFO order within each account, including tied
	// timestamps. UUID order is not acquisition order.
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].LedgerID < out.Rows[j].LedgerID })
	// Include the actual FX evidence, including corrections, in the page fence.
	generation, e := investmentGenerationTx(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	fingerprint := investmentHash([]any{tenant, actor, key, date, instrument, normalized, groups.Version, revisions, policy, points, generation})
	if cursor != "" && page.Fingerprint != fingerprint {
		return out, d.ErrInvestmentConflict
	}
	out.Total = len(out.Rows)
	if page.Offset > out.Total {
		return out, d.ErrInvestmentInvalid
	}
	endOffset := page.Offset + 50
	if endOffset > out.Total {
		endOffset = out.Total
	}
	if endOffset < out.Total {
		raw, _ := json.Marshal(lotCursor{fingerprint, endOffset})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	out.Rows = out.Rows[page.Offset:endOffset]
	originIDs := []string{}
	seedIDs := []string{}
	for _, row := range out.Rows {
		if !slices.Contains(originIDs, row.Lot.OriginID) {
			originIDs = append(originIDs, row.Lot.OriginID)
		}
		if !slices.Contains(seedIDs, row.LedgerID) {
			seedIDs = append(seedIDs, row.LedgerID)
		}
	}
	traces := []inv.LotChange{}
	if len(seedIDs) > 0 {
		traced, e := readConnectedInvestmentReplay(ctx, tx, tenant, seedIDs, nil, at, originIDs)
		if e != nil {
			return out, e
		}
		ledgerIDs := []string{}
		for id := range traced.Ledgers {
			ledgerIDs = append(ledgerIDs, id)
		}
		sort.Strings(ledgerIDs)
		for _, id := range ledgerIDs {
			traces = append(traces, traced.Ledgers[id].LotHistory...)
		}
		slices.SortStableFunc(traces, func(a, b inv.LotChange) int {
			if c := a.At.Compare(b.At); c != 0 {
				return c
			}
			if c := strings.Compare(a.LedgerID, b.LedgerID); c != 0 {
				return c
			}
			if a.Sequence < b.Sequence {
				return -1
			}
			if a.Sequence > b.Sequence {
				return 1
			}
			return 0
		})
	}
	traceCount := 0

	for i := range out.Rows {
		row := &out.Rows[i]
		row.HistoricalCost, e = converter.Convert([]inv.RemainingLot{row.Lot}, row.Currency, q.Currency)
		if e != nil {
			return out, e
		}
		row.FX, row.FXAt, e = converter.AcquisitionFX(row.Lot, row.Currency, q.Currency)
		if e != nil {
			return out, e
		}
		if row.Origin.ID == "" {
			row.Origin, e = scanInvestmentOperation(tx.QueryRow(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=$2 AND id=$3`, tenant, row.Lot.OriginLedgerID, row.Lot.OriginID))
			if e != nil {
				return out, e
			}
		}
		for _, change := range traces {
			lot := change.After
			if lot == nil {
				lot = change.Before
			}
			if lot.OriginID == row.Lot.OriginID {
				traceCount++
				if traceCount > 10000 {
					return out, d.ErrInvestmentLimit
				}
				row.Changes = append(row.Changes, change)
			}
		}
	}
	return out, tx.Commit(ctx)
}
