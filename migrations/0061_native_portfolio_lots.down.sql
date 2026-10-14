DO $native_portfolio_lots$
BEGIN
 IF EXISTS (
  SELECT 1 FROM investment_operations o JOIN investment_ledgers l ON l.tenant_id=o.tenant_id AND l.id=o.ledger_id
  WHERE l.portfolio_tracking AND o.status='posted'
    AND COALESCE(o.data->>'FeeCurrency','')<>'' AND o.data->>'FeeCurrency'<>o.data->>'Currency'
 ) OR EXISTS (
  SELECT 1 FROM investment_operations o JOIN investment_ledgers l ON l.tenant_id=o.tenant_id AND l.id=o.ledger_id
  WHERE l.portfolio_tracking AND o.status='posted'
    AND COALESCE(o.data->>'InstrumentKey','')<>'' AND COALESCE(o.data->>'Currency','')<>''
  GROUP BY o.tenant_id,o.ledger_id,o.data->>'InstrumentKey'
  HAVING count(DISTINCT o.data->>'Currency')>1
 ) THEN
  RAISE EXCEPTION 'Cannot revert portfolios containing native commissions or multiple acquisition currencies';
 END IF;
 UPDATE investment_valuation_generations g SET generation=generation+1
 WHERE EXISTS (SELECT 1 FROM investment_ledgers l WHERE l.tenant_id=g.tenant_id AND l.portfolio_tracking);
END
$native_portfolio_lots$;
