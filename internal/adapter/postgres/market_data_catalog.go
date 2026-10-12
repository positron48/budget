package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	md "github.com/positron48/budget/internal/usecase/marketdata"
)

func saveMarketInstrument(ctx context.Context, tx pgx.Tx, s md.Scope, i md.Instrument) (md.SavedInstrument, error) {
	out := md.SavedInstrument{Instrument: i}
	if err := md.ValidateScope(s); err != nil {
		return out, err
	}
	if err := md.ValidateInstrument(i); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "market:"+s.TenantID+":"+s.Provider+":"+s.AccessScope); err != nil {
		return out, err
	}
	hash := investmentHash(i)
	raw, err := json.Marshal(i)
	if err != nil {
		return out, err
	}
	var oldHash string
	var oldData []byte
	err = tx.QueryRow(ctx, `SELECT payload_hash,revision,received_at,data FROM market_provider_instruments WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 FOR UPDATE`, s.TenantID, s.Provider, s.AccessScope, i.ID).Scan(&oldHash, &out.Revision, &out.ReceivedAt, &oldData)
	if err == nil && !i.Detailed {
		var previous md.Instrument
		if err = json.Unmarshal(oldData, &previous); err != nil {
			return out, err
		}
		if previous.Detailed {
			out.Instrument = previous
			return out, nil
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO market_provider_instruments(tenant_id,provider,access_scope,instrument_id,data,payload_hash,revision) VALUES($1,$2,$3,$4,$5,$6,1) RETURNING revision,received_at`, s.TenantID, s.Provider, s.AccessScope, i.ID, raw, hash).Scan(&out.Revision, &out.ReceivedAt)
	} else if err == nil && oldHash != hash {
		err = tx.QueryRow(ctx, `UPDATE market_provider_instruments SET data=$5,payload_hash=$6,revision=revision+1,received_at=clock_timestamp() WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND instrument_id=$4 RETURNING revision,received_at`, s.TenantID, s.Provider, s.AccessScope, i.ID, raw, hash).Scan(&out.Revision, &out.ReceivedAt)
	}
	if err != nil {
		return out, err
	}
	if oldHash != hash {
		_, err = tx.Exec(ctx, `INSERT INTO market_provider_instrument_revisions(tenant_id,provider,access_scope,instrument_id,revision,data,received_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, s.TenantID, s.Provider, s.AccessScope, i.ID, out.Revision, raw, out.ReceivedAt)
	}
	return out, err
}
func (r *MarketDataRepo) SearchCatalog(ctx context.Context, s md.Scope, query string, limit, offset int) ([]md.SavedInstrument, error) {
	if err := md.ValidateScope(s); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if len(query) > 100 || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return nil, md.ErrInvalid
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	rows, err := r.pool.DB.Query(ctx, `SELECT data,revision,received_at FROM market_provider_instruments WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND (data->>'ticker' ILIKE $4 OR data->>'isin' ILIKE $4 OR data->>'figi' ILIKE $4 OR data->>'name' ILIKE $4 OR instrument_id::text=$5) ORDER BY data->>'ticker',data->>'class_code',instrument_id LIMIT $6 OFFSET $7`, s.TenantID, s.Provider, s.AccessScope, pattern, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []md.SavedInstrument{}
	for rows.Next() {
		var i md.SavedInstrument
		var raw []byte
		if err = rows.Scan(&raw, &i.Revision, &i.ReceivedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &i.Instrument); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *MarketDataRepo) CompleteCatalog(ctx context.Context, j md.Job, result md.CatalogResult) error {
	if err := validateJobLease(j); err != nil {
		return err
	}
	if len(result.Instruments) > 1000 || (result.Truncated && len(result.Instruments) == 0) {
		return md.ErrInvalid
	}
	tx, err := r.pool.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := scanMarketJob(tx.QueryRow(ctx, `SELECT `+marketJobColumns+` FROM market_data_jobs WHERE tenant_id=$1 AND provider=$2 AND access_scope=$3 AND id=$4 AND lease_token=$5 AND status='running' AND lease_until>clock_timestamp() FOR UPDATE`, j.Scope.TenantID, j.Scope.Provider, j.Scope.AccessScope, j.ID, j.LeaseToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return md.ErrLeaseLost
	}
	if err != nil {
		return err
	}
	if current.Kind != "catalog_search" {
		return md.ErrInvalid
	}
	seen := map[string]bool{}
	for _, i := range result.Instruments {
		if i.Kind != current.InstrumentKind || seen[i.ID] {
			return md.ErrInvalid
		}
		seen[i.ID] = true
		if _, err = saveMarketInstrument(ctx, tx, current.Scope, i); err != nil {
			return err
		}
	}
	state := "catalog_candidates"
	if len(result.Instruments) == 0 {
		state = "empty_unexplained"
	}
	if result.Truncated {
		state = "catalog_truncated"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO market_data_coverage(tenant_id,job_id,state,observation_count,instrument_count) VALUES($1,$2,$3,0,$4)`, j.Scope.TenantID, j.ID, state, len(result.Instruments)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE market_data_jobs SET status='ready',lease_token=NULL,lease_until=NULL,error_code='',finished_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, j.Scope.TenantID, j.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
