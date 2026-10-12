DO $migration$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='investment_operations'::regclass AND conname='investment_operations_kind_check' AND pg_get_constraintdef(oid) LIKE '%amortization%') THEN
ALTER TABLE investment_operations DROP CONSTRAINT IF EXISTS investment_operations_kind_check;
ALTER TABLE investment_operations ADD CONSTRAINT investment_operations_kind_check CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split','amortization','redemption'));
 END IF;
END
$migration$;
