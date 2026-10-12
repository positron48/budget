-- Refuse destructive rollback once user data exists. Disable writes instead.
DO $guard$
BEGIN
 IF EXISTS (SELECT 1 FROM investment_ledgers) THEN
  RAISE EXCEPTION 'Cannot roll back 0013_investment_journal: investment data exists' USING ERRCODE = '55000';
 END IF;
END
$guard$;
DROP TABLE IF EXISTS investment_recalculation_jobs;
DROP TABLE IF EXISTS investment_cash_legs;
DROP TABLE IF EXISTS investment_security_legs;
DROP TABLE IF EXISTS investment_operation_revisions;
DROP TABLE IF EXISTS investment_operations;
DROP TABLE IF EXISTS investment_ledgers;
DELETE FROM financial_write_requests WHERE operation IN ('investment.ledger.create','investment.operation.mutate');
