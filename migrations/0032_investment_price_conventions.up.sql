CREATE TABLE IF NOT EXISTS investment_price_conventions (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 profile_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 PRIMARY KEY(tenant_id,profile_key)
);
CREATE TABLE IF NOT EXISTS investment_price_convention_revisions (
 tenant_id uuid NOT NULL,
 profile_key text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 data jsonb NOT NULL,
 payload_hash text NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,profile_key,version),
 FOREIGN KEY(tenant_id,profile_key) REFERENCES investment_price_conventions(tenant_id,profile_key)
);
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='nav_generation_price_conventions' AND tgrelid='investment_price_conventions'::regclass) THEN
  CREATE TRIGGER nav_generation_price_conventions AFTER INSERT OR UPDATE OR DELETE ON investment_price_conventions FOR EACH ROW EXECUTE FUNCTION bump_investment_valuation_generation();
 END IF;
END $$;
