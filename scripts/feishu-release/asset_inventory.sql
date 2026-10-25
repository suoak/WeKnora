-- Read-only, metadata only. "review" is NOT permission to remove a file.
-- Keep all active, current, referenced, unexpired and historical published files.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '15s';
SELECT a.id AS attempt_id, r.tenant_id, r.data_source_id, r.knowledge_id,
       a.status, a.lease_until, r.active_attempt = a.id AS active,
       r.current_attempt = a.id AS current,
       jsonb_array_length(COALESCE(a.manifest->'assets', '[]'::jsonb)) AS assets,
       CASE
         WHEN a.status = 'published' THEN 'retain_published_history'
         WHEN r.active_attempt = a.id THEN 'retain_active'
         WHEN r.current_attempt = a.id THEN 'retain_current_candidate'
         WHEN a.lease_until >= clock_timestamp() THEN 'retain_live_lease'
         ELSE 'expired_private_requires_object_and_reference_review'
       END AS disposition
FROM feishu_legacy_attempts a
JOIN feishu_legacy_resources r ON r.id = a.resource_id
WHERE a.manifest ? 'assets'
ORDER BY a.created_at;
ROLLBACK;
