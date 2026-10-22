"""Run the installed Codex using exactly the copied V1 UI configuration."""
from pathlib import Path
import datetime,json,os,re,subprocess

root=Path(__file__).resolve().parents[4]
c=json.loads((root/'docs/integration/knowhub-mcp-portal/evidence/feishu-joint/current.json').read_text())
private=Path('E:/AIData/Codex/knowhub-v1-feishu-private')/c['candidate_sha256'][:16]
runtime=json.loads((private/'client.json').read_text())
template=(private/'copied-codex.toml').read_text(encoding='utf8')
url=json.loads(re.search(r'^url = (.+)$',template,re.M)[1])
variable=re.search(r'^bearer_token_env_var = "([A-Z_]+)"$',template,re.M)[1]
assert variable=='KNOWHUB_MCP_KEY'and url==runtime['url']+'/api/v1/mcp/personal'
assert runtime['token']not in template
work=private/'empty-workspace';work.mkdir(exist_ok=True)
prompt='Use only the knowhub MCP server, no shell, files or edits. Call all five tools: list_knowledge_bases, search_knowledge query PUBLIC mode hybrid limit 30, grep_chunks query PUBLIC, list_documents for public-kb, read_document for public-doc. Verify actual results include public-kb, public-two, private-home and private-other, and ordinary Office text remains readable with URL destinations removed. Attempt read_document knowledge_id sensitive and report the actual denial. Never output credentials. Do not infer results without tool calls.'
argv=['C:/Users/admin/AppData/Local/Programs/OpenAI/Codex/bin/codex.exe','exec','--ignore-user-config','--ephemeral','--json','--skip-git-repo-check','-s','read-only','-c','approval_policy="never"','-C',str(work)]
# Interpret the two exact UI TOML settings, without constructing a different
# endpoint or putting a token in CLI args/config. Execution approvals are client
# test controls; the APP's authorization and five-tool whitelist remain intact.
for line in template.splitlines():
    if line.startswith(('url = ','bearer_token_env_var = ')):argv+=['-c','mcp_servers.knowhub.'+line]
argv+=['-c','mcp_servers.knowhub.required=true','-c','mcp_servers.knowhub.default_tools_approval_mode="approve"',prompt]
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
try:
    p=subprocess.run(argv,cwd=work,env=dict(os.environ,**{variable:runtime['token']}),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=240);code=p.returncode;raw=p.stdout.decode('utf8','replace')
except subprocess.TimeoutExpired as e:code=124;raw=(e.output or b'').decode('utf8','replace')
for secret in [runtime.get('token'),runtime.get('old_token'),runtime['login'].get('token'),runtime.get('admin_jwt'),runtime.get('owner_jwt')]:
    if secret:raw=raw.replace(secret,'[REDACTED_SYNTHETIC_CREDENTIAL]')
parent=root/'docs/integration/knowhub-mcp-portal/evidence/feishu-joint'/('codex-'+c['candidate_sha256'][:16]);parent.mkdir(parents=True,exist_ok=True)
out=parent/('attempt-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'));out.mkdir(exist_ok=False)
(out/'codex.log').write_text(raw,encoding='utf8')
calls=[]
for line in raw.splitlines():
    try:event=json.loads(line)
    except ValueError:continue
    item=event.get('item',{})
    if event.get('type')=='item.completed'and item.get('type')=='mcp_tool_call':calls.append({'tool':item.get('tool'),'status':item.get('status'),'error':item.get('error')})
passed=code==0 and {'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'}<={x['tool']for x in calls if x['status']=='completed'}
result={'candidate_sha256':c['candidate_sha256'],'exit_code':code,'tool_gate_pass':passed,'actual_completed_calls':calls,'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'copied_UI_template_used':True,'credential_in_argv':False,'synthetic_data':True}
(out/'result.json').write_text(json.dumps(result,indent=2),encoding='utf8');print(json.dumps(result))
raise SystemExit(0 if passed else 1)
