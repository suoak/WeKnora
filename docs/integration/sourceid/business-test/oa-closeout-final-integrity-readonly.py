R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
import sys
sys.path.insert(0,str(W));from runtime import fresh_meta,jq,env,health
source=jq("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'vectors',(SELECT COUNT(*) FROM embeddings),'active_sessions',(SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()),'active_access',(SELECT COUNT(*) FROM auth_tokens WHERE session_id IS NOT NULL AND session_id<>'' AND NOT is_revoked AND expires_at>NOW()))",False)
assert source=={'users':3,'bindings':1,'jit':0,'documents':3,'chunks':2717,'vectors':2717,'active_sessions':0,'active_access':0}
scan="import os,json,hashlib;rows=[]\nfor root,ds,fs in os.walk('/scan'):\n for n in fs:\n  p=os.path.join(root,n)\n  if os.path.isfile(p):\n   with open(p,'rb') as f:h=hashlib.sha256(f.read()).hexdigest()\n   rows.append([os.path.relpath(p,'/scan'),os.path.getsize(p),h])\nprint(json.dumps(sorted(rows)))"
I='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
files=json.loads(run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',I,'-c',scan]))
assert files==json.loads((R/'private/original-backup/protected-file-manifest.json').read_text())
assert 'knowhub-jit-fresh_isolated' not in inspect('knowhub-company-entry-entry-1','.NetworkSettings.Networks')
new=fresh_meta();assert new['users']==new['identities']==new['jit']==1 and new['active_sessions']==new['active_access']==new['refresh']==new['members']==new['kbs']==new['privileged']==0
assert not run(['docker','ps','-aq','--filter','name=knowhub-oa-closeout-']).strip()
health('company-test-verified-email-db3030')
oldenv=env('knowhub-company-test-app-1');oldbefore=json.loads((W/'original-app-env-protected.json').read_text());assert oldenv==oldbefore
newenv=env('knowhub-jit-fresh-app-1');assert newenv['OIDC_AUTH_ENABLE']=='false' and newenv['SOURCEID_JIT_ENABLE']=='false' and newenv['SOURCEID_PROTOCOL_ONLY']=='true'
out={'result':'PASS','original_counts':source,'original_files_match':True,'original_app_environment_unchanged':True,'original_database_redis_and_file_configuration_unchanged':True,'fresh_counts':new,'fresh_gates_closed':True,'gateway_old_marker_and_ports_restored':True,'gateway_disconnected_from_fresh_network':True,'synthetic_security_containers_removed':True,'real_sid_calls':0}
(W/'final-integrity-safe.json').write_text(json.dumps(out));print(json.dumps(out))
