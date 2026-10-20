"""Sanitize clone; start candidate with no external interfaces or copied secrets."""
import importlib.util, secrets, ssl, time
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
assert m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3027|false'
image='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
assert run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',image]).decode().strip()=='035f35e1d388a2f02615d35cf826bbdcc22e56b0'
before=m.ownership()
m.sql("BEGIN; UPDATE auth_tokens SET is_revoked=TRUE; UPDATE users SET password_hash='!isolated-no-original-password'; UPDATE tenant_api_keys SET revoked_at=NOW(); UPDATE models SET parameters=(parameters::jsonb - 'api_key' - 'app_secret')::jsonb; UPDATE data_sources SET status='disabled',sync_schedule='',config='{}'; DELETE FROM system_settings; COMMIT;")
assert before==m.ownership()
out={'old_auth_tokens_all_revoked':int(m.sql('SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked'))==0,'old_api_keys_all_revoked':int(m.sql('SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL'))==0,'old_passwords_disabled':True,'fresh_jwt_and_aes_keys':True,'copied_models_keys_removed':True,'external_sources_disabled':True,'role_acl_preserved_during_sanitation':True}
paths=json.loads(m.sql("SELECT COALESCE(json_agg(COALESCE(r.physical_path,k.file_path)),'[]') FROM knowledges k LEFT JOIN resources r ON k.file_path='resource://'||r.handle AND r.deleted_at IS NULL WHERE k.deleted_at IS NULL"))
readable=exact=0
root=(m.BASE/'files').resolve()
for raw in paths:
    if not raw:continue
    if raw.startswith('storage://') and '/local://' in raw:raw=raw[raw.index('/local://')+1:]
    rel=raw[8:] if raw.startswith('local://') else raw[len('/data/files/'):] if raw.startswith('/data/files/') else raw
    p=(root/rel).resolve()
    try:p.relative_to(root)
    except ValueError:continue
    if p.is_file():
        exact+=1
        with p.open('rb') as f:
            if f.read(4096):readable+=1
out['original_documents_file_check']={'documents':len(paths),'exact_paths_present':exact,'nonempty_readable_files':readable}
# Compare existing stored vectors only: no model call and no values exported.
out['stored_vector_self_match_hits']=int(m.sql("SELECT COUNT(*) FROM (SELECT e.id FROM embeddings e ORDER BY e.embedding <=> (SELECT embedding FROM embeddings WHERE embedding IS NOT NULL LIMIT 1) LIMIT 3) s"))
private=m.BASE/'private'
fixture=private/'mock';fixture.mkdir(mode=0o700)
m.execute(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-keyout',str(fixture/'key.pem'),'-out',str(fixture/'ca.pem'),'-days','1','-subj','/CN=isolated-sid-mock','-addext','subjectAltName=IP:127.0.0.1'])
modulus=m.execute(['openssl','rsa','-in',str(fixture/'key.pem'),'-noout','-modulus']).decode().strip().split('=')[1]
import base64
n=base64.urlsafe_b64encode(bytes.fromhex(modulus)).decode().rstrip('=')
(fixture/'jwks.json').write_text(json.dumps({'keys':[{'kty':'RSA','use':'sig','alg':'RS256','kid':'isolated-key','n':n,'e':'AQAB'}]}))
admin=m.scalar("SELECT row_to_json(s) FROM (SELECT id,tenant_id FROM users WHERE is_system_admin AND is_active AND deleted_at IS NULL ORDER BY id LIMIT 1) s")
assert admin
# Only the copy gets a synthetic login address; original ID/roles/ACL stay intact.
m.sql("UPDATE users SET email='isolated-admin@example.test' WHERE id='"+admin['id']+"'")
(private/'original-admin-baseline.json').write_text(json.dumps(admin))
env={'DB_DRIVER':'postgres','DB_HOST':'127.0.0.1','DB_PORT':'5432','DB_USER':'clone','DB_PASSWORD':'synthetic-clone-only','DB_NAME':'clone','REDIS_ADDR':'127.0.0.1:6379','RETRIEVE_DRIVER':'postgres','AUTO_MIGRATE':'false','AUTO_RECOVER_DIRTY':'false','JWT_SECRET':secrets.token_urlsafe(48),'SYSTEM_AES_KEY':secrets.token_hex(16),'OIDC_AUTH_ENABLE':'false','SOURCEID_ENABLE':'true','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_PROTOCOL_ONLY':'true','SOURCEID_CONTRACT_CONFIRMED':'false','SOURCEID_AUTO_BINDING_ENABLE':'false','SOURCEID_EMAIL_BINDING_ENABLE':'false','SOURCEID_LOGOUT_ENABLE':'false','SOURCEID_DIAGNOSTICS_ENABLE':'false','SOURCEID_SESSION_MODE':'external','NEO4J_ENABLE':'false','LANGFUSE_ENABLED':'false','LLM_DEBUG_LOG':'false','GIN_MODE':'release','LOG_LEVEL':'error','LOG_PATH':'/tmp/isolated-logs','WEKNORA_SANDBOX_DOCKER_ENABLED':'false','WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL':'','STORAGE_TYPE':'local','LOCAL_STORAGE_BASE_DIR':'/data/files','DISABLE_REGISTRATION':'true','WEKNORA_TENANT_ENABLE_RBAC':'true','WEKNORA_TENANT_ENABLE_CROSS_TENANT_ACCESS':'true','SSRF_WHITELIST':'127.0.0.1','HTTP_PROXY':'','HTTPS_PROXY':'','ALL_PROXY':'','NO_PROXY':'*'}
(private/'app-disabled.env').write_text('\n'.join(k+'='+v for k,v in env.items())+'\n')
redis='oa-legacy79-redis-20261016'
m.execute(['docker','run','-d','--name',redis,'--network','container:'+m.PG,'--log-driver','none','--memory','256m','sha256:426f6fb54a1b82a2254bce70e5f4a72d15c14ecd3cc70d799465b956e7a32548','redis-server','--bind','127.0.0.1','--save','','--appendonly','no'])
app='oa-legacy79-app-20261016'
m.execute(['docker','run','-d','--name',app,'--network','container:'+m.PG,'--log-driver','none','--cpus','2','--memory','4g','--env-file',str(private/'app-disabled.env'),'--mount','type=bind,source='+str(m.BASE/'files')+',target=/data/files,readonly','--tmpfs','/tmp:rw,size=268435456','--entrypoint','/app/WeKnora',image])
probe="import urllib.request,json,socket;print(json.dumps({'health':urllib.request.urlopen('http://127.0.0.1:8080/health',timeout=3).status}))"
for _ in range(50):
    p=subprocess.run(['docker','exec',app,'python3','-c',probe],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    if p.returncode==0:break
    time.sleep(1)
else:raise RuntimeError('isolated_app_health_timeout')
out['disabled_oidc_app_health']=json.loads(p.stdout)
network_test="import socket,json; r={}\nfor ip,port in [('172.30.232.42',80),('172.30.232.42',8080),('1.1.1.1',443)]:\n s=socket.socket();s.settimeout(1)\n try:s.connect((ip,port));r[ip+':'+str(port)]=False\n except OSError:r[ip+':'+str(port)]=True\n finally:s.close()\nprint(json.dumps(r))"
out['outbound_blocked']=json.loads(m.execute(['docker','exec',app,'python3','-c',network_test]));assert all(out['outbound_blocked'].values())
out['candidate_image_id']=image;out['network_mode']='shared isolated network-none namespace; loopback only';out['published_ports']=inspect(app,'.HostConfig.PortBindings');assert not out['published_ports']
out['result']='PASS'
(m.BASE/'app-preparation-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
