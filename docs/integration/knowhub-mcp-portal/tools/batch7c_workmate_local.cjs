// Existing user-configured local WorkMate API; no app/profile/credentials changes.
const fs=require('fs'),path=require('path');
const root=path.resolve(__dirname,'../../../..');
const runtime=JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-b7c-clients/connection-runtime.json','utf8'));
let backendPort,localApp;const scenario='positive';
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/published/clients');
const record={harness_sha256:require('crypto').createHash('sha256').update(fs.readFileSync(__filename)).digest('hex'),client:'CSBU WorkMate',local_existing_configured_profile:true,candidate:runtime.candidate,started_utc:new Date().toISOString()};
let createdKey;
async function knowhub(method,url,body){const r=await fetch(runtime.app_url+url,{method,headers:{Authorization:'Bearer '+runtime.user_jwt,'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});return {status:r.status,body:await r.json()};}
(async()=>{try{
 const {_electron}=require(path.join(root,'frontend/node_modules/playwright'));const env={};for(const k of ['PATH','SystemRoot','WINDIR','COMSPEC','TEMP','TMP','APPDATA','LOCALAPPDATA','USERPROFILE','HOME','ProgramFiles','ProgramFiles(x86)'])if(process.env[k])env[k]=process.env[k];
 localApp=await _electron.launch({executablePath:'E:/Program Files/csbuwork/CSBU WorkMate/CSBU WorkMate.exe',env,timeout:60000});
 const localPage=await localApp.firstWindow();await localPage.waitForTimeout(2000);backendPort=await localPage.evaluate(()=>window.__backendPort);record.local_application=await localApp.evaluate(({app})=>({version:app.getVersion(),userData:app.getPath('userData')}));
 createdKey=await knowhub('POST','/api/v1/mcp-api-keys',{name:'synthetic-local-workmate-b7',client_type:'workmate',capabilities:['retrieve','enterprise_retrieve'],tenant_scopes:[{tenant_id:1,kb_scope_mode:'all',knowledge_bases:[]}],expires_at_unix:Math.floor(Date.now()/1000)+1200});
 if(!createdKey.body?.data?.token)throw Error('test Key create '+createdKey.status);
 const token=createdKey.body.data.token;runtime.token=token;runtime.url=runtime.tunnel_url||runtime.url;
 record.run=await (async({connection,token,scenario})=>{
   const base='http://127.0.0.1:'+backendPort;
   async function api(method,url,data){const r=await fetch(base+url,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data),signal:AbortSignal.timeout(30000)});const text=await r.text();let body;try{body=JSON.parse(text)}catch{body={text:text.slice(0,1000)}};return {status:r.status,body};}
   const server=await api('POST','/api/mcp/servers',{name:'knowhub_batch7_synthetic',transport:{type:'http',url:connection.url,headers:{Authorization:'Bearer '+token}}});
   const id=server.body?.data?.id;if(!id)return {stage:'create_mcp',response:server};
   if(!server.body.data.enabled)await api('POST',`/api/mcp/servers/${id}/toggle`,{enabled:true});
   const tested=await api('POST','/api/mcp/test-connection',{id,name:'knowhub_batch7_synthetic',transport:{type:'http',url:connection.url,headers:{Authorization:'Bearer '+token}}});
   const workspace='E:/AIData/Codex/knowhub-b7c-clients/empty-workspace';
   const providers=await api('GET','/api/providers');const provider=(providers.body.data||[]).find(p=>p.enabled&&p.models?.includes('glm-5.2'));if(!provider)throw Error('configured glm-5.2 provider unavailable');
   const created=await api('POST','/api/conversations',{type:'aionrs',name:'KnowHub batch7 synthetic MCP acceptance',model:{provider_id:provider.id,model:'glm-5.2'},extra:{workspace,selected_mcp_server_ids:[id],selected_session_mcp_servers:[{id,name:'knowhub_batch7_synthetic',transport:{type:'streamable_http',url:connection.url,headers:{Authorization:'Bearer '+token}}}]}});
   const conv=created.body?.data?.id;if(!conv)return {stage:'create_conversation',response:created,server_id:id};
   const events=[];const ws=new WebSocket(base.replace('http:','ws:')+'/ws');ws.onmessage=e=>{try{const v=JSON.parse(e.data);if(v.name==='message.stream'&&v.data?.conversation_id===conv)events.push(v);}catch{}};
   await new Promise(resolve=>{ws.onopen=resolve;setTimeout(resolve,3000)});
   let prompt='The attached KnowHub MCP tools are already loaded as immediate tools, not deferred. Do not call ToolSearch. Invoke the actual available MCP tool names directly, using their supplied schema. Use only the attached KnowHub MCP. No shell, files, edits or alternative data sources. Call list_knowledge_bases, then search_knowledge query PUBLIC mode hybrid, grep_chunks pattern PUBLIC, list_documents knowledge_base_id public-kb, and read_document knowledge_id public-doc. Also attempt read_document knowledge_id sensitive and search_knowledge scoped to private-kb; report actual denials. Confirm private-home, public-two, and public-kb from actual results. Never output credentials.';
   if(scenario==='other')prompt='Use only attached KnowHub MCP. Call list_knowledge_bases and search_knowledge query PUBLIC mode keyword; attempt read_document knowledge_id private-home-doc and report actual denial. No shell/files/edits.';
   if(scenario==='withdrawn')prompt='Use only attached KnowHub MCP. Call list_knowledge_bases and search_knowledge query PUBLIC mode keyword; attempt read_document knowledge_id public-doc and report actual denial after Portal withdrawal. Read private-home-doc to confirm independent private access. No shell/files/edits.';
   if(scenario==='revoked')prompt='Use only attached KnowHub MCP. Attempt list_knowledge_bases and report actual authentication error for this revoked Key. No shell/files/edits or alternative data.';
   const ensured={local_configured_provider:'New API',model:'glm-5.2'};
   const selectedModel={source:'existing user provider; no credential or global settings changed'};
   const sent=await api('POST',`/api/conversations/${conv}/messages`,{content:prompt});
   const confirmations=[],done=new Set();let terminal=null;
   for(let i=0;i<180;i++){
     const pending=await api('GET',`/api/conversations/${conv}/confirmations`);
     for(const c of pending.body?.data||[]){
       const safe=c.action==='ToolSearch'||['list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'].some(n=>c.action?.endsWith(n));
       const choice=(c.options||[]).find(o=>o.value==='proceed_once'||o.value?.value==='proceed_once');
       if(safe&&choice&&!done.has(c.call_id)){done.add(c.call_id);const response=await api('POST',`/api/conversations/${conv}/confirmations/${c.call_id}/confirm`,{msg_id:c.id,data:typeof choice.value==='object'?{...choice.value,option_id:'allow'}:{option_id:'allow',value:choice.value}});confirmations.push({call_id:c.call_id,action:c.action,status:response.status});}
     }

     for(const frame of events){const f=frame.data;if(f.type==='finish'||f.type==='error')terminal=f.type;
       if(String(f.type).includes('permission')){const call=f.data?.tool_call?.tool_call_id||f.data?.call_id||f.data?.request_id||f.msg_id;const options=f.data?.options||[];
         // Only approve the five read-only KnowHub tools; never approve shell.
         const summary=JSON.stringify(f);const safe=['list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'].some(x=>summary.includes(x))&&!/exec_command|shell_command|write_file|apply_patch/.test(summary);
         if(call&&!done.has(call)&&safe){const choice=options.find(o=>o.kind==='allow_once')||options.find(o=>o.optionId==='allow_once');if(choice){done.add(call);const result=await api('POST',`/api/conversations/${conv}/confirmations/${call}/confirm`,{msg_id:f.msg_id,data:choice.optionId||choice});confirmations.push({call_id:call,status:result.status});}}
       }}
     if(terminal)break;await new Promise(r=>setTimeout(r,1000));
   }
   const snapshot=await api('GET',`/api/conversations/${conv}`);
   const messages=await api('GET',`/api/conversations/${conv}/messages?limit=100`);ws.close();
   return {stage:'conversation',server_id:id,conversation_id:conv,sent_status:sent.status,terminal,confirmations,events,messages,tested,snapshot,ensured,selectedModel};
 })({connection:runtime,token,scenario});
 record.actual_tools_call=(record.run.events||[]).some(e=>/tool_call/.test(e.data?.type||'')&&['list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'].some(n=>String(e.data?.data?.name||e.data?.data?.tool_name||'').endsWith(n)));
 record.exit_code=record.run.terminal==='finish'&&record.actual_tools_call?0:1;
}catch(e){record.exit_code=1;record.error=String(e)}finally{
 if(createdKey?.body?.data?.id)record.test_key_revoke=(await knowhub('DELETE','/api/v1/mcp-api-keys/'+createdKey.body.data.id)).status;
 if(record.run?.server_id){const r=await fetch('http://127.0.0.1:'+backendPort+'/api/mcp/servers/'+record.run.server_id,{method:'DELETE'});record.synthetic_mcp_entry_cleanup=r.status;}
 if(localApp)await localApp.close().catch(()=>{});
 record.finished_utc=new Date().toISOString();let raw=JSON.stringify(record,null,2);for(const v of Object.values(runtime))if(typeof v==='string'&&(v.startsWith('mcp_')||v.startsWith('eyJ')))raw=raw.replaceAll(v,'[REDACTED]');fs.writeFileSync(path.join(out,'workmate-local-configured-attempt8.json'),raw);console.log(JSON.stringify({exit_code:record.exit_code,tools:record.actual_tools_call,error:record.error,terminal:record.run?.terminal}));process.exitCode=record.exit_code;
}})();
