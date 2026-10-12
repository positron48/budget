ALTER TABLE asset_accounts
 ADD COLUMN IF NOT EXISTS investment_mode TEXT NOT NULL DEFAULT 'manual' CHECK(investment_mode IN ('manual','ledger')),
 ADD COLUMN IF NOT EXISTS investment_ledger_id UUID,
 ADD COLUMN IF NOT EXISTS ledger_started_at TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS ledger_valuation_from TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS investment_convention_key TEXT;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='asset_accounts'::regclass AND conname='asset_investment_binding') THEN
ALTER TABLE asset_accounts ADD CONSTRAINT asset_investment_binding CHECK(
 (investment_mode='manual' AND investment_ledger_id IS NULL AND ledger_started_at IS NULL AND ledger_valuation_from IS NULL AND investment_convention_key IS NULL)
 OR (kind='investment' AND investment_mode='ledger' AND investment_ledger_id IS NOT NULL AND ledger_started_at IS NOT NULL AND ledger_valuation_from IS NOT NULL AND ledger_valuation_from>=ledger_started_at AND investment_convention_key IS NOT NULL));
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='asset_accounts'::regclass AND conname='asset_investment_ledger_fk') THEN
ALTER TABLE asset_accounts ADD CONSTRAINT asset_investment_ledger_fk FOREIGN KEY(tenant_id,investment_ledger_id) REFERENCES investment_ledgers(tenant_id,id);
 END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS asset_investment_ledger_unique ON asset_accounts(tenant_id,investment_ledger_id) WHERE investment_ledger_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS investment_asset_activations (
 tenant_id UUID NOT NULL, account_id UUID NOT NULL, ledger_id UUID NOT NULL,
 actor_id UUID NOT NULL REFERENCES users(id), activated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 valuation_from TIMESTAMPTZ NOT NULL, preview_hash TEXT NOT NULL, evidence TEXT NOT NULL,
 comparison JSONB NOT NULL,
 PRIMARY KEY(tenant_id,account_id),
 FOREIGN KEY(tenant_id,account_id) REFERENCES asset_accounts(tenant_id,id),
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id)
);
