"""Independent post-completion verification; no service mutation or auth."""
APP='knowhub-company-test-app-1';FRONT='knowhub-company-test-frontend-1';PG='knowhub-company-test-postgres-1'
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
record=json.loads((B/'reliable-deploy-result.json').read_text());assert record['result']=='PASS'
assert inspect(APP,'.Image')==record['app_image'] and inspect(APP,'.State.Health.Status')=='healthy'
assert json.loads(run(['docker','image','inspect',record['app_image']]))[0]['Config']['Labels']['org.opencontainers.image.revision']==record['app_revision']
assert inspect(FRONT,'.Image')==record['frontend_image']
assert json.loads(run(['docker','image','inspect',record['frontend_image']]))[0]['Config']['Labels']['org.opencontainers.image.revision']==record['frontend_revision']
assert inspect(APP,'.RestartCount')==0
q="SELECT json_build_object('schema',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations),'identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'policy_decisions',(SELECT COUNT(*) FROM source_id_link_decisions),'active_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()))"
db=json.loads(run(['docker','exec',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-c',q]));assert db['schema']==3030 and not db['dirty'] and all(db[k]==0 for k in ('identities','sessions','jit','policy_decisions','active_authorizations'))
flags=list(record['gates']);current=json.loads(run(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(flags)]));assert current==record['gates']
started=datetime.datetime.fromisoformat(inspect(APP,'.State.StartedAt')[:26]+'+00:00');age=(datetime.datetime.now(datetime.timezone.utc)-started).total_seconds()
out={'result':'PASS','app_revision':record['app_revision'],'app_image':record['app_image'],'frontend_revision':record['frontend_revision'],'database':db,'app_healthy':True,'restart_count':0,'observed_uptime_seconds':int(age),'gates_unchanged':True,'schema_commands_executed':0,'real_sid_requests':0,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(B/'reliable-final-readonly-result.json').write_text(json.dumps(out));print(json.dumps(out))
