-- PostgreSQL: run only with an authorized read-only connection.
BEGIN TRANSACTION READ ONLY;
SELECT pc.tenant_id, pc.display_name, pc.status, pc.allow_access_request,
       pc.responsible_team, pc.contact,
       (SELECT COUNT(*) FROM tenant_access_requests ar
        WHERE ar.tenant_id = pc.tenant_id AND ar.status = 'pending') AS pending_requests,
       (SELECT COUNT(*) FROM tenant_members tm
        WHERE tm.tenant_id = pc.tenant_id AND tm.status = 'suspended'
          AND tm.deleted_at IS NULL) AS suspended_members,
       CASE WHEN pc.allow_access_request THEN 'already_open'
            ELSE 'review_explicit_opt_out_before_enabling' END AS review_action
FROM tenant_portal_configs pc
JOIN tenants t ON t.id = pc.tenant_id
WHERE pc.status = 'published' AND t.status = 'active' AND t.deleted_at IS NULL
ORDER BY pc.tenant_id;
COMMIT;
