"""Packaging-only source/capacity/current-version read-only gate."""
import shutil,crypt
oldpg='b2229c012c06ac8fab34b83b8ae0442077bb69bfc5cf065764bf94ca446c8abe'
assert not inspect('839b9ed3ef0ba4c531a8c7b1331adcbd5881154c3f2a097171fde66300193d06','.State.Running')
projects={}
for project in ('weknora','knowhub-sourceid-business','weknora-segment-test'):
    ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
    projects[project]={x['service']:x for x in (metadata(cid) for cid in ids)}
assert projects['knowhub-sourceid-business']['app']['image_id']=='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
assert projects['knowhub-sourceid-business']['frontend']['image_id']=='sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
q="BEGIN READ ONLY; SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations; SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'tenants',(SELECT COUNT(*) FROM tenants),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'active_documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'active_nonempty_chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embeddings',(SELECT COUNT(*) FROM embeddings)); SELECT json_build_object('model_rows',COUNT(*),'api_key_present',COUNT(*) FILTER (WHERE COALESCE(parameters::jsonb->>'api_key','')<>''),'api_key_encrypted',COUNT(*) FILTER (WHERE parameters::jsonb->>'api_key' LIKE 'enc:v1:%'),'app_secret_present',COUNT(*) FILTER (WHERE COALESCE(parameters::jsonb->>'app_secret','')<>'')) FROM models; ROLLBACK;"
p=subprocess.run(['docker','exec','-i',oldpg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert p.returncode==0
data=[json.loads(x) for x in p.stdout.decode().splitlines() if x];assert data[0]=={'version':79,'dirty':False}
disk=shutil.disk_usage('/home/aqyfzx/ai');mem=int(next(x.split()[1] for x in pathlib.Path('/proc/meminfo').read_text().splitlines() if x.startswith('MemAvailable:')))*1024
assert disk.free>12*1024**3 and mem>7*1024**3 and hasattr(crypt,'METHOD_BLOWFISH')
old=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016/private')
assert (old/'database79.dump').is_file() and (old/'files.tar').is_file()
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'result':'PASS','target':'172.30.232.42','projects':projects,'source':data,'disk_free_bytes':disk.free,'memory_available_bytes':mem,'old_clean_source_snapshot_present':True,'local_candidate_password_hash_supported':True,'source_modified':False}))
