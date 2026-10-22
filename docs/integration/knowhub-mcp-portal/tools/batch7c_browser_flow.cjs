// Real Vue components -> real JWT middleware -> policy CAS/audit and Key APIs.
const fs=require('node:fs'),path=require('node:path'),{pathToFileURL}=require('node:url');
const {spawn}=require('node:child_process');
const root=path.resolve(__dirname,'../../../..'),front=path.join(root,'frontend');
const {chromium}=require(path.join(front,'node_modules/playwright'));
const runtimePath='E:/AIData/Codex/knowhub-b7c-clients/connection-runtime.json',runtime=JSON.parse(fs.readFileSync(runtimePath,'utf8'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/published/browser');fs.mkdirSync(out,{recursive:true});
const record={candidate:runtime.candidate,synthetic_users:true,real_jwt:true,sid_login_ceremony:'NOT_TESTED',actions:[],started_utc:new Date().toISOString()};let server,browser;
(async()=>{try{
 const vite=await import(pathToFileURL(path.join(front,'node_modules/vite/dist/node/index.js')).href);
 process.env.VITE_DEV_PROXY_TARGET=runtime.app_url;
 server=await vite.createServer({root:front,configFile:path.join(front,'vite.config.ts'),configLoader:'runner',server:{host:'127.0.0.1',port:14881,strictPort:true,open:false,proxy:{'/api/v1/mcp/personal':{target:new URL(runtime.url).origin,changeOrigin:true},'/api':{target:runtime.app_url,changeOrigin:true}}}});await server.listen();
 browser=await chromium.launch({headless:true});
 async function pageFor(mode,token){const context=await browser.newContext();await context.addInitScript(({token,mode})=>{localStorage.setItem('weknora_token',token);if(mode==='owner')localStorage.setItem('weknora_selected_tenant_id','99')},{token,mode});const page=await context.newPage();page.setDefaultTimeout(45000);await page.goto(`http://127.0.0.1:14881/tests/portal-review.html?mode=${mode}`);return page;}
 const owner=await pageFor('owner',runtime.owner_jwt);
 const row=owner.locator('[data-kb-id="public-kb"]');await row.waitFor();
 if(await row.getAttribute('data-blocked')!=='false')throw Error('Published KB unexpectedly requires approval');
 record.actions.push({action:'published_scope_without_approval',status:'PASS'});
 await row.getByRole('button').click();await owner.locator('[data-testid="exception-reason"] textarea').fill('SYNTHETIC_SENSITIVE_KB');
 await owner.locator('[data-testid="exception-save"]').click();await owner.locator('[data-kb-id="public-kb"][data-blocked="true"]').waitFor();
 record.actions.push({action:'owner_public_KB_exception_block',status:'PASS'});
 await row.getByRole('button').click();await owner.locator('[data-testid="exception-reason"] textarea').fill('SYNTHETIC_RESTORE_PORTAL_DEFAULT');
 await owner.locator('[data-testid="exception-save"]').click();await owner.locator('[data-kb-id="public-kb"][data-blocked="false"]').waitFor();
 record.actions.push({action:'owner_exception_CAS_restore_without_approval',status:'PASS'});await owner.screenshot({path:path.join(out,'owner-public-scope.png'),fullPage:true});
 const admin=await pageFor('admin',runtime.reviewer_jwt);await admin.locator('[data-kb-id="public-kb"]').waitFor();
 const user=await pageFor('keys',runtime.user_jwt);await user.getByRole('button',{name:'创建统一个人 MCP（只读）'}).click();
 await user.getByPlaceholder('例如：Jerry - WorkBuddy').fill('synthetic-browser-personal');
 for(let step=0;step<4;step++)await user.getByRole('button',{name:'下一步',exact:true}).click();
 const secretResponse=user.waitForResponse(r=>r.url().endsWith('/api/v1/mcp-api-keys')&&r.request().method()==='POST');
 await user.getByRole('button',{name:'创建接入',exact:true}).click();const response=await secretResponse;const created=await response.json();
 if(response.status()!==201&&response.status()!==200)throw Error('Key create failed '+response.status());
 if(!created.data?.token||!created.data.capabilities.includes('enterprise_retrieve'))throw Error('missing explicit personal enterprise Key');
 // Secret retained only in private runtime file outside repository/evidence.
 runtime.browser_created_key_id=created.data.id;runtime.original_fixture_token=runtime.token;runtime.token=created.data.token;runtime.tunnel_url=runtime.url;runtime.url='http://127.0.0.1:14881/api/v1/mcp/personal';fs.writeFileSync(runtimePath,JSON.stringify(runtime));
 record.actions.push({action:'personal_key_created_through_existing_UI',status:'PASS',key_id:created.data.id,capabilities:created.data.capabilities,scope_count:created.data.tenant_scopes.length});
 await user.getByRole('button',{name:'测试连接',exact:true}).click();await user.getByRole('status').filter({hasText:'5 个只读工具'}).waitFor();
 record.actions.push({action:'personal_config_and_real_initialize_tools_list',status:'PASS'});
 // Never capture a screenshot while the one-time Secret/config is visible.
 await user.getByRole('button',{name:'关闭',exact:true}).last().click();await user.screenshot({path:path.join(out,'user-key-list.png'),fullPage:true});
 async function client(name,args){const started=new Date().toISOString();return await new Promise(resolve=>{const p=spawn(name,args,{cwd:root,windowsHide:true,stdio:['ignore','pipe','pipe']});let log='';p.stdout.on('data',d=>{log+=d});p.stderr.on('data',d=>{log+=d});p.on('close',code=>{record.actions.push({action:'real_client',argv:[name,...args],exit_code:code,started_utc:started,finished_utc:new Date().toISOString(),output:log.slice(-2000)});resolve(code)});});}
 const tools=path.join(root,'docs/integration/knowhub-mcp-portal/tools');
 await client('python',[path.join(tools,'batch7c_network_check.py')]);
 await client('python',[path.join(tools,'batch7c_cli_clients.py'),'codex','positive']);
 const workmate=await client('node',[path.join(tools,'batch7c_workmate_chat.cjs'),'positive']);record.workmate_exit_code=workmate;
 await client('python',[path.join(tools,'batch7c_cli_clients.py'),'codex','other']);
 const withdrawal=await admin.evaluate(async()=>{const r=await fetch('/api/v1/system/admin/portal/spaces/99/unpublish',{method:'POST',headers:{Authorization:'Bearer '+localStorage.getItem('weknora_token')}});return r.status});
 record.actions.push({action:'existing_portal_unpublish_route',http_status:withdrawal});if(withdrawal!==200)throw Error('Portal withdrawal failed');
 await client('python',[path.join(tools,'batch7c_cli_clients.py'),'codex','withdrawn']);
 const revoked=await user.evaluate(async id=>{const r=await fetch('/api/v1/mcp-api-keys/'+id,{method:'DELETE',headers:{Authorization:'Bearer '+localStorage.getItem('weknora_token')}});return r.status},created.data.id);
 record.actions.push({action:'existing_personal_key_revoke_route',http_status:revoked});if(revoked!==200)throw Error('Key revoke failed');
 await client('python',[path.join(tools,'batch7c_cli_clients.py'),'codex','revoked']);
 record.exit_code=record.actions.some(a=>a.action==='real_client'&&a.exit_code!==0)?1:0;
}catch(e){record.exit_code=1;record.error=String(e)}finally{if(browser)await browser.close();if(server)await server.close();record.finished_utc=new Date().toISOString();let raw=JSON.stringify(record,null,2);const latest=JSON.parse(fs.readFileSync(runtimePath));for(const v of Object.values(latest))if(typeof v==='string'&&(v.startsWith('mcp_')||v.startsWith('eyJ')))raw=raw.replaceAll(v,'[REDACTED]');fs.writeFileSync(path.join(out,'result.json'),raw);console.log(JSON.stringify(record));process.exitCode=record.exit_code;}})();
