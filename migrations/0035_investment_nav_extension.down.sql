DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM investment_portfolios WHERE data->>'AutoExtend'='true')
 OR EXISTS(SELECT 1 FROM investment_nav_watches WHERE extension_limited) THEN
  RAISE EXCEPTION 'Automatic history extension state must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
DROP INDEX investment_portfolios_rolling;
ALTER TABLE investment_nav_watches DROP COLUMN extension_limited;
