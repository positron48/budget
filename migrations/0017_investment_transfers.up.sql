DO $migration$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='investment_operations'::regclass AND conname='investment_operations_kind_check' AND pg_get_constraintdef(oid) LIKE '%transfer_out%') THEN
  ALTER TABLE investment_operations DROP CONSTRAINT IF EXISTS investment_operations_kind_check;
  ALTER TABLE investment_operations ADD CONSTRAINT investment_operations_kind_check CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split','amortization','redemption','fx','transfer_out','transfer_in'));
 END IF;
END
$migration$;
CREATE TABLE IF NOT EXISTS investment_transfers (
 tenant_id UUID NOT NULL,
 id UUID NOT NULL,
 from_ledger_id UUID NOT NULL,
 to_ledger_id UUID NOT NULL CHECK(to_ledger_id<>from_ledger_id),
 from_operation_id UUID NOT NULL,
 to_operation_id UUID NOT NULL,
 data JSONB NOT NULL,
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,from_ledger_id,from_operation_id),
 UNIQUE(tenant_id,to_ledger_id,to_operation_id),
 FOREIGN KEY(tenant_id,from_ledger_id,from_operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id),
 FOREIGN KEY(tenant_id,to_ledger_id,to_operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
