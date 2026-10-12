-- Refuse destructive rollback once user data exists. Disable writes instead.
DO $guard$
BEGIN
 IF EXISTS (SELECT 1 FROM investment_imports) THEN
  RAISE EXCEPTION 'Cannot roll back 0012_investment_imports: investment data exists' USING ERRCODE = '55000';
 END IF;
END
$guard$;
DROP TABLE IF EXISTS investment_import_mapping_revisions;
DROP TABLE IF EXISTS investment_import_rows;
DROP TABLE IF EXISTS investment_import_accounts;
DROP TABLE IF EXISTS investment_imports;
DELETE FROM financial_write_requests WHERE operation='investment.import.upload';
