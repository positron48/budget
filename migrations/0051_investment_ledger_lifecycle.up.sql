ALTER TABLE investment_ledgers
 ADD COLUMN IF NOT EXISTS broker TEXT NOT NULL DEFAULT '' CHECK(length(broker)<=200),
 ADD COLUMN IF NOT EXISTS account_type TEXT NOT NULL DEFAULT 'other' CHECK(account_type IN ('regular','iis','other')),
 ADD COLUMN IF NOT EXISTS comment TEXT NOT NULL DEFAULT '' CHECK(length(comment)<=2000),
 ADD COLUMN IF NOT EXISTS default_currency TEXT NOT NULL DEFAULT 'RUB' CHECK(default_currency IN ('RUB','USD','EUR')),
 ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT false,
 ADD COLUMN IF NOT EXISTS metadata_version BIGINT NOT NULL DEFAULT 1 CHECK(metadata_version>0);
CREATE TABLE IF NOT EXISTS investment_ledger_revisions (
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 version BIGINT NOT NULL CHECK(version>0),
 action TEXT NOT NULL CHECK(action IN ('create','baseline','edit','archive','restore')),
 actor_id UUID NOT NULL REFERENCES users(id),
 reason TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 before_data JSONB,
 after_data JSONB NOT NULL,
 PRIMARY KEY(tenant_id,ledger_id,version),
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id)
);
INSERT INTO investment_ledger_revisions(tenant_id,ledger_id,version,action,actor_id,reason,after_data)
 SELECT tenant_id,id,1,'baseline',created_by,'Metadata lifecycle migration',to_jsonb(l) FROM investment_ledgers l ON CONFLICT(tenant_id,ledger_id,version) DO NOTHING;
