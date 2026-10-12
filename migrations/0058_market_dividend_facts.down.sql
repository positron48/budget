DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM market_dividend_snapshots) OR EXISTS(SELECT 1 FROM market_data_jobs WHERE kind='dividend_facts') OR EXISTS(SELECT 1 FROM market_data_subscriptions WHERE kind='dividend_facts') THEN
  RAISE EXCEPTION 'dividend facts contain source data, jobs or subscriptions' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE IF EXISTS market_dividend_snapshots;
DROP FUNCTION IF EXISTS reject_market_dividend_snapshot_change();
ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle','catalog_search','bond_facts'));
ALTER TABLE market_data_subscriptions DROP CONSTRAINT IF EXISTS market_data_subscriptions_kind_check;
ALTER TABLE market_data_subscriptions ADD CONSTRAINT market_data_subscriptions_kind_check CHECK(kind IN ('last_price','daily_candle','bond_facts'));
