package postgres

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"

	md "github.com/positron48/budget/internal/usecase/marketdata"
)

// PriceEvidence reads one entitled series at a revision knowledge cutoff.
// Zero KnowledgeAt selects latest corrected history, not simulated historical
// availability. Source enablement and revisions are checked in one SQL snapshot.
// Caller supplies a sufficient lower bound (e.g. verified calendar coverage).
func (r *MarketDataRepo) PriceEvidence(ctx context.Context, q md.PriceSelectionRequest, from time.Time) ([]md.ScopedObservation, error) {
	return readPriceEvidence(ctx, r.pool.DB, q, from)
}

type marketEvidenceReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readPriceEvidence(ctx context.Context, db marketEvidenceReader, q md.PriceSelectionRequest, from time.Time) ([]md.ScopedObservation, error) {
	return readPriceEvidenceMode(ctx, db, q, from, false)
}

// Current last-price selection needs one unique timestamp/revision, not an
// entire intraday history. The DB uniqueness key includes source/adjustment.
func readPriceEvidenceMode(ctx context.Context, db marketEvidenceReader, q md.PriceSelectionRequest, from time.Time, latest bool) ([]md.ScopedObservation, error) {
	if latest && q.Kind != "last_price" {
		return nil, md.ErrInvalid
	}
	maxHistory := 3660 * 24 * time.Hour
	if q.Kind == "daily_candle" && q.Retrospective != nil {
		// A ten-year valuation window needs the preceding UTC candle identity.
		maxHistory += 24 * time.Hour
	}
	if md.ValidateScope(q.Scope) != nil || md.ValidateRequest(md.JobRequest{InstrumentID: q.InstrumentID, Kind: "last_price"}) != nil || (q.Kind != "last_price" && q.Kind != "daily_candle") || (q.SourceKind != "exchange" && q.SourceKind != "dealer") || (q.Adjustment != "unadjusted" && q.Adjustment != "provider_unspecified") || from.IsZero() || q.At.Before(from) || q.At.Sub(from) > maxHistory {
		return nil, md.ErrInvalid
	}
	var knowledge any
	if !q.KnowledgeAt.IsZero() {
		knowledge = q.KnowledgeAt
	}
	order := " ORDER BY o.observed_at,o.id LIMIT 10001"
	if latest {
		order = " ORDER BY o.observed_at DESC,o.id DESC LIMIT 1"
	}
	rows, err := db.Query(ctx, `SELECT o.id,h.data,h.revision,h.received_at
 FROM market_provider_observations o
 JOIN market_data_sources s USING(tenant_id,provider,access_scope)
 JOIN LATERAL (
  SELECT data,revision,received_at FROM market_provider_observation_revisions
  WHERE tenant_id=o.tenant_id AND observation_id=o.id AND ($10::timestamptz IS NULL OR received_at<=$10)
  ORDER BY revision DESC LIMIT 1
 ) h ON true
 WHERE o.tenant_id=$1 AND o.provider=$2 AND o.access_scope=$3 AND o.instrument_id=$4 AND o.kind=$5
 AND o.source_kind=$6 AND o.adjustment=$7 AND o.observed_at BETWEEN $8 AND $9 AND s.enabled
 `+order, q.TenantID, q.Provider, q.AccessScope, q.InstrumentID, q.Kind, q.SourceKind, q.Adjustment, from, q.At, knowledge)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []md.ScopedObservation{}
	for rows.Next() {
		o := md.ScopedObservation{Scope: q.Scope}
		var raw []byte
		if err = rows.Scan(&o.ID, &raw, &o.Revision, &o.ReceivedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &o.Observation); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if len(out) > 10000 {
		return nil, md.ErrLimit
	}
	return out, rows.Err()
}
