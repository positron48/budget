-- Working investment ledgers do not affect AssetService until explicit cutover.
CREATE TABLE IF NOT EXISTS investment_ledgers (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 200),
 started_at TIMESTAMPTZ NOT NULL,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 next_sequence BIGINT NOT NULL DEFAULT 1 CHECK(next_sequence>0),
 created_by UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS investment_operations (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('deposit','withdrawal','buy','sell','dividend','coupon','fee','tax','split')),
 occurred_at TIMESTAMPTZ NOT NULL,
 settlement_at TIMESTAMPTZ NOT NULL CHECK(settlement_at>=occurred_at),
 sequence BIGINT NOT NULL CHECK(sequence>0),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 status TEXT NOT NULL CHECK(status IN ('posted','voided')),
 data JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,ledger_id,id), UNIQUE(tenant_id,ledger_id,sequence),
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS investment_operations_history ON investment_operations(tenant_id,ledger_id,occurred_at,sequence);
CREATE TABLE IF NOT EXISTS investment_operation_revisions (
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 version BIGINT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('create','amend','void')),
 before_data JSONB,
 after_data JSONB NOT NULL,
 actor_id UUID NOT NULL REFERENCES users(id),
 reason TEXT NOT NULL,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,ledger_id,operation_id,version),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
CREATE TABLE IF NOT EXISTS investment_security_legs (
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 ordinal INTEGER NOT NULL,
 instrument_key TEXT NOT NULL,
 quantity NUMERIC(28,10) NOT NULL,
 effective_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(tenant_id,ledger_id,operation_id,ordinal),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
CREATE TABLE IF NOT EXISTS investment_cash_legs (
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 operation_id UUID NOT NULL,
 ordinal INTEGER NOT NULL,
 currency VARCHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 bucket TEXT NOT NULL CHECK(bucket IN ('settled','receivable','payable')),
 component TEXT NOT NULL,
 amount NUMERIC(36,12) NOT NULL,
 effective_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(tenant_id,ledger_id,operation_id,ordinal),
 FOREIGN KEY(tenant_id,ledger_id,operation_id) REFERENCES investment_operations(tenant_id,ledger_id,id)
);
CREATE INDEX IF NOT EXISTS investment_cash_legs_history ON investment_cash_legs(tenant_id,ledger_id,effective_at);
CREATE TABLE IF NOT EXISTS investment_recalculation_jobs (
 tenant_id UUID NOT NULL,
 ledger_id UUID NOT NULL,
 journal_revision BIGINT NOT NULL,
 dirty_from TIMESTAMPTZ NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','ready','error')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,ledger_id,journal_revision),
 FOREIGN KEY(tenant_id,ledger_id) REFERENCES investment_ledgers(tenant_id,id)
);
