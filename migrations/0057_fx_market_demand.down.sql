DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM market_data_subscriptions WHERE fx_demand) THEN
  RAISE EXCEPTION 'Active FX refresh demand must be preserved' USING ERRCODE='55000';
 END IF;
END $$;
ALTER TABLE market_data_subscriptions DROP COLUMN IF EXISTS fx_demand;
