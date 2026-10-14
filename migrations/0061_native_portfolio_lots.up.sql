-- Native portfolios keep acquisition currency on each FIFO lot and expense
-- commissions separately. Invalidate derived analytics; source journals and
-- household Budget data remain unchanged. This version also fences old binaries
-- whose embedded migrations do not know the native operation fields.
UPDATE investment_valuation_generations g SET generation=generation+1
WHERE EXISTS (SELECT 1 FROM investment_ledgers l WHERE l.tenant_id=g.tenant_id AND l.portfolio_tracking);
