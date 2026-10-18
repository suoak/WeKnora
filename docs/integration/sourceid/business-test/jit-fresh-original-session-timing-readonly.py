import sys
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window';sys.path.insert(0,str(W))
from runtime import jq
before=json.loads((W/'original-baseline.json').read_text());known={x['row']['id'] for x in before['source_id_sessions']}
uid=json.loads(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/first-account-preparation/selected-account.json').read_text())['existing_user_id']
rows=jq("SELECT COALESCE(json_agg(json_build_object('id',id,'original_user',user_id='"+uid+"','created_at',created_at,'revoked',revoked,'active',NOT revoked AND expires_at>NOW())),'[]') FROM source_id_sessions",False)
extra=[{k:v for k,v in x.items() if k!='id'} for x in rows if x['id'] not in known]
print(json.dumps({'result':'READ_ONLY','recovery_completed_utc':json.loads((W/'status.json').read_text())['utc'],'new_original_sessions':extra,'original_users_or_bindings_deleted':False,'real_sid_requests_by_agent':0}))
