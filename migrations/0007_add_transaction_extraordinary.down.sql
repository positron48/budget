DROP INDEX IF EXISTS idx_transactions_extraordinary;

ALTER TABLE transactions
  DROP COLUMN IF EXISTS is_extraordinary;
