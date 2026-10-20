"""Read-only 80/8080 replacement gate; emit metadata/counts only."""
import os,time
out={'target':'172.30.232.42','read_only':True,'projects':{},'production_accessed':False}
flags=('OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_JIT_ENABLE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','AUTO_MIGRATE')
for project in ('knowhub-sourceid-business','knowhub-oa-sso-pilot'):
 ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
 services={metadata(cid)['service']:metadata(cid) for cid in ids}
 for service,m in services.items():
  m['networks']={k:{'ip':v.get('IPAddress'),'aliases':v.get('Aliases')} for k,v in inspect(m['id'],'.NetworkSettings.Networks').items()}
  m['restart_policy']=inspect(m['id'],'.HostConfig.RestartPolicy')
  if service in ('app','frontend'):m['revision']=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',m['image_id']]).decode().strip()
 env=environment(services['app']['id'])
 services['app']['safe_flags']={k:env.get(k,'NOT_SET') for k in flags}
 assert all(v in ('true','false','external','oauth_form','NOT_SET') for v in services['app']['safe_flags'].values())
 pg=services['postgres']['id']
 q="BEGIN READ ONLY;SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'knowledge_bases',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions),'active_access',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked AND expires_at>NOW()));ROLLBACK;"
 z=subprocess.run(['docker','exec','-i',pg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 assert z.returncode==0
 out['projects'][project]={'services':services,'database':[json.loads(x) for x in z.stdout.decode().splitlines() if x]}
 assert env.get('SOURCEID_PROTOCOL_ONLY')=='true' and env.get('SOURCEID_AUTO_BINDING_ENABLE')=='false'
out['established_80_8080_counts']=[]
for _ in range(3):
 raw=run(['ss','-Htn','state','established']).decode().splitlines()
 out['established_80_8080_counts'].append(sum(bool(re.search(r':(?:80|8080)\s',x)) for x in raw));time.sleep(2)
out['listener_lines']=[x for x in run(['ss','-Hltn']).decode().splitlines() if re.search(r':(?:80|8080|18090)\s',x)]
out['other_running_container_names']=run(['docker','ps','--format','{{.Names}}']).decode().splitlines()
out['disk_available_bytes']=os.statvfs('/home/aqyfzx/ai').f_bavail*os.statvfs('/home/aqyfzx/ai').f_frsize
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
print(json.dumps(out))
