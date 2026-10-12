ALTER TABLE market_data_jobs DROP CONSTRAINT IF EXISTS market_data_jobs_kind_check;
ALTER TABLE market_data_jobs ADD CONSTRAINT market_data_jobs_kind_check CHECK(kind IN ('last_price','daily_candle','catalog_search','bond_facts'));
CREATE TABLE IF NOT EXISTS market_bond_snapshots (
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
CREATE INDEX IF NOT EXISTS market_bond_snapshots_lookup ON market_bond_snapshots(tenant_id,provider,access_scope,instrument_id,recorded_at DESC,job_id DESC);
-- Immutable source revisions. Corrected jobs append snapshots rather than
-- rewriting a previous response. Publication is fenced by the worker lease.
CREATE OR REPLACE FUNCTION reject_market_bond_snapshot_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'bond source snapshots are immutable' USING ERRCODE='55000';
END $$;
DROP TRIGGER IF EXISTS immutable_market_bond_snapshot ON market_bond_snapshots;
CREATE TRIGGER immutable_market_bond_snapshot BEFORE UPDATE OR DELETE ON market_bond_snapshots FOR EACH ROW EXECUTE FUNCTION reject_market_bond_snapshot_change();
