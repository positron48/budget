ALTER TABLE market_data_subscriptions ADD COLUMN IF NOT EXISTS benchmark_demand BOOLEAN NOT NULL DEFAULT false;
