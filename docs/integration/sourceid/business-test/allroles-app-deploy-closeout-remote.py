"""Post-deployment read-only verification. Never starts OIDC authorization."""
root=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d')
receipt=json.loads((root/'app-deployment-result.json').read_text());assert receipt['result']=='PASS' and not receipt['rolled_back']
ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project=knowhub-sourceid-business','--format','{{.ID}}']).decode().split()
current={x['service']:x for x in (metadata(cid) for cid in ids)}
app=current['app'];front=current['frontend'];assert app['id']==receipt['containers_after']['app']['id']
assert app['image_id']=='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286' and app['health']=='healthy'
assert front['id']=='7b624ec2ee904cb7a0ab86458954722bae831f20f1f3c9da54c7ca3f5e53c448'
revision=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',app['image_id']]).decode().strip();assert revision=='035f35e1d388a2f02615d35cf826bbdcc22e56b0'
flags={}
for key in receipt['flags_before']:
    value=run(['docker','exec',app['id'],'sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode()
    assert value in ('true','false','oauth_form','external','NOT_SET');flags[key]=value
assert flags==receipt['flags_before']
q="BEGIN READ ONLY; SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations; SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'tenants',(SELECT COUNT(*) FROM tenants),'members',(SELECT COUNT(*) FROM tenant_members),'identities',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions),'active_knowledge_bases',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'active_mcp_endpoints',(SELECT COUNT(*) FROM mcp_endpoints WHERE deleted_at IS NULL)); ROLLBACK;"
p=subprocess.run(['docker','exec','-i',current['postgres']['id'],'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert p.returncode==0
db=[json.loads(line) for line in p.stdout.decode().splitlines() if line];assert db[0]=={'version':3027,'dirty':False}
assert db[1]['identities']==db[1]['reviews']==db[1]['sessions']==db[1]['local_sessions']==0
assert db[1]['users']==2 and db[1]['tenants']==1 and db[1]['members']==2 and db[1]['active_knowledge_bases']==db[1]['active_mcp_endpoints']==0
checks={}
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for name,url in [('frontend_80','http://127.0.0.1/'),('app_8080','http://127.0.0.1:8080/health'),('proxy_oidc_config','http://127.0.0.1/api/v1/auth/oidc/config')]:
    with opener.open(url,timeout=5) as r:
        checks[name]={'status':r.status};assert r.status==200
        if name=='proxy_oidc_config':
            data=json.loads(r.read());assert data['sourceid_basic_encoding']=='oauth_form'
            checks[name].update({k:data.get(k) for k in ('enabled','sourceid_basic_encoding','redirect_uri')})
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'result':'PASS','app_container_id':app['id'],'app_image_id':app['image_id'],'app_revision':revision,'frontend_container_id':front['id'],'frontend_revision':'1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b','flags':flags,'database':db,'health':checks,'real_sid_login':False,'legacy_database_reused':False,'database_migration_executed':False}))
