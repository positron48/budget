DO $guard$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_ledgers) THEN
  RAISE EXCEPTION 'Cannot roll back investment transfers: journal data exists' USING ERRCODE='55000';
 END IF;
END
$guard$;
DROP TABLE investment_transfers;
DELETE FROM financial_write_requests WHERE operation='investment.transfer.mutate';
ALTER TABLE investment_operations DROP CONSTRAINT investment_operations_kind_check;
ALTER TABLE investment_operations ADD CONSTRAINT investment_operations_kind_check CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split','amortization','redemption','fx'));
