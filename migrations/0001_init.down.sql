DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS user_tenants;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;

DO $$ BEGIN DROP TYPE IF EXISTS transaction_type; EXCEPTION WHEN undefined_object THEN NULL; END $$;
DO $$ BEGIN DROP TYPE IF EXISTS category_kind; EXCEPTION WHEN undefined_object THEN NULL; END $$;
DO $$ BEGIN DROP TYPE IF EXISTS tenant_role; EXCEPTION WHEN undefined_object THEN NULL; END $$;


