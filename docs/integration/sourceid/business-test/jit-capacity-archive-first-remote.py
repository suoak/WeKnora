"""Retain the first fixture-assertion failure; never erase it or change data."""
import shutil
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
r=json.loads((b/'capacity-result.json').read_text());assert r['result']=='FAIL' and r['failure_class']=='AssertionError' and r['capacity']['result']=='PASS'
assert r['failure_lines']==[82] and r['disposable_mock_database_removed']
fault=json.loads((b/'capacity-fault-evidence.json').read_text());assert fault['result']=='PASS' and fault['invariants']['historical_refresh_rows']==24 and fault['invariants']['new_refresh_rows']==0
assert run(['docker','exec','knowhub-oa-sso-pilot-postgres-1','psql','-X','-U','clean_owner','-d','postgres','-qAt','-c',"SELECT COUNT(*) FROM pg_database WHERE datname='enterprise_capacity_validation'"]).decode().strip()=='0'
archive=b/'capacity-first-attempt';archive.mkdir(mode=0o700)
for n in ('capacity-result.json','capacity-progress.json','capacity-fault-evidence.json','capacity-worker.py'):
 if (b/n).exists():shutil.move(str(b/n),str(archive/n))
print(json.dumps({'result':'PASS','initial_failure_retained':True,'failure_cause':'fixture_assertion_included_24_revoked_historical_refresh_rows','app_code_changed':False,'rerun_uses_new_synthetic_rows_only':True}))
