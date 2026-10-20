"""Approved single-account write only. Email/DB credentials arrive over private IPC.

This file never starts the application or enables SID. It is not invoked by CI.
"""
import datetime, hashlib, json, os, pathlib, re, subprocess, sys, urllib.parse

os.umask(0o077)
base = pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
status = json.loads((base / 'status.json').read_text())
assert status['result'] == 'PASS'
binary = base / 'artifacts/sourceid-test-account'
assert hashlib.sha256(binary.read_bytes()).hexdigest() == status['binary_sha256']
payload = json.load(sys.stdin)
assert set(payload) == {'email'} and isinstance(payload['email'], str)

def run(args, data=None):
    p = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=50)
    if p.returncode:
        raise RuntimeError('controlled_operation_failed')
    return p.stdout

def inspect(cid, field):
    return json.loads(run(['docker', 'inspect', '--format', '{{json '+field+'}}', cid]))

ids = run(['docker', 'ps', '-q', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business']).decode().split()
services = {}
for cid in ids:
    services[inspect(cid, '.Config.Labels')['com.docker.compose.service']] = cid
app, pg = services['app'], services['postgres']
app_image = inspect(app, '.Image')
assert app_image == 'sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
assert inspect(services['frontend'], '.Image') == 'sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
flags = {}
for key, expected in {'SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_BASIC_ENCODING':'oauth_form'}.items():
    value = run(['docker','exec',app,'sh','-c','printf "%s" "${'+key+'}"']).decode()
    assert value == expected
    flags[key] = value
network = 'knowhub-sourceid-business_isolated'
pgip = inspect(pg, '.NetworkSettings.Networks')[network]['IPAddress']
# PostgreSQL configuration only; never inspect the APP environment or SID Secret.
env = dict(x.split('=',1) for x in inspect(pg,'.Config.Env') if x.startswith(('POSTGRES_USER=','POSTGRES_PASSWORD=','POSTGRES_DB=')))

def sql(text):
    raw = run(['docker','exec','-i',pg,'sh','-c','exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -qAt'], ('BEGIN READ ONLY;\n'+text+';\nROLLBACK;').encode())
    return [json.loads(x) for x in raw.decode().splitlines() if x.strip()]

initial = sql("""SELECT json_build_object('users',(SELECT count(*) FROM users),'tenants',(SELECT count(*) FROM tenants),
 'identities',(SELECT count(*) FROM external_identities),'sessions',(SELECT count(*) FROM source_id_sessions),
 'version',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations));
 SELECT json_build_object('operator',u.id,'tenant',m.tenant_id) FROM users u JOIN tenant_members m ON m.user_id=u.id
 WHERE u.is_active AND u.deleted_at IS NULL AND m.role='owner' AND m.status='active' AND m.deleted_at IS NULL""")
assert len(initial) == 2 and initial[0] == {'users':1,'tenants':1,'identities':0,'sessions':0,'version':3027,'dirty':False}
operation = 'cc558c5d-e9fb-4b92-95d8-566c325ce145'
private = base / 'private'
private.mkdir(mode=0o700, exist_ok=True)
receipt = private / (operation+'.json')
assert not receipt.exists()
backup = private / 'before-approved-account.dump'
with backup.open('xb') as f:
    p = subprocess.run(['docker','exec',pg,'sh','-c','exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc'],stdout=f,stderr=subprocess.PIPE,timeout=60)
    assert p.returncode == 0
os.chmod(backup,0o600)
query = urllib.parse.urlencode({'sslmode':'disable','options':'-c log_statement=none -c log_min_messages=panic -c log_min_error_statement=panic -c log_parameter_max_length_on_error=0'})
dsn = 'postgres://'+urllib.parse.quote(env['POSTGRES_USER'],safe='')+':'+urllib.parse.quote(env['POSTGRES_PASSWORD'],safe='')+'@'+pgip+':5432/'+urllib.parse.quote(env['POSTGRES_DB'],safe='')+'?'+query
data = json.dumps({'email':payload['email'],'database_dsn':dsn}).encode()
args = ['docker','run','--rm','--name','oa-approved-account-preparer','--network',network,'--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','-i','-v',str(binary)+':/account-preparer:ro','--entrypoint','/account-preparer',app_image,'--protected-json-stdin','--test-host','172.30.232.42','--tenant',str(initial[1]['tenant']),'--operator',initial[1]['operator'],'--operation',operation,'--approval','user-chat-20261016-single-account-only','--role','viewer']
dry = json.loads(run(args,data))
assert dry == {'dry_run':True,'create_user':True,'add_membership':True,'already_applied':False,'cleanup':False}
applied = json.loads(run(args+['--apply'],data))
assert applied == dict(dry,dry_run=False)
verify = json.loads(run(args,data))
assert verify['dry_run'] and verify['already_applied'] and not verify['create_user'] and not verify['add_membership']
data = dsn = env = payload = None
post = sql("""SELECT json_build_object('user_id',u.id,'tenant_id',u.tenant_id,'role',m.role,'active',u.is_active,
 'system_admin',u.is_system_admin,'cross_tenant',u.can_access_all_tenants,'membership_id',m.id)
 FROM audit_logs a JOIN users u ON u.id=a.target_user_id JOIN tenant_members m ON m.user_id=u.id AND m.tenant_id=a.tenant_id
 WHERE a.scope_id='"""+operation+"""' AND a.action='sourceid.test_account_prepared';
 SELECT json_build_object('users',(SELECT count(*) FROM users),'tenants',(SELECT count(*) FROM tenants),
 'owner_members',(SELECT count(*) FROM tenant_members WHERE role='owner' AND deleted_at IS NULL),
 'identities',(SELECT count(*) FROM external_identities),'reviews',(SELECT count(*) FROM source_id_binding_reviews),
 'sessions',(SELECT count(*) FROM source_id_sessions),'local_sessions',(SELECT count(*) FROM source_id_local_sessions),
 'active_kbs',(SELECT count(*) FROM knowledge_bases WHERE deleted_at IS NULL))""")
assert len(post)==2 and post[0]['role']=='viewer' and post[0]['active'] and not post[0]['system_admin'] and not post[0]['cross_tenant']
assert post[1]=={'users':2,'tenants':1,'owner_members':1,'identities':0,'reviews':0,'sessions':0,'local_sessions':0,'active_kbs':0}
receipt.write_text(json.dumps({'operation':operation,'protected_baseline':post[0],'approval_scope':'one account only; no SID gates/login/network/extra tenant','backup':str(backup)},indent=2))
os.chmod(receipt,0o600)
for key,expected in flags.items():
    assert run(['docker','exec',app,'sh','-c','printf "%s" "${'+key+'}"']).decode()==expected
assert set(run(['docker','ps','-q','--filter','label=com.docker.compose.project=knowhub-sourceid-business']).decode().split())==set(ids)
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','result':'PASS','operation':operation,'account_created':True,'role':'viewer','personal_tenant_created':False,'owner_permissions_changed':False,'dry_run_passed':True,'repeat_dry_run_idempotent':True,'business_containers_unchanged':True,'flags':flags,'aggregates':post[1],'protected_baseline_saved':True,'database_backup_saved':True,'employee_values_exported':False,'sid_login_started':False,'network_rules_changed':False,'binary_sha256':status['binary_sha256']}))
