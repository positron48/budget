package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	d "github.com/positron48/budget/internal/domain"
	inv "github.com/positron48/budget/internal/usecase/investment"
	nav "github.com/positron48/budget/internal/usecase/investmentvaluation"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func (r *MarketDataRepo) InvestmentPositions(ctx context.Context, tenant, actor, key, date string, selected []string) (nav.PositionDetails, error) {
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nav.PositionDetails{}, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out, _, e := readInvestmentPositions(ctx, tx, tenant, actor, key, date, selected)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func readInvestmentPositions(ctx context.Context, tx pgx.Tx, tenant, actor, key, date string, selected []string) (nav.PositionDetails, nav.Request, error) {
	out := nav.PositionDetails{Date: date, Metadata: map[string]md.Instrument{}}
	var q nav.Request
	if e := groupAccess(ctx, tx, tenant, actor, false); e != nil {
		return out, q, e
	}
	q, start, end, e := readNAVRegisteredRequest(ctx, tx, tenant, actor, key)
	if e != nil {
		return out, q, e
	}
	at, e := nav.DayCutoff(date)
	if e != nil || date < start || date > end || at.After(time.Now()) {
		return out, q, d.ErrInvestmentInvalid
	}
	q.At = at
	out.Currency = q.Currency
	out.FXPolicyKey = q.FXPolicyKey
	out.Classification, e = readInstrumentGroups(ctx, tx, tenant)
	if e != nil {
		return out, q, e
	}
	if _, e = nav.NormalizeGroupSelection(selected, out.Classification); e != nil {
		return out, q, e
	}
	history, e := readDailyNAVHistory(ctx, tx, q, date, date)
	if e != nil {
		return out, q, e
	}
	var value *nav.Result
	out.ValuationState = "unavailable"
	if len(history) > 0 {
		out.ValuationState = history[0].State
		if history[0].State == "ready" || history[0].State == "incomplete" {
			value = history[0].Result
		}
	}
	if value != nil && (value.Components == nil || value.ComponentValues == nil) {
		value = nil
		out.ValuationState = "unavailable"
	}
	lots := map[string][]inv.RemainingLot{}
	snapshots := []d.InvestmentSnapshot{}
	payments := []nav.PositionPayment{}
	for _, id := range q.LedgerIDs {
		ledger, e := scanLedger(tx.QueryRow(ctx, `SELECT `+ledgerColumns+` FROM investment_ledgers WHERE tenant_id=$1 AND id=$2`, tenant, id))
		if e != nil {
			return out, q, e
		}
		if at.Before(ledger.StartedAt) {
			snapshots = append(snapshots, d.InvestmentSnapshot{Ledger: ledger, AsOf: at})
			continue
		}
		ops, e := loadInvestmentOperations(ctx, tx, tenant, id)
		if e != nil {
			return out, q, e
		}
		replay, e := replayInvestmentLedgerTx(ctx, tx, tenant, ledger, ops, at)
		if e != nil {
			return out, q, e
		}
		for instrument, remaining := range replay.ExactLots {
			lots[id+"\x00"+instrument] = remaining
		}
		incoming := map[string]d.InvestmentOperation{}
		for _, op := range ops {
			if op.Status == "posted" && !op.OccurredAt.After(at) && (op.Kind == "dividend" || op.Kind == "coupon") && op.InstrumentKey != "" {
				incoming[op.ID] = op
			}
		}
		sums := map[string]*nav.PositionPayment{}
		netByOperation := map[string]*big.Rat{}
		for _, leg := range replay.CashLegs {
			if _, exists := incoming[leg.OperationID]; exists && !leg.EffectiveAt.After(at) {
				v, ok := new(big.Rat).SetString(leg.Amount)
				if !ok {
					return out, q, d.ErrInvestmentPrecondition
				}
				sum := netByOperation[leg.OperationID]
				if sum == nil {
					sum = new(big.Rat)
					netByOperation[leg.OperationID] = sum
				}
				sum.Add(sum, v)
			}
		}

		for opID, op := range incoming {
			k := op.InstrumentKey + "\x00" + op.Currency
			p := sums[k]
			if p == nil {
				p = &nav.PositionPayment{LedgerID: id, InstrumentKey: op.InstrumentKey, Currency: op.Currency, Gross: new(big.Rat), Tax: new(big.Rat), Net: new(big.Rat)}
				sums[k] = p
			}
			amount, ok := new(big.Rat).SetString(op.Amount)
			if !ok {
				return out, q, d.ErrInvestmentPrecondition
			}
			p.Gross.Add(p.Gross, amount)
			tax, ok := new(big.Rat).SetString(op.Tax)
			if !ok {
				return out, q, d.ErrInvestmentPrecondition
			}
			p.Tax.Add(p.Tax, tax)
			if net := netByOperation[opID]; net != nil {
				p.Net.Add(p.Net, net)
			}
		}
		for _, payment := range sums {
			payments = append(payments, *payment)
			found := false
			for _, p := range replay.Snapshot.Positions {
				if p.InstrumentKey == payment.InstrumentKey {
					found = true
					break
				}
			}
			if !found {
				replay.Snapshot.Positions = append(replay.Snapshot.Positions, d.InvestmentPosition{InstrumentKey: payment.InstrumentKey, Currency: payment.Currency, Quantity: "0", CleanCost: "0", AcquiredAccrued: "0", RealizedClean: "0", RealizedAccrued: "0"})
			}
		}
		snapshots = append(snapshots, replay.Snapshot)
	}
	sort.Slice(payments, func(i, j int) bool {
		a, b := payments[i], payments[j]
		if a.LedgerID != b.LedgerID {
			return a.LedgerID < b.LedgerID
		}
		if a.InstrumentKey != b.InstrumentKey {
			return a.InstrumentKey < b.InstrumentKey
		}
		return a.Currency < b.Currency
	})
	out.Rows, e = nav.PositionRows(snapshots, value, out.Classification, payments, selected)
	if e != nil {
		return out, q, e
	}
	if len(out.Rows) > 10000 {
		return out, q, d.ErrInvestmentLimit
	}
	policy, points, fxErr := readFXSnapshot(ctx, tx, tenant, q.FXPolicyKey, at, time.Time{})
	if fxErr != nil && !errors.Is(fxErr, md.ErrNotFound) {
		return out, q, fxErr
	}
	converter := nav.LotCostConverter{Policy: q.FXPolicyKey}
	if fxErr == nil {
		converter.Policy = md.FXPolicyRef(policy.Key, policy.Version)
	}
	for _, point := range points {
		converter.Fixings = append(converter.Fixings, point.Fixing())
	}
	for i := range out.Rows {
		row := &out.Rows[i]
		row.HistoricalCost, e = converter.Convert(lots[row.LedgerID+"\x00"+row.Position.InstrumentKey], row.Position.Currency, q.Currency)
		if e != nil {
			return out, q, e
		}
	}
	for _, row := range out.Rows {
		key := row.Position.InstrumentKey
		if _, exists := out.Metadata[key]; exists {
			continue
		}
		metadata, e := readPositionMetadata(ctx, tx, tenant, key, at)
		if e != nil {
			return out, q, e
		}
		out.Metadata[key] = metadata
	}
	return out, q, nil
}
func readPositionMetadata(ctx context.Context, tx pgx.Tx, tenant, key string, at time.Time) (md.Instrument, error) {
	var out md.Instrument
	var raw []byte
	var mapping md.Mapping
	e := tx.QueryRow(ctx, `SELECT data FROM investment_market_mappings WHERE tenant_id=$1 AND instrument_key=$2`, tenant, key).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(raw, &mapping); e != nil {
		return out, e
	}
	for _, p := range mapping.Periods {
		if !at.Before(p.From) && (p.To.IsZero() || at.Before(p.To)) {
			e = tx.QueryRow(ctx, `SELECT data FROM market_provider_instruments WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id::text=$4`, tenant, p.Provider, p.AccessScope, p.InstrumentID).Scan(&raw)
			if errors.Is(e, pgx.ErrNoRows) {
				return out, nil
			}
			if e != nil {
				return out, e
			}
			e = json.Unmarshal(raw, &out)
			return out, e
		}
	}
	return out, nil
}

type instrumentOperationCursor struct {
	Fingerprint  string
	At           time.Time
	LedgerID, ID string
	Sequence     int64
}

func (r *MarketDataRepo) InvestmentInstrument(ctx context.Context, tenant, actor, key, date, instrument, cursor string, selected []string) (nav.InstrumentDetails, error) {
	var out nav.InstrumentDetails
	if strings.TrimSpace(instrument) != instrument || len(instrument) > 200 || instrument == "" || strings.ContainsAny(instrument, "\x00\r\n") {
		return out, d.ErrInvestmentInvalid
	}
	var page instrumentOperationCursor
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(raw) > 512 || json.Unmarshal(raw, &page) != nil || page.At.IsZero() || page.Sequence < 1 {
			return out, d.ErrInvestmentInvalid
		}
		for _, id := range []string{page.LedgerID, page.ID} {
			v, e := uuid.Parse(id)
			if e != nil || v.String() != id {
				return out, d.ErrInvestmentInvalid
			}
		}
	}
	tx, e := r.pool.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	details, q, e := readInvestmentPositions(ctx, tx, tenant, actor, key, date, selected)
	if e != nil {
		return out, e
	}
	out.PositionDetails = details
	out.Rows = nil
	out.InstrumentKey = instrument
	out.AsOf = q.At
	if _, exists := out.Metadata[instrument]; !exists {
		out.Metadata[instrument], e = readPositionMetadata(ctx, tx, tenant, instrument, q.At)
		if e != nil {
			return out, e
		}
	}
	for _, p := range details.Rows {
		if p.Position.InstrumentKey == instrument {
			out.Rows = append(out.Rows, p)
		}
	}
	versions, e := tx.Query(ctx, `SELECT id::text,version FROM investment_ledgers WHERE tenant_id=$1 AND id=ANY($2::uuid[]) ORDER BY id`, tenant, q.LedgerIDs)
	if e != nil {
		return out, e
	}
	revision := []any{}
	for versions.Next() {
		var id string
		var version int64
		if e = versions.Scan(&id, &version); e != nil {
			versions.Close()
			return out, e
		}
		revision = append(revision, []any{id, version})
	}
	e = versions.Err()
	versions.Close()
	if e != nil {
		return out, e
	}
	normalized, _ := nav.NormalizeGroupSelection(selected, details.Classification)
	generation, e := investmentGenerationTx(ctx, tx, tenant)
	if e != nil {
		return out, e
	}
	fingerprint := investmentHash([]any{tenant, actor, key, date, instrument, normalized, details.Classification.Version, revision, generation})
	if cursor != "" && page.Fingerprint != fingerprint {
		return out, d.ErrInvestmentConflict
	}
	// Current group assignment applies to all displayed history, including closed positions.
	allowed := instrumentGroupLedgers(q.LedgerIDs, instrument, details.Classification, selected)
	rows, e := tx.Query(ctx, `SELECT data FROM investment_operations WHERE tenant_id=$1 AND ledger_id=ANY($2::uuid[]) AND data->>'InstrumentKey'=$3 AND occurred_at<=$4
 AND ($5 OR (occurred_at,ledger_id,sequence,id)<($6,$7::uuid,$8,$9::uuid)) ORDER BY occurred_at DESC,ledger_id DESC,sequence DESC,id DESC LIMIT 51`, tenant, allowed, instrument, q.At, cursor == "", page.At, emptyUUID(page.LedgerID), page.Sequence, emptyUUID(page.ID))
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var op d.InvestmentOperation
		if e = rows.Scan(&raw); e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &op); e != nil {
			return out, e
		}
		out.Operations = append(out.Operations, op)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Operations) > 50 {
		out.Operations = out.Operations[:50]
		last := out.Operations[49]
		raw, _ := json.Marshal(instrumentOperationCursor{Fingerprint: fingerprint, At: last.OccurredAt, LedgerID: last.LedgerID, Sequence: last.Sequence, ID: last.ID})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, tx.Commit(ctx)
}
func emptyUUID(value string) string {
	if value == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return value
}
