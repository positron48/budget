CREATE UNIQUE INDEX IF NOT EXISTS transactions_tenant_id_id_payment ON transactions(tenant_id,id);
CREATE UNIQUE INDEX IF NOT EXISTS categories_tenant_id_id_payment ON categories(tenant_id,id);
CREATE TABLE IF NOT EXISTS recurring_payment_templates (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL REFERENCES tenants(id),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 200), category_id uuid NOT NULL, currency_code text NOT NULL CHECK(currency_code ~ '^[A-Z]{3}$'),
 account_id uuid, note text NOT NULL DEFAULT '', timezone text NOT NULL,
 amount_mode text NOT NULL CHECK(amount_mode IN ('fixed','variable')), fixed_amount bigint CHECK(fixed_amount>0), initial_estimate bigint CHECK(initial_estimate>0),
 effective_month date NOT NULL, first_month date NOT NULL, last_month date, interval_months int NOT NULL CHECK(interval_months IN (1,3,12)), day int NOT NULL CHECK(day BETWEEN 1 AND 31), last_day boolean NOT NULL DEFAULT false,
 period_offset int NOT NULL CHECK(period_offset IN (0,-1)), archived boolean NOT NULL DEFAULT false, version bigint NOT NULL DEFAULT 1,
 created_by uuid NOT NULL REFERENCES users(id), user_id uuid NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,category_id) REFERENCES categories(tenant_id,id),
 FOREIGN KEY(tenant_id,account_id) REFERENCES asset_accounts(tenant_id,id),
 CHECK(amount_mode<>'fixed' OR fixed_amount IS NOT NULL), CHECK(last_month IS NULL OR last_month>=first_month)
);
CREATE TABLE IF NOT EXISTS recurring_payment_occurrences (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id uuid NOT NULL, template_id uuid NOT NULL,
 scheduled_month date NOT NULL, period_month date NOT NULL, due_date date NOT NULL,
 name text NOT NULL, category_id uuid NOT NULL, currency_code text NOT NULL CHECK(currency_code ~ '^[A-Z]{3}$'), account_id uuid,
 note text NOT NULL DEFAULT '', timezone text NOT NULL, amount_mode text NOT NULL CHECK(amount_mode IN ('fixed','variable')),
 exact_amount bigint CHECK(exact_amount>0), estimate_override bigint CHECK(estimate_override>0), initial_estimate bigint CHECK(initial_estimate>0),
 skipped boolean NOT NULL DEFAULT false, skip_reason text NOT NULL DEFAULT '', manual boolean NOT NULL DEFAULT false, version bigint NOT NULL DEFAULT 1,
 user_id uuid NOT NULL REFERENCES users(id), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,template_id,scheduled_month), FOREIGN KEY(tenant_id,template_id) REFERENCES recurring_payment_templates(tenant_id,id),
 FOREIGN KEY(tenant_id,category_id) REFERENCES categories(tenant_id,id), FOREIGN KEY(tenant_id,account_id) REFERENCES asset_accounts(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS recurring_payment_due ON recurring_payment_occurrences(tenant_id,due_date,id);
CREATE TABLE IF NOT EXISTS recurring_payment_links (
 tenant_id uuid NOT NULL, occurrence_id uuid NOT NULL, transaction_id uuid NOT NULL,
 confirmed_amount bigint NOT NULL CHECK(confirmed_amount>0), confirmed_currency text NOT NULL, confirmed_type text NOT NULL CHECK(confirmed_type='expense'),
 confirmed_by uuid NOT NULL REFERENCES users(id), confirmed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,occurrence_id), UNIQUE(tenant_id,transaction_id),
 FOREIGN KEY(tenant_id,occurrence_id) REFERENCES recurring_payment_occurrences(tenant_id,id),
 FOREIGN KEY(tenant_id,transaction_id) REFERENCES transactions(tenant_id,id) ON DELETE CASCADE
);
