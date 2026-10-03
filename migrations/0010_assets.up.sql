CREATE TABLE IF NOT EXISTS asset_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    kind TEXT NOT NULL CHECK (kind IN ('cash', 'bank', 'deposit', 'investment')),
    institution TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    fixed_currency_code VARCHAR(3),
    deposit_rate NUMERIC(10,4),
    deposit_opened_on DATE,
    deposit_matures_on DATE,
    archived BOOLEAN NOT NULL DEFAULT false,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CHECK ((kind IN ('deposit', 'investment') AND fixed_currency_code IS NOT NULL) OR
           (kind IN ('cash', 'bank') AND fixed_currency_code IS NULL)),
    CHECK (deposit_rate IS NULL OR deposit_rate >= 0),
    CHECK (deposit_matures_on IS NULL OR deposit_opened_on IS NULL OR deposit_matures_on >= deposit_opened_on)
);
CREATE INDEX IF NOT EXISTS idx_asset_accounts_tenant ON asset_accounts(tenant_id, archived, kind);

CREATE TABLE IF NOT EXISTS asset_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    account_id UUID NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    amount_minor BIGINT NOT NULL,
    as_of TIMESTAMPTZ NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('opening', 'reconciliation', 'valuation')),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    note TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE CASCADE,
    UNIQUE (account_id, currency_code, as_of)
);
CREATE INDEX IF NOT EXISTS idx_asset_snapshots_lookup ON asset_snapshots(tenant_id, account_id, currency_code, as_of DESC);

CREATE TABLE IF NOT EXISTS asset_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    from_account_id UUID,
    to_account_id UUID,
    from_amount_minor BIGINT NOT NULL CHECK (from_amount_minor > 0),
    from_currency_code VARCHAR(3) NOT NULL,
    to_amount_minor BIGINT NOT NULL CHECK (to_amount_minor > 0),
    to_currency_code VARCHAR(3) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_account_id IS NOT NULL OR to_account_id IS NOT NULL),
    CHECK (from_account_id IS NULL OR to_account_id IS NULL OR from_account_id <> to_account_id),
    FOREIGN KEY (tenant_id, from_account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, to_account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT
);

ALTER TABLE transactions ADD COLUMN IF NOT EXISTS asset_account_id UUID;
ALTER TABLE currency_exchanges ADD COLUMN IF NOT EXISTS from_asset_account_id UUID;
ALTER TABLE currency_exchanges ADD COLUMN IF NOT EXISTS to_asset_account_id UUID;
-- This migration is also safe for the existing Kubernetes init script, which reapplies SQL.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'transactions_asset_account_fk') THEN
        ALTER TABLE transactions ADD CONSTRAINT transactions_asset_account_fk
            FOREIGN KEY (tenant_id, asset_account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'exchanges_from_asset_fk') THEN
        ALTER TABLE currency_exchanges ADD CONSTRAINT exchanges_from_asset_fk
            FOREIGN KEY (tenant_id, from_asset_account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT;
        ALTER TABLE currency_exchanges ADD CONSTRAINT exchanges_to_asset_fk
            FOREIGN KEY (tenant_id, to_asset_account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT;
        ALTER TABLE currency_exchanges ADD CONSTRAINT exchanges_asset_pair_check
            CHECK ((from_asset_account_id IS NULL) = (to_asset_account_id IS NULL));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS asset_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    account_id UUID NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    amount_minor BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    transaction_id UUID REFERENCES transactions(id) ON DELETE CASCADE,
    exchange_id UUID REFERENCES currency_exchanges(id) ON DELETE CASCADE,
    transfer_id UUID REFERENCES asset_transfers(id) ON DELETE CASCADE,
    leg TEXT NOT NULL CHECK (leg IN ('transaction', 'from', 'to')),
    FOREIGN KEY (tenant_id, account_id) REFERENCES asset_accounts(tenant_id, id) ON DELETE RESTRICT,
    CHECK (num_nonnulls(transaction_id, exchange_id, transfer_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_movement_transaction ON asset_movements(transaction_id) WHERE transaction_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_movement_exchange ON asset_movements(exchange_id, leg) WHERE exchange_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_movement_transfer ON asset_movements(transfer_id, leg) WHERE transfer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_asset_movements_balance ON asset_movements(tenant_id, account_id, currency_code, occurred_at);

CREATE TABLE IF NOT EXISTS financial_write_requests (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    operation TEXT NOT NULL,
    request_key TEXT NOT NULL CHECK (length(request_key) BETWEEN 1 AND 128),
    payload_hash TEXT NOT NULL,
    result JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, operation, request_key)
);
