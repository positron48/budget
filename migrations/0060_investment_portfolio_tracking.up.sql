-- Preserve facts and convert standalone investment accounts into portfolio trackers.
DO $portfolio_tracking$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid='investment_ledgers'::regclass AND attname='portfolio_tracking' AND NOT attisdropped) THEN
  ALTER TABLE investment_ledgers ADD COLUMN portfolio_tracking boolean NOT NULL DEFAULT true;
  -- Accounts already connected to household transfers retain their cash contract.
  UPDATE investment_ledgers l SET portfolio_tracking=false
  WHERE EXISTS (SELECT 1 FROM investment_cash_links c WHERE c.tenant_id=l.tenant_id AND c.ledger_id=l.id)
     OR EXISTS (SELECT 1 FROM investment_transfers t WHERE t.tenant_id=l.tenant_id AND (t.from_ledger_id=l.id OR t.to_ledger_id=l.id));
  UPDATE investment_valuation_generations SET generation=generation+1;
 END IF;
END
$portfolio_tracking$;
