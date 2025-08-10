-- Проверяем токены в базе данных
SELECT 
    user_id, 
    tenant_id, 
    created_at,
    expires_at,
    revoked_at,
    CASE 
        WHEN tenant_id IS NULL THEN 'NULL tenant_id'
        WHEN tenant_id = '' THEN 'Empty tenant_id'
        ELSE 'Has tenant_id'
    END as status
FROM refresh_tokens 
ORDER BY created_at DESC 
LIMIT 10;
