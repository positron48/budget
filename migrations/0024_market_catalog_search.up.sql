ALTER TABLE market_data_jobs ADD COLUMN IF NOT EXISTS search_query text NOT NULL DEFAULT '';
ALTER TABLE market_data_jobs ADD COLUMN IF NOT EXISTS instrument_kind text NOT NULL DEFAULT '';
ALTER TABLE market_data_jobs ALTER COLUMN instrument_id DROP NOT NULL;
ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle','catalog_search'));
ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_target_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_target_check CHECK(
 (kind='catalog_search' AND instrument_id IS NULL AND length(search_query) BETWEEN 1 AND 100 AND instrument_kind IN ('share','bond','etf','currency')) OR
 (kind<>'catalog_search' AND instrument_id IS NOT NULL AND search_query='' AND instrument_kind='')
);
DROP INDEX IF EXISTS market_data_jobs_active_request;
CREATE UNIQUE INDEX market_data_jobs_active_request ON market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at,search_query,instrument_kind) NULLS NOT DISTINCT WHERE status IN ('pending','running');
ALTER TABLE market_data_coverage ADD COLUMN IF NOT EXISTS instrument_count integer NOT NULL DEFAULT 0 CHECK(instrument_count>=0);
ALTER TABLE market_data_coverage DROP CONSTRAINT IF EXISTS market_data_coverage_state_check;
ALTER TABLE market_data_coverage ADD CONSTRAINT market_data_coverage_state_check CHECK(state IN ('observed','empty_unexplained','catalog_candidates','catalog_truncated'));
