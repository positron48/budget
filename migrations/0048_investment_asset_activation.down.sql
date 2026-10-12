DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_asset_activations) OR EXISTS(SELECT 1 FROM asset_accounts WHERE investment_mode='ledger') THEN
 RAISE EXCEPTION 'Investment asset activation exists; restore a coordinated backup or roll forward' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_asset_activations;
DROP INDEX asset_investment_ledger_unique;
ALTER TABLE asset_accounts DROP CONSTRAINT asset_investment_ledger_fk;
ALTER TABLE asset_accounts DROP CONSTRAINT asset_investment_binding;
ALTER TABLE asset_accounts DROP COLUMN investment_convention_key, DROP COLUMN ledger_valuation_from, DROP COLUMN ledger_started_at, DROP COLUMN investment_ledger_id, DROP COLUMN investment_mode;
