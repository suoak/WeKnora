// Real Vue components -> real JWT middleware -> policy CAS/audit and Key APIs.
const fs=require('node:fs'),path=require('node:path'),{pathToFileURL}=require('node:url');
const {spawn}=require('node:child_process');
const root=path.resolve(__dirname,'../../../..'),front=path.join(root,'frontend');
const {chromium}=require(path.join(front,'node_modules/playwright'));
const runtimePath='E:/AIData/Codex/knowhub-b7d-clients/connection-runtime.json',runtime=JSON.parse(fs.readFileSync(runtimePath,'utf8'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/business/browser');fs.mkdirSync(out,{recursive:true});
const record={candidate:runtime.candidate,synthetic_users:true,real_jwt:true,sid_login_ceremony:'NOT_TESTED',actions:[],started_utc:new Date().toISOString()};let server,browser;
(async()=>{try{
 const vite=await import(pathToFileURL(path.join(front,'node_modules/vite/dist/node/index.js')).href);
 process.env.VITE_DEV_PROXY_TARGET=runtime.app_url;
 server=await vite.createServer({root:front,configFile:path.join(front,'vite.config.ts'),configLoader:'runner',server:{host:'127.0.0.1',port:14881,strictPort:true,open:false,proxy:{'/api/v1/mcp/personal':{target:new URL(runtime.url).origin,changeOrigin:true},'/api':{target:runtime.app_url,changeOrigin:true}}}});await server.listen();
 browser=await chromium.launch({headless:true});
 async function pageFor(mode,token){const context=await browser.newContext();await context.addInitScript(({token,mode})=>{localStorage.setItem('weknora_token',token);if(mode==='owner')localStorage.setItem('weknora_selected_tenant_id','99')},{token,mode});const page=await context.newPage();page.setDefaultTimeout(45000);await page.goto(`http://127.0.0.1:14881/tests/portal-review.html?mode=${mode}`);return page;}
 const user=await pageFor('keys',runtime.user_jwt);await user.getByRole('button',{name:'创建统一个人 MCP（只读）'}).click();
 await user.getByPlaceholder('例如：我的 Codex').fill('synthetic-business-personal');
 const secretResponse=user.waitForResponse(r=>r.url().endsWith('/api/v1/mcp-api-keys')&&r.request().method()==='POST');
 await user.getByRole('button',{name:'创建个人 Key',exact:true}).click();const response=await secretResponse;const created=await response.json();
 if(response.status()!==201&&response.status()!==200)throw Error('Key create failed '+response.status());
 if(created.data.expires_at)throw Error('personal Key default unexpectedly expires');
 if(!created.data?.token||!created.data.capabilities.includes('enterprise_retrieve'))throw Error('missing explicit personal enterprise Key');
 // Secret retained only in private runtime file outside repository/evidence.
 runtime.browser_created_key_id=created.data.id;runtime.original_fixture_token=runtime.token;runtime.token=created.data.token;runtime.tunnel_url=runtime.tunnel_url||runtime.url;runtime.url='http://127.0.0.1:14881/api/v1/mcp/personal';fs.writeFileSync(runtimePath,JSON.stringify(runtime));
 record.actions.push({action:'personal_key_created_through_existing_UI',status:'PASS',key_id:created.data.id,capabilities:created.data.capabilities,scope_count:created.data.tenant_scopes.length});
 await user.getByRole('button',{name:'测试连接',exact:true}).click();await user.getByRole('status').filter({hasText:'5 个只读工具'}).waitFor();
 record.actions.push({action:'personal_config_and_real_initialize_tools_list',status:'PASS'});
 await user.getByText('Codex CLI',{exact:true}).click();const codex=await user.locator('pre').innerText();if(!codex.includes('[mcp_servers.knowhub]')||!codex.includes('bearer_token_env_var = "KNOWHUB_MCP_KEY"')||codex.includes(runtime.token))throw Error('invalid Codex template');record.actions.push({action:'Codex_connection_template',status:'PASS'});
 // Never capture a screenshot while the one-time Secret/config is visible.
 await user.getByRole('button',{name:'关闭',exact:true}).last().click();await user.screenshot({path:path.join(out,'user-key-list.png'),fullPage:true});

 let rpcId=10;
 async function call(name,args={}){const response=await fetch(runtime.url,{method:'POST',headers:{Authorization:'Bearer '+runtime.token,'Content-Type':'application/json',Accept:'application/json, text/event-stream'},body:JSON.stringify({jsonrpc:'2.0',id:rpcId++,method:'tools/call',params:{name,arguments:args}}),signal:AbortSignal.timeout(30000)});let text=await response.text();if(text.startsWith('event:')||text.startsWith('data:'))text=text.split('\n').find(l=>l.startsWith('data:')).slice(5).trim();const body=text?JSON.parse(text):null;record.actions.push({action:'real_MCP_tool',tool:name,http_status:response.status,is_error:!!(body?.error||body?.result?.isError)});return {status:response.status,body};}
 function parsed(result){if(result.status!==200||result.body?.error||result.body?.result?.isError)throw Error('MCP tool rejected');return JSON.parse(result.body.result.content[0].text);}
 const scopes=created.data.tenant_scopes.map(s=>s.tenant_id).sort((a,b)=>a-b);if(JSON.stringify(scopes)!=='[1,7]')throw Error('personal creation lost an existing private scope');
 const bases=parsed(await call('list_knowledge_bases'));const ids=bases.map(k=>k.id).sort();if(JSON.stringify(ids)!==JSON.stringify(['private-home','private-other','public-kb','public-two']))throw Error('unexpected dynamic public/private union');
 const hits=parsed(await call('search_knowledge',{query:'PUBLIC',mode:'hybrid',limit:30}));if(new Set(hits.results.map(h=>h.knowledge_base_id)).size!==4)throw Error('hybrid did not return all four authorized KBs');
 parsed(await call('grep_chunks',{query:'PUBLIC',limit:30}));parsed(await call('list_documents',{knowledge_base_id:'public-kb'}));parsed(await call('read_document',{knowledge_id:'private-other-doc'}));
 const denied=await call('read_document',{knowledge_id:'sensitive'});if(!denied.body?.result?.isError&&!denied.body?.error)throw Error('sensitive public exception leaked');
 const write=await call('create_knowledge');if(!write.body?.result?.isError&&!write.body?.error)throw Error('write tool accepted');
 const withdrawal=await fetch(runtime.app_url+'/api/v1/system/admin/portal/spaces/99/unpublish',{method:'POST',headers:{Authorization:'Bearer '+runtime.reviewer_jwt}});if(withdrawal.status!==200)throw Error('unpublish failed');
 const after=parsed(await call('list_knowledge_bases'));if(after.some(k=>k.id==='public-kb')||!after.some(k=>k.id==='private-other'))throw Error('withdrawal union incorrect');
 const old=await call('read_document',{knowledge_id:'public-doc'});if(!old.body?.result?.isError&&!old.body?.error)throw Error('old document reference survived withdrawal');
 const revoke=await fetch(runtime.app_url+'/api/v1/mcp-api-keys/'+created.data.id,{method:'DELETE',headers:{Authorization:'Bearer '+runtime.user_jwt}});if(revoke.status!==200)throw Error('key revoke failed');
 const rejected=await call('list_knowledge_bases');if(rejected.status!==401)throw Error('revoked key accepted');
 record.actions.push({action:'one_dialog_personal_create_all_private_scopes_and_public_union',status:'PASS',private_tenants:scopes,initial_kbs:ids});record.exit_code=0;

}catch(e){record.exit_code=1;record.error=String(e)}finally{if(browser)await browser.close();if(server)await server.close();record.finished_utc=new Date().toISOString();let raw=JSON.stringify(record,null,2);const latest=JSON.parse(fs.readFileSync(runtimePath));for(const v of Object.values(latest))if(typeof v==='string'&&(v.startsWith('mcp_')||v.startsWith('eyJ')))raw=raw.replaceAll(v,'[REDACTED]');fs.writeFileSync(path.join(out,'result-attempt2.json'),raw);console.log(JSON.stringify(record));process.exitCode=record.exit_code;}})();
