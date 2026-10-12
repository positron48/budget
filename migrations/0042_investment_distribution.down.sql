DO $guard$
BEGIN
 IF EXISTS(SELECT 1 FROM investment_operations WHERE kind='distribution') THEN
 RAISE EXCEPTION 'Cannot roll back corporate distributions: journal data exists' USING ERRCODE='55000';
 END IF;
END
$guard$;
ALTER TABLE investment_operations DROP CONSTRAINT investment_operations_kind_check;
ALTER TABLE investment_operations ADD CONSTRAINT investment_operations_kind_check CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split','amortization','redemption','fx','transfer_out','transfer_in','opening_cash','opening_position','cash_interest','repo_income'));
