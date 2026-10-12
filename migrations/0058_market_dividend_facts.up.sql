ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle','catalog_search','bond_facts','dividend_facts'));
ALTER TABLE market_data_subscriptions DROP CONSTRAINT IF EXISTS market_data_subscriptions_kind_check;
ALTER TABLE market_data_subscriptions ADD CONSTRAINT market_data_subscriptions_kind_check CHECK(kind IN ('last_price','daily_candle','bond_facts','dividend_facts'));
CREATE TABLE IF NOT EXISTS market_dividend_snapshots (
 tenant_id uuid NOT NULL,
 job_id uuid NOT NULL,
 provider text NOT NULL CHECK(provider='tinvest'),
 access_scope text NOT NULL,
 instrument_id uuid NOT NULL,
 from_at timestamptz NOT NULL,
 to_at timestamptz NOT NULL CHECK(to_at>from_at),
 data jsonb NOT NULL CHECK(jsonb_typeof(data)='object'),
 payload_hash text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,job_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES market_data_jobs(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS market_dividend_snapshots_lookup ON market_dividend_snapshots(tenant_id,provider,access_scope,instrument_id,recorded_at DESC,job_id DESC);
CREATE OR REPLACE FUNCTION reject_market_dividend_snapshot_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'dividend source snapshots are immutable' USING ERRCODE='55000';
END $$;
DROP TRIGGER IF EXISTS immutable_market_dividend_snapshot ON market_dividend_snapshots;
CREATE TRIGGER immutable_market_dividend_snapshot BEFORE UPDATE OR DELETE ON market_dividend_snapshots FOR EACH ROW EXECUTE FUNCTION reject_market_dividend_snapshot_change();
