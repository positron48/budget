ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS is_extraordinary BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_transactions_extraordinary
  ON transactions(tenant_id, is_extraordinary, occurred_at);
