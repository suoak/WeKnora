"""Protected-process own-email comparison: booleans/counts only, no identities."""
import datetime, json, subprocess, sys

data = json.load(sys.stdin)
assert set(data) == {'email'} and isinstance(data['email'], str)
email = data['email'].strip().lower()
assert email.endswith('@ruijie.com.cn') and len(email) <= 254
expression = "convert_from(decode('"+email.encode().hex()+"','hex'),'UTF8')"
results = []
for project in ('weknora', 'weknora-segment-test', 'knowhub-sourceid-business'):
    ids = subprocess.check_output(['docker','ps','-q','--filter','label=com.docker.compose.project='+project,'--filter','label=com.docker.compose.service=postgres']).decode().split()
    if len(ids) != 1:
        results.append({'project':project,'status':'NOT_ACCESSIBLE'});continue
    sql = """BEGIN READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL log_statement='none';
SET LOCAL log_min_messages='panic';
SET LOCAL log_min_error_statement='panic';
SET LOCAL log_parameter_max_length_on_error=0;
SELECT json_build_object('matched_accounts',COUNT(*),
 'active_matches',COUNT(*) FILTER(WHERE u.is_active AND u.deleted_at IS NULL),
 'system_admin_matches',COUNT(*) FILTER(WHERE u.is_system_admin),
 'cross_tenant_matches',COUNT(*) FILTER(WHERE u.can_access_all_tenants),
 'owner_or_admin_membership_matches',COUNT(*) FILTER(WHERE EXISTS(
 SELECT 1 FROM tenant_members m WHERE m.user_id=u.id AND m.deleted_at IS NULL AND m.role IN ('owner','admin'))),
 'ordinary_membership_matches',COUNT(*) FILTER(WHERE EXISTS(
 SELECT 1 FROM tenant_members m WHERE m.user_id=u.id AND m.deleted_at IS NULL AND m.role IN ('contributor','viewer') AND m.status='active')))
FROM users u WHERE LOWER(TRIM(u.email))="""+expression+";\nROLLBACK;"
    q = subprocess.run(['docker','exec','-i',ids[0],'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=sql.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
    if q.returncode:
        results.append({'project':project,'status':'NOT_ACCESSIBLE_PRIVATE_QUERY'});continue
    row = json.loads(q.stdout.decode())
    assert set(row)=={'matched_accounts','active_matches','system_admin_matches','cross_tenant_matches','owner_or_admin_membership_matches','ordinary_membership_matches'}
    assert all(type(x) is int and x>=0 for x in row.values())
    results.append({'project':project,'status':'READ_ONLY_MATCHED_COUNTS','counts':row})
email = expression = sql = data = None
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'result':'PASS','projects':results,'scope':'authorized employee own-email comparison only','employee_values_exported':False,'email_fingerprint_exported':False,'all_queries_read_only':True,'database_or_configuration_changed':False,'sid_login_started':False}))
