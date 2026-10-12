CREATE TABLE IF NOT EXISTS investment_nav_invalidations (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 generation bigint NOT NULL CHECK(generation>0),
 affected_from timestamptz,
 source_table text NOT NULL,
 PRIMARY KEY(tenant_id,generation)
);
-- A conservative checkpoint covers generations that predate this feed. Do not
-- overwrite a real event when migrations are replayed on an existing database.
INSERT INTO investment_nav_invalidations(tenant_id,generation,source_table)
SELECT tenant_id,generation,'migration_checkpoint' FROM investment_valuation_generations
ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION bump_investment_valuation_generation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE scope uuid; gen bigint; dirty timestamptz; before_row jsonb; after_row jsonb;
BEGIN
 IF TG_OP<>'INSERT' THEN before_row=to_jsonb(OLD); END IF;
 IF TG_OP<>'DELETE' THEN after_row=to_jsonb(NEW); END IF;
 scope=COALESCE((after_row->>'tenant_id')::uuid,(before_row->>'tenant_id')::uuid);
 CASE TG_TABLE_NAME
 WHEN 'investment_ledgers' THEN
  -- Operation rows carry exact dirty dates. A revision/name-only ledger update
  -- need not invalidate dates before the changed operation.
  IF TG_OP='UPDATE' AND after_row->>'started_at'=before_row->>'started_at' THEN RETURN NEW; END IF;
  dirty=LEAST((before_row->>'started_at')::timestamptz,(after_row->>'started_at')::timestamptz);
 WHEN 'investment_operations' THEN
  dirty=LEAST((before_row->>'occurred_at')::timestamptz,(after_row->>'occurred_at')::timestamptz);
 WHEN 'market_provider_observations' THEN
  dirty=LEAST((before_row->>'observed_at')::timestamptz,(after_row->>'observed_at')::timestamptz);
 WHEN 'investment_fx_points' THEN
  dirty=LEAST((before_row->>'effective_at')::timestamptz,(after_row->>'effective_at')::timestamptz);
 WHEN 'investment_market_mappings' THEN
  SELECT min((value->>'From')::timestamptz) INTO dirty FROM jsonb_array_elements(
   COALESCE(NULLIF(before_row->'data'->'Periods','null'::jsonb),'[]'::jsonb) ||
   COALESCE(NULLIF(after_row->'data'->'Periods','null'::jsonb),'[]'::jsonb));
 ELSE dirty=NULL; -- Source entitlement and convention changes affect all dates.
 END CASE;
 INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(scope,1)
 ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1
 RETURNING generation INTO gen;
 INSERT INTO investment_nav_invalidations(tenant_id,generation,affected_from,source_table)
 VALUES(scope,gen,dirty,TG_TABLE_NAME);
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
DO $migration$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='nav_generation_investment_operations' AND tgrelid='investment_operations'::regclass) THEN
  CREATE TRIGGER nav_generation_investment_operations AFTER INSERT OR UPDATE OR DELETE ON investment_operations FOR EACH ROW EXECUTE FUNCTION bump_investment_valuation_generation();
 END IF;
END
$migration$;
CREATE INDEX IF NOT EXISTS investment_nav_invalidations_dated ON investment_nav_invalidations(tenant_id,affected_from,generation) WHERE affected_from IS NOT NULL;
CREATE INDEX IF NOT EXISTS investment_nav_invalidations_global ON investment_nav_invalidations(tenant_id,generation) WHERE affected_from IS NULL;
