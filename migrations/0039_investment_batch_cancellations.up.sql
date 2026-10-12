CREATE TABLE IF NOT EXISTS investment_batch_cancellations (
 tenant_id uuid NOT NULL,
 batch_request_key text NOT NULL,
 batch_namespace text NOT NULL DEFAULT 'investment.import.commit' CHECK(batch_namespace='investment.import.commit'),
 request_key text NOT NULL CHECK(length(request_key) BETWEEN 1 AND 128),
 request_hash text NOT NULL,
 draft_id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 2000),
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,batch_request_key),
 UNIQUE(tenant_id,request_key),
 FOREIGN KEY(tenant_id,batch_namespace,batch_request_key) REFERENCES financial_write_requests(tenant_id,operation,request_key),
 FOREIGN KEY(tenant_id,actor_id,draft_id) REFERENCES investment_batch_drafts(tenant_id,actor_id,id)
);
