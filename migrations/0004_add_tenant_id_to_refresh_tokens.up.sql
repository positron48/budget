-- Add tenant_id column to refresh_tokens table
ALTER TABLE refresh_tokens ADD COLUMN tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

-- Create index for tenant_id
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_tenant ON refresh_tokens(tenant_id);
