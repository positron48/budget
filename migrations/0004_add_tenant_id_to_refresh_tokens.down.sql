-- Remove tenant_id column from refresh_tokens table
DROP INDEX IF EXISTS idx_refresh_tokens_tenant;
ALTER TABLE refresh_tokens DROP COLUMN IF EXISTS tenant_id;
