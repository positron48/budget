ALTER TABLE market_data_subscriptions ADD COLUMN IF NOT EXISTS fx_demand BOOLEAN NOT NULL DEFAULT false;
