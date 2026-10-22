// Reuse the existing isolated user/API and published scope. No client patches.
const fs=require('node:fs'),path=require('node:path'),{pathToFileURL}=require('node:url'),{spawn}=require('node:child_process');
const root=path.resolve(__dirname,'../../../..'),front=path.join(root,'frontend'),file='E:/AIData/Codex/knowhub-b7c-clients/connection-runtime.json';
const runtime=JSON.parse(fs.readFileSync(file,'utf8'));let server;
const record={candidate:runtime.candidate,real_existing_key_api:true,model_selection:'gpt-5.6-sol advertised by actual WorkMate handshake',started_utc:new Date().toISOString()};
(async()=>{try{
 async function api(method,p,token,body){const r=await fetch(runtime.app_url+p,{method,headers:{'Content-Type':'application/json',Authorization:'Bearer '+token},body:body?JSON.stringify(body):undefined,signal:AbortSignal.timeout(30000)});return {status:r.status,body:await r.json()};}
 const published=await api('POST','/api/v1/system/admin/portal/spaces/99/publish',runtime.reviewer_jwt);if(published.status!==200)throw Error('publish '+published.status);record.publish_status=published.status;
 const created=await api('POST','/api/v1/mcp-api-keys',runtime.user_jwt,{name:'synthetic-workmate-supported-model-retry',client_type:'workmate',capabilities:['retrieve','enterprise_retrieve'],expires_at_unix:Math.floor(Date.now()/1000)+1200,tenant_scopes:[{tenant_id:1,kb_scope_mode:'all',knowledge_bases:[]}]});
 if(!created.body?.data?.token)throw Error('create key '+created.status);
 record.create_key_status=created.status;record.key_id=created.body.data.id;
 runtime.token=created.body.data.token;runtime.tunnel_url=runtime.tunnel_url||runtime.url;runtime.url='http://127.0.0.1:14881/api/v1/mcp/personal';fs.writeFileSync(file,JSON.stringify(runtime));
 process.env.VITE_DEV_PROXY_TARGET=runtime.app_url;
 const vite=await import(pathToFileURL(path.join(front,'node_modules/vite/dist/node/index.js')).href);
 server=await vite.createServer({root:front,configFile:path.join(front,'vite.config.ts'),configLoader:'runner',server:{host:'127.0.0.1',port:14881,strictPort:true,open:false,proxy:{'/api/v1/mcp/personal':{target:new URL(runtime.tunnel_url).origin,changeOrigin:true},'/api':{target:runtime.app_url,changeOrigin:true}}}});await server.listen();
 await new Promise(resolve=>{const p=spawn('node',[path.join(__dirname,'batch7c_workmate_chat.cjs'),'positive','-attempt2'],{cwd:root,windowsHide:true,stdio:['ignore','pipe','pipe']});p.stdout.on('data',d=>process.stdout.write(d));p.stderr.on('data',d=>process.stderr.write(d));p.on('close',code=>{record.wrapper_exit=code;resolve()})});
 const revoked=await api('DELETE','/api/v1/mcp-api-keys/'+record.key_id,runtime.user_jwt);record.key_revoke_status=revoked.status;
 record.exit_code=record.wrapper_exit;
}catch(e){record.exit_code=1;record.error=String(e)}finally{if(server)await server.close();record.finished_utc=new Date().toISOString();fs.writeFileSync(path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/published/clients/workmate-retry-control.json'),JSON.stringify(record,null,2));console.log(JSON.stringify(record));process.exitCode=record.exit_code;}})();
