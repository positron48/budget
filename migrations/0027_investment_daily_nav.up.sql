CREATE TABLE IF NOT EXISTS investment_valuation_generations (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 generation bigint NOT NULL CHECK(generation>0)
);
INSERT INTO investment_valuation_generations(tenant_id,generation) SELECT id,1 FROM tenants ON CONFLICT DO NOTHING;
DO $guard$
BEGIN
 IF to_regprocedure('bump_investment_valuation_generation()') IS NULL THEN
  EXECUTE $function$
CREATE FUNCTION bump_investment_valuation_generation() RETURNS trigger LANGUAGE plpgsql AS $body$
DECLARE scope uuid;
BEGIN
 IF TG_OP='DELETE' THEN scope=OLD.tenant_id; ELSE scope=NEW.tenant_id; END IF;
 INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(scope,1)
 ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $body$;
$function$;
 END IF;
END
$guard$;
-- Transactional generation fencing avoids losing invalidations due to commit
-- order differing from identity sequence order in the durable event feeds.
DO $migration$
DECLARE tab text;
BEGIN
 FOREACH tab IN ARRAY ARRAY['investment_ledgers','market_provider_observations','investment_market_mappings','market_data_sources','investment_fx_policies','investment_fx_points'] LOOP
  IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='nav_generation_'||tab AND tgrelid=tab::regclass) THEN
   EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION bump_investment_valuation_generation()', 'nav_generation_'||tab,tab);
  END IF;
 END LOOP;
END
$migration$;
CREATE TABLE IF NOT EXISTS investment_nav_series (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 series_key text NOT NULL CHECK(series_key ~ '^[a-f0-9]{64}$'),
 definition jsonb NOT NULL,
 PRIMARY KEY(tenant_id,series_key)
);
CREATE TABLE IF NOT EXISTS investment_daily_nav (
 tenant_id uuid NOT NULL,
 series_key text NOT NULL,
 report_date date NOT NULL,
 generation bigint NOT NULL CHECK(generation>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 computed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,series_key,report_date),
 FOREIGN KEY(tenant_id,series_key) REFERENCES investment_nav_series(tenant_id,series_key)
);
CREATE TABLE IF NOT EXISTS investment_daily_nav_revisions (
 tenant_id uuid NOT NULL,
 series_key text NOT NULL,
 report_date date NOT NULL,
 generation bigint NOT NULL CHECK(generation>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 computed_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,series_key,report_date,generation),
 FOREIGN KEY(tenant_id,series_key,report_date) REFERENCES investment_daily_nav(tenant_id,series_key,report_date)
);
