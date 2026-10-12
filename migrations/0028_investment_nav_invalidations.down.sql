DO $migration$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_daily_nav_revisions) THEN
  RAISE EXCEPTION 'Refusing to discard invalidation evidence for stored NAV' USING ERRCODE='55000';
 END IF;
END
$migration$;
DROP TRIGGER IF EXISTS nav_generation_investment_operations ON investment_operations;
CREATE OR REPLACE FUNCTION bump_investment_valuation_generation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE scope uuid;
BEGIN
 IF TG_OP='DELETE' THEN scope=OLD.tenant_id; ELSE scope=NEW.tenant_id; END IF;
 INSERT INTO investment_valuation_generations(tenant_id,generation) VALUES(scope,1)
 ON CONFLICT(tenant_id) DO UPDATE SET generation=investment_valuation_generations.generation+1;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
DROP TABLE IF EXISTS investment_nav_invalidations;
