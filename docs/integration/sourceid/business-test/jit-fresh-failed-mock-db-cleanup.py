R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');v=json.loads((R/'mock-result.json').read_text());assert v['result']=='FAIL'
PG='knowhub-jit-fresh-postgres-1'
assert not run(['docker','ps','-aq','--filter','name=knowhub-jit-fresh-mock-']).strip()
for DB in ('jit_mock_new','jit_mock_existing'):
 check=run(['docker','exec',PG,'psql','-X','-qAt','-U','fresh_owner','-d','postgres','-c',"SELECT COUNT(*) FROM pg_stat_activity WHERE datname='"+DB+"'"]).decode().strip();assert check=='0'
 run(['docker','exec',PG,'dropdb','--if-exists','-U','fresh_owner',DB])
print(json.dumps({'result':'FAILED_SYNTHETIC_CLONE_REMOVED','fresh_real_db_unchanged':True,'original_db_unchanged':True}))
