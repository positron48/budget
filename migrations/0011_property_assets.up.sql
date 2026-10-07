-- Replace the kind and fixed-currency checks, including on repeated init runs.
DO $$ DECLARE c RECORD; BEGIN
    FOR c IN SELECT conname FROM pg_constraint
             WHERE conrelid = 'asset_accounts'::regclass AND contype = 'c'
               AND pg_get_constraintdef(oid) LIKE '%kind%'
    LOOP
        EXECUTE format('ALTER TABLE asset_accounts DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE asset_accounts
    ADD CONSTRAINT asset_accounts_kind_check
        CHECK (kind IN ('cash', 'bank', 'deposit', 'investment', 'property')),
    ADD CONSTRAINT asset_accounts_currency_kind_check
        CHECK ((kind IN ('deposit', 'investment', 'property') AND fixed_currency_code IS NOT NULL) OR
               (kind IN ('cash', 'bank') AND fixed_currency_code IS NULL));
