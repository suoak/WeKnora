// Additional real-page compatibility and layout checks; synthetic instance only.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const root=path.resolve(__dirname,'../../../..'),{chromium}=require(path.join(root,'frontend/node_modules/playwright'));
const cfg=JSON.parse(fs.readFileSync(path.join(root,'docs/integration/knowhub-mcp-portal/evidence/v1/current.json')));
const runtime=JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-v1-private/'+cfg.candidate_sha256.slice(0,16)+'/client.json'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/v1/browser-management-'+cfg.candidate_sha256.slice(0,16),Date.now().toString());fs.mkdirSync(out,{recursive:true});
const result={candidate_sha256:cfg.candidate_sha256,harness_sha256:crypto.createHash('sha256').update(fs.readFileSync(__filename)).digest('hex'),synthetic_only:true,actions:[]};
(async()=>{let browser;try{
 browser=await chromium.launch({headless:true});const context=await browser.newContext({viewport:{width:1366,height:900}});
 await context.addInitScript(login=>{localStorage.setItem('weknora_token',login.token);localStorage.setItem('weknora_user',JSON.stringify(login.user));localStorage.setItem('weknora_tenant',JSON.stringify(login.active_tenant));localStorage.setItem('weknora_memberships',JSON.stringify(login.memberships));localStorage.setItem('locale','zh-CN')},runtime.login);
 const page=await context.newPage();page.setDefaultTimeout(15000);result.http=[];page.on('response',r=>{if(r.url().includes('/api/v1/mcp-api-keys'))result.http.push({path:new URL(r.url()).pathname,method:r.request().method(),status:r.status()});});await page.goto(runtime.url+'/platform/mcp-access');await page.getByRole('heading',{name:'MCP 接入',exact:true}).waitFor();
 try{await page.locator('.guide__close').waitFor({timeout:4000});await page.locator('.guide__close').click();}catch(e){if(await page.locator('.guide__close').isVisible())throw e;}
 const headers={Authorization:'Bearer '+runtime.login.token};
 const before=(await(await context.request.get(runtime.url+'/api/v1/mcp-api-keys',{headers})).json()).data;
 const legacyResponse=await context.request.post(runtime.url+'/api/v1/mcp-api-keys',{headers,data:{name:'V1 legacy compatibility '+Date.now(),client_type:'generic',capabilities:['retrieve'],tenant_scopes:[{tenant_id:runtime.login.user.tenant_id,kb_scope_mode:'all',knowledge_bases:[]}],expires_at_unix:Math.floor(Date.now()/1000)+86400}});
 const legacy=(await legacyResponse.json()).data;if(legacyResponse.status()!==201||!legacy||legacy.capabilities.includes('enterprise_retrieve'))throw Error('synthetic legacy fixture creation failed');
 await page.getByRole('button',{name:'刷新',exact:true}).click();await page.locator('.legacy-access summary').waitFor();
 await page.locator('.legacy-access summary').click();const card=page.locator('.legacy-access article').filter({hasText:legacy.name});await card.getByRole('button',{name:'管理',exact:true}).click();
 await page.getByRole('button',{name:'重新生成 Secret',exact:true}).last().click();
 result.step='legacy rotate';const [rotatedResponse]=await Promise.all([page.waitForResponse(r=>r.url().endsWith('/'+legacy.id+'/rotate')&&r.request().method()==='POST'),page.getByRole('button',{name:'确认重新生成',exact:true}).click()]);
 const rotated=(await rotatedResponse.json()).data;if(rotatedResponse.status()!==200||!rotated.token)throw Error('legacy rotate failed');
 for(const field of ['capabilities','tenant_scopes','expires_at'])if(JSON.stringify(rotated[field])!==JSON.stringify(legacy[field]))throw Error('legacy authority or expiry changed: '+field);
 if(await page.getByText('Codex CLI',{exact:true}).count())throw Error('legacy key disguised as unified personal configuration');
 await page.getByRole('button',{name:'关闭',exact:true}).last().click();await card.getByRole('button',{name:'管理',exact:true}).click();await page.getByRole('button',{name:'停用接入',exact:true}).click();
 result.step='legacy revoke';const [revokedResponse]=await Promise.all([page.waitForResponse(r=>r.url().endsWith('/'+legacy.id)&&r.request().method()==='DELETE'),page.locator('.t-popconfirm__buttons button').last().click()]);if(revokedResponse.status()!==200)throw Error('legacy revoke failed');
 await page.locator('.details').filter({hasText:legacy.name}).waitFor({state:'hidden'});result.actions.push({name:'legacy real UI view/rotate/revoke, authority and expiry preserved',status:'PASS',key_id:legacy.id});
 await page.screenshot({path:path.join(out,'desktop-history-management.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 await page.waitForTimeout(350);if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2))throw Error('narrow overflow');
 await page.screenshot({path:path.join(out,'narrow-sidebar-collapsed.png'),fullPage:true});result.actions.push({name:'390px with existing sidebar collapse control',status:'PASS'});result.exit_code=0;
 }catch(e){result.exit_code=1;result.error=String(e)}finally{if(browser)await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));process.exitCode=result.exit_code;}})();
