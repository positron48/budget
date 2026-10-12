-- Refuse destructive rollback once user data exists. Disable writes instead.
DO $guard$
BEGIN
 IF EXISTS (SELECT 1 FROM investment_source_links) THEN
  RAISE EXCEPTION 'Cannot roll back 0014_investment_source_links: investment data exists' USING ERRCODE = '55000';
 END IF;
 IF EXISTS (SELECT 1 FROM investment_import_ledger_bindings) THEN
  RAISE EXCEPTION 'Cannot roll back 0014_investment_source_links: investment data exists' USING ERRCODE = '55000';
 END IF;
END
$guard$;
DROP TABLE IF EXISTS investment_import_ledger_bindings;
DROP TABLE IF EXISTS investment_source_links;
