-- Refuse a rollback that would lose property records; prefer a forward fix.
DO $$ DECLARE c RECORD; BEGIN
    IF EXISTS (SELECT 1 FROM asset_accounts WHERE kind = 'property') THEN
        RAISE EXCEPTION 'Cannot roll back while property records exist';
    END IF;
    FOR c IN SELECT conname FROM pg_constraint
             WHERE conrelid = 'asset_accounts'::regclass AND contype = 'c'
               AND pg_get_constraintdef(oid) LIKE '%kind%'
    LOOP
        EXECUTE format('ALTER TABLE asset_accounts DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE asset_accounts
    ADD CONSTRAINT asset_accounts_kind_check
        CHECK (kind IN ('cash', 'bank', 'deposit', 'investment')),
    ADD CONSTRAINT asset_accounts_currency_kind_check
        CHECK ((kind IN ('deposit', 'investment') AND fixed_currency_code IS NOT NULL) OR
               (kind IN ('cash', 'bank') AND fixed_currency_code IS NULL));
