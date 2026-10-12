DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_portfolios) OR EXISTS(SELECT 1 FROM investment_portfolio_requests) THEN
  RAISE EXCEPTION 'Portfolio definitions and audit must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP TABLE investment_portfolio_requests;
DROP TABLE investment_portfolio_revisions;
DROP TABLE investment_portfolios;
