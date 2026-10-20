"""Read-only gate for explicitly approved APP-only release."""
import yaml
live=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
project='knowhub-sourceid-business'
ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
containers={x['service']:x for x in (metadata(cid) for cid in ids)}
app=containers['app'];front=containers['frontend'];pg=containers['postgres']
assert app['working_dir']==str(live) and app['image_id']=='sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
assert front['image_id']=='sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
images={}
for name,iid,revision in [('app',app['image_id'],'431226c3b1bd5338ec06c1bd0520e637d5fed398'),('frontend',front['image_id'],'1ba24ef5f9a730bb98d9ade6a5a5d141ea49292b'),('candidate','sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286','035f35e1d388a2f02615d35cf826bbdcc22e56b0')]:
    actual=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',iid]).decode().strip()
    assert actual==revision;images[name]={'id':iid,'revision':actual}
flags={}
for key in ('SOURCEID_BASIC_ENCODING','SOURCEID_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_LOGOUT_ENABLE','OIDC_AUTH_ENABLE','SOURCEID_DIAGNOSTICS_ENABLE','OIDC_AUTH_PKCE_ENABLE','AUTO_MIGRATE'):
    value=run(['docker','exec',app['id'],'sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode()
    assert value in ('true','false','external','oauth_form','NOT_SET');flags[key]=value
assert flags['SOURCEID_BASIC_ENCODING']=='oauth_form' and flags['SOURCEID_PROTOCOL_ONLY']=='true'
for key in ('SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_LOGOUT_ENABLE','AUTO_MIGRATE'):assert flags[key]=='false'
assert flags['SOURCEID_SESSION_MODE'] in ('NOT_SET','external')
q="BEGIN READ ONLY; SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations; SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'tenants',(SELECT COUNT(*) FROM tenants),'members',(SELECT COUNT(*) FROM tenant_members),'identities',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions)); ROLLBACK;"
p=subprocess.run(['docker','exec','-i',pg['id'],'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
assert p.returncode==0
db=[json.loads(x) for x in p.stdout.decode().splitlines() if x];assert db[0]=={'version':3027,'dirty':False}
assert db[1]['identities']==db[1]['reviews']==db[1]['sessions']==db[1]['local_sessions']==0
files=[live/'compose.yml',pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b/candidate-image-pin.yml'),pathlib.Path('/home/aqyfzx/ai/knowhub-oa-ui-candidate-1ba24ef5f/frontend-ui-pin.yml')]
assert front['compose_files'].split(',')==list(map(str,files))
cmd=['docker','compose','--project-name',project,'--project-directory',str(live)]
for path in files:cmd+=['-f',str(path)]
config=json.loads(run(cmd+['config','--format','json']))
assert config['services']['app']['image']==app['image_id']
assert config['services']['frontend']['image']==front['image_id']
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','result':'PASS','containers':containers,'images':images,'flags':flags,'database':db,'compose_files_in_order':list(map(str,files)),'read_only':True}))
