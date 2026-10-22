// Real installed WorkMate, isolated profile, real conversation/runtime and MCP.
// Never patches the client or calls KnowHub directly to substitute tool calls.
const fs=require('node:fs'),path=require('node:path');
const root=path.resolve(__dirname,'../../../..'),{_electron}=require(path.join(root,'frontend/node_modules/playwright'));
const runtime=JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-b7c-clients/connection-runtime.json','utf8'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/published/clients');fs.mkdirSync(out,{recursive:true});
const scenario=process.argv[2]||'positive';if(!['positive','other','withdrawn','revoked'].includes(scenario))throw Error('scenario');
const attempt=process.argv[3]||'';if(!['','-attempt2'].includes(attempt))throw Error('attempt');
const token=scenario==='other'?runtime.other_token:runtime.token;
const env={};for(const k of ['PATH','SystemRoot','WINDIR','COMSPEC','TEMP','TMP','APPDATA','LOCALAPPDATA','USERPROFILE','HOME','ProgramFiles','ProgramFiles(x86)','NUMBER_OF_PROCESSORS','PROCESSOR_ARCHITECTURE'])if(process.env[k])env[k]=process.env[k];
env.PATH='C:/Users/admin/AppData/Local/Programs/OpenAI/Codex/bin;'+env.PATH;
Object.assign(env,{CSBU_WORKMATE_E2E_TEST:'1',CSBU_WORKMATE_E2E_USER_DATA_DIR:'E:/AIData/Codex/knowhub-b7c-clients/workmate-isolated-profile',CSBU_WORKMATE_DISABLE_AUTO_UPDATE:'1'});
const record={client:'CSBU WorkMate',candidate:runtime.candidate,scenario,started_utc:new Date().toISOString(),installed_application:true,actual_tools_call:'REQUIRES_SERVER_PROTOCOL_REVIEW',isolated_profile:true};let app;
(async()=>{try{
 app=await _electron.launch({executablePath:'E:/Program Files/csbuwork/CSBU WorkMate/CSBU WorkMate.exe',env,timeout:120000});
 const page=await app.firstWindow({timeout:120000});await page.waitForLoadState('domcontentloaded');
 record.app=await app.evaluate(({app,BrowserWindow})=>{for(const w of BrowserWindow.getAllWindows())w.hide();return {version:app.getVersion(),userData:app.getPath('userData')}});
 await page.waitForTimeout(3000);
 record.run=await page.evaluate(async({connection,token,scenario})=>{
   const base='http://127.0.0.1:'+window.__backendPort;
   async function api(method,url,data){const r=await fetch(base+url,{method,headers:{'Content-Type':'application/json'},body:data===undefined?undefined:JSON.stringify(data),signal:AbortSignal.timeout(30000)});const text=await r.text();let body;try{body=JSON.parse(text)}catch{body={text:text.slice(0,1000)}};return {status:r.status,body};}
   const server=await api('POST','/api/mcp/servers',{name:'knowhub-b7c-'+scenario,transport:{type:'http',url:connection.url,headers:{Authorization:'Bearer '+token}}});
   const id=server.body?.data?.id;if(!id)return {stage:'create_mcp',response:server};
   if(!server.body.data.enabled)await api('POST',`/api/mcp/servers/${id}/toggle`,{enabled:true});
   const tested=await api('POST','/api/mcp/test-connection',{id,name:'knowhub-b7c-'+scenario,transport:{type:'http',url:connection.url,headers:{Authorization:'Bearer '+token}}});
   const workspace='E:/AIData/Codex/knowhub-b7c-clients/empty-workspace';
   const created=await api('POST','/api/conversations',{type:'acp',extra:{workspace,backend:'codex',current_model_id:'gpt-5.6-sol',selected_mcp_server_ids:[id]}});
   const conv=created.body?.data?.id;if(!conv)return {stage:'create_conversation',response:created,server_id:id};
   const events=[];const ws=new WebSocket(base.replace('http:','ws:')+'/ws');ws.onmessage=e=>{try{const v=JSON.parse(e.data);if(v.name==='message.stream'&&v.data?.conversation_id===conv)events.push(v);}catch{}};
   await new Promise(resolve=>{ws.onopen=resolve;setTimeout(resolve,3000)});
   let prompt='Use only the attached KnowHub MCP. No shell, files, edits or alternative data sources. Call list_knowledge_bases, then search_knowledge query PUBLIC mode hybrid, grep_chunks pattern PUBLIC, list_documents knowledge_base_id public-kb, and read_document knowledge_id public-doc. Also attempt read_document knowledge_id sensitive and search_knowledge scoped to private-kb; report actual denials. Confirm private-home, public-two, and public-kb from actual results. Never output credentials.';
   if(scenario==='other')prompt='Use only attached KnowHub MCP. Call list_knowledge_bases and search_knowledge query PUBLIC mode keyword; attempt read_document knowledge_id private-home-doc and report actual denial. No shell/files/edits.';
   if(scenario==='withdrawn')prompt='Use only attached KnowHub MCP. Call list_knowledge_bases and search_knowledge query PUBLIC mode keyword; attempt read_document knowledge_id public-doc and report actual denial after Portal withdrawal. Read private-home-doc to confirm independent private access. No shell/files/edits.';
   if(scenario==='revoked')prompt='Use only attached KnowHub MCP. Attempt list_knowledge_bases and report actual authentication error for this revoked Key. No shell/files/edits or alternative data.';
   const ensured=await api('POST',`/api/conversations/${conv}/runtime/ensure`,{});
   const selectedModel=await api('PUT',`/api/conversations/${conv}/config-options/model`,{value:'gpt-5.6-sol'});
   const sent=await api('POST',`/api/conversations/${conv}/messages`,{content:prompt});
   const confirmations=[],done=new Set();let terminal=null;
   for(let i=0;i<180;i++){
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
 },{connection:runtime,token,scenario});
 record.actual_tools_call=(record.run?.events||[]).some(e=>/tool_call/.test(e.data?.type||'')&&/list_knowledge_bases|search_knowledge|grep_chunks|list_documents|read_document/.test(JSON.stringify(e)));record.exit_code=record.run?.terminal==='finish'&&record.actual_tools_call?0:1;
 await page.screenshot({path:path.join(out,`workmate-${scenario}${attempt}.png`)});
}catch(e){record.exit_code=1;record.error=String(e)}finally{if(app)await app.close().catch(()=>{});record.finished_utc=new Date().toISOString();let raw=JSON.stringify(record,null,2);for(const secret of [runtime.token,runtime.other_token,runtime.owner_jwt,runtime.reviewer_jwt,runtime.user_jwt].filter(Boolean))raw=raw.replaceAll(secret,'[REDACTED]');fs.writeFileSync(path.join(out,`workmate-${scenario}${attempt}.json`),raw);console.log(JSON.stringify({scenario,attempt,exit_code:record.exit_code,actual_tools_call:record.actual_tools_call,stage:record.run?.stage,terminal:record.run?.terminal,events:record.run?.events?.length}));process.exitCode=record.exit_code;}})();
