b=pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
t=(b/'artifacts/tests.log').read_text()
status=json.loads((b/'status.json').read_text())
status['top_level_passes']=len(re.findall(r'^--- PASS:',t,re.M))
status['subtest_passes']=len(re.findall(r'^    --- PASS:',t,re.M))
status['test_failures']=len(re.findall(r'^--- FAIL:|^FAIL',t,re.M))
status['isolated_pg_removed']=not bool(run(['docker','ps','-aq','--filter','name=^oa-accountprep-pg-20261016$']).strip())
status['isolated_network_removed']=not bool(run(['docker','network','ls','-q','--filter','name=^oa-accountprep-synthetic-20261016$']).strip())
assert status['result']=='PASS' and status['test_failures']==0 and status['isolated_pg_removed'] and status['isolated_network_removed']
private=b/'private';backup=private/'before-approved-account.dump'
assert private.stat().st_mode & 0o777==0o700
assert backup.stat().st_mode & 0o777==0o600
assert backup.stat().st_size>0
receipts=list(private.glob('*.json'));assert len(receipts)==1
receipt=json.loads(receipts[0].read_text())
uid=receipt['protected_baseline']['user_id']
assert re.fullmatch(r'[a-f0-9-]{36}',uid)
pg=run(['docker','ps','-q','--filter','label=com.docker.compose.project=knowhub-sourceid-business','--filter','label=com.docker.compose.service=postgres']).decode().strip()
sql="SELECT json_build_object('active',u.is_active,'ordinary',NOT u.is_system_admin AND NOT u.can_access_all_tenants,'same_user',u.id='"+uid+"','same_tenant',u.tenant_id="+str(receipt['protected_baseline']['tenant_id'])+",'viewer',m.role='viewer','email_nonempty',u.email<>'' ) FROM users u JOIN tenant_members m ON m.user_id=u.id WHERE u.id='"+uid+"' AND m.deleted_at IS NULL;"
q=subprocess.run(['docker','exec','-i',pg,'sh','-c','exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+sql+'ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
assert q.returncode==0
row=json.loads(q.stdout.decode().strip());assert all(row.values())
status['actual_account_baseline_checks']=row
status['backup_permissions']='0700/0600';status['protected_baseline_saved']=True
status['synthetic_tests_accessed_business_database']=status.pop('business_database_accessed')
status['actual_account_postcheck_read_business_database']=True
status['postcheck_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
print(json.dumps(status))
