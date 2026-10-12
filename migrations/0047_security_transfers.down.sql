DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_operations WHERE kind IN ('security_transfer_in','security_transfer_out')) THEN
  RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='security transfer facts exist; rollback would lose lot lineage';
 END IF;
END $$;
DROP INDEX IF EXISTS investment_security_transfer_counterparty;
ALTER TABLE investment_operations DROP CONSTRAINT IF EXISTS investment_operations_kind_check;
ALTER TABLE investment_operations ADD CONSTRAINT investment_operations_kind_check CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split','amortization','redemption','fx','transfer_out','transfer_in','opening_cash','opening_position','cash_interest','repo_income','distribution'));
