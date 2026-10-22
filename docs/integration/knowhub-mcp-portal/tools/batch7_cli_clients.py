"""Real installed clients; runtime credentials never written to evidence."""
from pathlib import Path
import datetime,json,os,subprocess,sys
ROOT=Path(__file__).resolve().parents[4];OUT=ROOT/'docs/integration/knowhub-mcp-portal/evidence/batch7/clients';OUT.mkdir(exist_ok=True)
RUNTIME=Path('E:/AIData/Codex/knowhub-b7-clients');connection=json.loads((RUNTIME/'connection-runtime.json').read_text());name=sys.argv[1];scenario=sys.argv[2] if len(sys.argv)>2 else 'positive';assert scenario in ('positive','other','withdrawn','revoked');token=connection['other_token'] if scenario=='other' else connection['token'];log_name=name+'-'+scenario
env=dict(os.environ,KNOWHUB_M7_CLIENT_TOKEN=token)
work=RUNTIME/'empty-workspace';work.mkdir(exist_ok=True)
prompt='Use only the knowhub MCP server, with no shell, file access, or code edits. Call list_knowledge_bases. Then call search_knowledge with query PUBLIC and keyword mode, using its exact tool schema. Call read_document for public-doc. Report the KB IDs, owner tenants and document references returned by the actual tools. Do not infer success without tool results. Never output credentials.'
if scenario=='positive':prompt+=' Also call grep_chunks with pattern PUBLIC (use the schema), list_documents for public-kb, and attempt read_document for sensitive and search_knowledge scoped to private-kb; report authorization errors honestly. Confirm private-home, public-two and public-kb are present. Do not invoke unavailable tools.'
if scenario=='other':prompt='Use only knowhub MCP. Call list_knowledge_bases, search_knowledge query PUBLIC mode keyword, and read_document knowledge_id private-home-doc. This is employee C. Report actual tools results and the expected denial of employee A private-home, with no credentials or shell/file operations.'
if scenario=='withdrawn':prompt='Use only knowhub MCP. Call list_knowledge_bases then search_knowledge query PUBLIC mode keyword, then read_document knowledge_id public-doc and read_document knowledge_id private-home-doc. Space tenant 99 has been withdrawn from Portal. Report actual public-doc denial and independently retained private-home access, with no credentials or shell/file operations.'
if scenario=='revoked':prompt='Use only knowhub MCP. Attempt list_knowledge_bases. This synthetic Key has been revoked. Report the actual bounded initialization or tool authorization failure; do not use shell or any alternative data path.'
if name=='codex':
 argv=['C:/Users/admin/AppData/Local/Programs/OpenAI/Codex/bin/codex.exe','exec','--ignore-user-config','--ephemeral','--json','--skip-git-repo-check','-s','read-only','-c','approval_policy="never"','-C',str(work),'-c','mcp_servers.knowhub.default_tools_approval_mode="approve"','-c','mcp_servers.knowhub.required=true','-c','mcp_servers.knowhub.url='+json.dumps(connection['url']),'-c','mcp_servers.knowhub.bearer_token_env_var="KNOWHUB_M7_CLIENT_TOKEN"',prompt]
elif name=='claude':
 config=RUNTIME/'claude-mcp.json';config.write_text(json.dumps({'mcpServers':{'knowhub':{'type':'http','url':connection['url'],'headers':{'Authorization':'Bearer ${KNOWHUB_M7_CLIENT_TOKEN}'}}}}),encoding='utf8')
 argv=['C:/Users/admin/.local/bin/claude.exe','-p',prompt,'--mcp-config',str(config),'--strict-mcp-config','--no-session-persistence','--setting-sources','','--tools','','--allowedTools','mcp__knowhub__list_knowledge_bases','mcp__knowhub__search_knowledge','mcp__knowhub__read_document','--output-format','stream-json','--verbose']
else:raise ValueError('unsupported installed client')
start=datetime.datetime.now(datetime.timezone.utc).isoformat()
try:
 result=subprocess.run(argv,cwd=work,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180);code=result.returncode;raw=result.stdout
except subprocess.TimeoutExpired as e:code=124;raw=e.output or b''
text=raw.decode('utf8','replace').replace(token,'[SYNTHETIC_KEY_REDACTED]');(OUT/(log_name+'.log')).write_text(text,encoding='utf8')
record={'client':name,'scenario':scenario,'argv':argv,'candidate_sha256':connection['candidate'],'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'exit_code':code,'tool_result_pass':'REQUIRES_PROTOCOL_AND_LOG_REVIEW','credential_logged':False}
(OUT/(log_name+'-result.json')).write_text(json.dumps(record,indent=2),encoding='utf8');print(json.dumps({'client':name,'exit_code':code,'log':str(OUT/(log_name+'.log'))}))
