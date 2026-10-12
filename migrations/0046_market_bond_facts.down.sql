DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM market_bond_snapshots) OR EXISTS(SELECT 1 FROM market_data_jobs WHERE kind='bond_facts') THEN
  RAISE EXCEPTION 'bond facts contain source data or jobs' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS market_bond_snapshots;
DROP FUNCTION IF EXISTS reject_market_bond_snapshot_change();
ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle','catalog_search'));
