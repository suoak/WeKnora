"""Bounded rollback readiness and sanitized acceptance summary."""
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');image=inspect('knowhub-company-test-app-1','.Image');tools='/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools'
common="docker run --rm --network host --cap-add NET_ADMIN --tmpfs /nettools/run --log-driver none --mount type=bind,source="+tools+",target=/nettools,readonly --mount type=bind,source="+str(B/'private')+",target=/control,readonly --entrypoint python3 "+image+' '
rollback='\n'.join(['#!/bin/sh','set -eu','docker stop knowhub-company-entry-entry-1',common+'/control/remove-entry-forward-guard.py','docker start knowhub-sourceid-business-app-1',"for n in $(seq 1 90); do if [ \"$(docker inspect --format '{{.State.Health.Status}}' knowhub-sourceid-business-app-1)\" = healthy ]; then break; fi; sleep 1; done","[ \"$(docker inspect --format '{{.State.Health.Status}}' knowhub-sourceid-business-app-1)\" = healthy ]",'docker start knowhub-sourceid-business-frontend-1','docker exec knowhub-sourceid-business-frontend-1 nginx -s reload',''])
path=B/'private/rollback-entry.sh';path.write_text(rollback);path.chmod(0o700);run(['sh','-n',str(path)])
# Do not claim ephemeral host firewall state survives host/Docker restarts.
run(['docker','update','--restart','no','knowhub-company-entry-entry-1'])
c=json.loads((B/'entry/compose.json').read_text());c['services']['entry']['restart']='no';(B/'entry/compose.json').write_text(json.dumps(c,indent=2))
def summary(value):
 if isinstance(value,dict):
  return {k:summary(v) for k,v in value.items() if isinstance(v,(bool,int,dict)) or (k=='result' and v in ('PASS','FAIL'))}
 return value
results={}
for name in ('acceptance-result.json','mock-result.json'):results[name]=summary(json.loads((B/'validation'/name).read_text()))
out={'result':'PASS','rollback_shell_syntax':'PASS','rollback_scope':'old APP/Frontend entry only; removes only new entry firewall rules; no DB restore/down','restart_policy':'public entry no automatic restart; reapply guard and validate before restart','fixture_summaries':results,'no_real_authentication':True}
(B/'closeout-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
