DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM market_data_jobs WHERE kind='catalog_search') THEN
  RAISE EXCEPTION 'Refusing to discard catalog search history' USING ERRCODE='55000';
 END IF;
END
$migration$;
ALTER TABLE market_data_jobs DROP CONSTRAINT market_data_jobs_target_check;
ALTER TABLE market_data_jobs DROP CONSTRAINT market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle'));
DROP INDEX market_data_jobs_active_request;
ALTER TABLE market_data_jobs DROP COLUMN search_query;
ALTER TABLE market_data_jobs DROP COLUMN instrument_kind;
ALTER TABLE market_data_jobs ALTER COLUMN instrument_id SET NOT NULL;
CREATE UNIQUE INDEX market_data_jobs_active_request ON market_data_jobs(tenant_id,provider,access_scope,instrument_id,kind,from_at,to_at) WHERE status IN ('pending','running');
ALTER TABLE market_data_coverage DROP CONSTRAINT market_data_coverage_state_check;
ALTER TABLE market_data_coverage ADD CONSTRAINT market_data_coverage_state_check CHECK(state IN ('observed','empty_unexplained'));
ALTER TABLE market_data_coverage DROP COLUMN instrument_count;
