-- A tracker purchase need not have a matching cash deposit. Reverting populated
-- trackers to cash accounting would invalidate their preserved operation history.
DO $portfolio_tracking$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid='investment_ledgers'::regclass AND attname='portfolio_tracking' AND NOT attisdropped) THEN RETURN; END IF;
 IF EXISTS (SELECT 1 FROM investment_ledgers l JOIN investment_operations o ON o.tenant_id=l.tenant_id AND o.ledger_id=l.id WHERE l.portfolio_tracking) THEN
  RAISE EXCEPTION 'Cannot revert populated portfolio trackers to cash accounting';
 END IF;
 ALTER TABLE investment_ledgers DROP COLUMN portfolio_tracking;
 UPDATE investment_valuation_generations SET generation=generation+1;
END
$portfolio_tracking$;
