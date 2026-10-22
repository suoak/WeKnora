// Actual built SPA/navigation -> independent deployed APP. No fixture Vue entry.
const fs=require('node:fs'),path=require('node:path');
const root=path.resolve(__dirname,'../../../..');
const {chromium}=require('E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021/frontend/node_modules/playwright');
const current=JSON.parse(fs.readFileSync(path.join(root,'docs/integration/knowhub-mcp-portal/evidence/formal-shared-upgrade/current.json')));
const dir=path.join('E:/AIData/Codex/knowhub-formal-private',current.candidate_sha256.slice(0,16));
const runtime=JSON.parse(fs.readFileSync(path.join(dir,'client.json')));
const baseOut=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/formal-shared-upgrade/browser-'+current.candidate_sha256.slice(0,16));fs.mkdirSync(baseOut,{recursive:true});
const out=path.join(baseOut,'attempt-'+new Date().toISOString().replace(/[:.]/g,'-'));fs.mkdirSync(out);
const result={candidate_sha256:current.candidate_sha256,synthetic_account:true,sid_login:'NOT_REPEATED_PRODUCTION_CALLBACK',actions:[],started_utc:new Date().toISOString()};
const keyName='V1 UI personal '+Date.now();
let browser,page,key;
(async()=>{try{
 browser=await chromium.launch({headless:true,args:['--unsafely-treat-insecure-origin-as-secure='+runtime.url]});const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
 await context.addInitScript(login=>{localStorage.setItem('weknora_token',login.token);localStorage.setItem('weknora_user',JSON.stringify(login.user));localStorage.setItem('weknora_tenant',JSON.stringify(login.active_tenant));localStorage.setItem('weknora_memberships',JSON.stringify(login.memberships));localStorage.setItem('locale','zh-CN')},runtime.login);
 page=await context.newPage();page.setDefaultTimeout(30000);result.http=[];result.page_errors=[];result.console_errors=[];
 page.on('console',msg=>{if(msg.type()==='error'){let text=msg.text();for(const secret of [runtime.login.token,runtime.token,runtime.old_token,runtime.owner_jwt,runtime.admin_jwt])if(secret)text=text.split(secret).join('[REDACTED]');result.console_errors.push(text);}});
 page.on('response',r=>{if(r.url().includes('/api/v1/mcp-api-keys'))result.http.push({path:new URL(r.url()).pathname,method:r.request().method(),status:r.status()});});
 page.on('pageerror',error=>result.page_errors.push(error.message));
 await page.goto(runtime.url+'/platform/mcp-access');
 await page.getByRole('heading',{name:'MCP 接入',exact:true}).waitFor();
 // Dismiss the existing first-login tour through its actual UI control.
 try{await page.locator('.guide__close').waitFor({timeout:5000});await page.locator('.guide__close').click();}catch(error){if(await page.locator('.guide__close').isVisible())throw error;}
 if(!page.url().endsWith('/platform/mcp-access'))throw Error('personal entry moved');
 // Fresh synthetic employee has no historical credentials; empty state is expected.
 await page.getByRole('button',{name:'创建个人 Key',exact:true}).first().waitFor();await page.screenshot({path:path.join(out,'desktop-before-create.png'),fullPage:true});
 await page.getByRole('button',{name:'创建个人 Key',exact:true}).first().click();
 await page.getByPlaceholder('例如：我的 Codex').fill(keyName);
 const dialog=page.locator('.t-dialog:visible').filter({hasText:'创建个人 Key'});
 if(await dialog.getByText('选择空间',{exact:true}).count())throw Error('scope wizard exposed in personal creation');
 await page.screenshot({path:path.join(out,'create-dialog-no-secret.png'),fullPage:true});
 const responsePromise=page.waitForResponse(r=>r.url().endsWith('/api/v1/mcp-api-keys')&&r.request().method()==='POST').catch(()=>null);
 await dialog.getByRole('button',{name:'创建个人 Key',exact:true}).click();await page.waitForTimeout(400);result.messages_after_click=await page.locator('.t-message').allTextContents();
 const response=await responsePromise;if(!response)throw Error('create did not issue expected POST');const data=await response.json();
 if(response.status()!==201||!data.data?.token||data.data.expires_at)throw Error('personal key creation/default expiry failed');
 key=data.data;if(key.personal_scope_mode!=='dynamic_all')throw Error('new personal key did not opt into dynamic scope');runtime.token=key.token;runtime.key_id=key.id;fs.writeFileSync(path.join(dir,'client.json'),JSON.stringify(runtime));
 if(JSON.stringify([...key.capabilities].sort())!==JSON.stringify(['enterprise_retrieve','retrieve']))throw Error('wrong read-only capabilities');
 await page.locator('pre').waitFor();const toml=await page.locator('pre').innerText();
 if(!toml.includes('bearer_token_env_var = "KNOWHUB_MCP_KEY"')||toml.includes(key.token)||!toml.includes('/api/v1/mcp/personal'))throw Error('unsafe Codex template');
 await page.getByRole('button',{name:'复制全部配置',exact:true}).click();
 // The approved shared entry is HTTP. Exercise the real legacy copy fallback
 // by pasting from Chromium's clipboard; do not replace clipboard APIs.
 await page.evaluate(()=>{const e=document.createElement('textarea');e.id='acceptance-paste';document.body.appendChild(e);e.focus();});
 await page.locator('#acceptance-paste').press('Control+V');
 const copied=await page.locator('#acceptance-paste').inputValue();
 await page.locator('#acceptance-paste').evaluate(e=>e.remove());
 if(copied.replace(/\r\n/g,'\n')!==toml.replace(/\r\n/g,'\n'))throw Error('copy mismatch');
 result.clipboard_comparison='actual HTTP legacy copy and paste; Windows CRLF normalized to LF';fs.writeFileSync(path.join(dir,'copied-codex.toml'),copied);
 await page.getByText('Generic MCP JSON',{exact:true}).last().click();const generic=JSON.parse(await page.locator('pre').innerText());
 if(generic.mcpServers.knowhub.url!==runtime.url+'/api/v1/mcp/personal'||generic.mcpServers.knowhub.headers.Authorization!=='Bearer '+key.token)throw Error('wrong Generic config');
 await page.getByRole('button',{name:'测试连接',exact:true}).click();await page.getByRole('status').filter({hasText:'8 个只读工具发现通过'}).waitFor();
 await page.getByRole('button',{name:'关闭',exact:true}).last().click();
 await page.screenshot({path:path.join(out,'desktop-personal-and-history.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(out,'narrow-personal-and-history.png'),fullPage:true});
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2))throw Error('narrow layout overflow');
 await page.setViewportSize({width:1440,height:1000});
 const card=page.locator('.personal-card').filter({hasText:keyName});await card.getByRole('button',{name:'连接配置',exact:true}).click();
 await page.waitForFunction(expected=>document.querySelector('.token-row code')?.textContent===expected,key.token);
 if(await page.locator('.token-row code').innerText()!==key.token)throw Error('owner could not reopen encrypted Secret');
 await page.getByRole('button',{name:'关闭',exact:true}).last().click();
 await card.getByRole('button',{name:'管理',exact:true}).click();await page.getByRole('button',{name:'重新生成 Secret',exact:true}).last().click();
 const rotating=page.waitForResponse(r=>r.url().endsWith('/'+key.id+'/rotate')&&r.request().method()==='POST');await page.getByRole('button',{name:'确认重新生成',exact:true}).click();
 const rotated=await(await rotating).json();if(!rotated.data?.token||rotated.data.expires_at)throw Error('rotate failed');runtime.old_token=runtime.token;runtime.token=rotated.data.token;fs.writeFileSync(path.join(dir,'client.json'),JSON.stringify(runtime));
 await page.getByRole('button',{name:'关闭',exact:true}).last().click();
 result.actions.push({name:'real SPA original route, create, copy TOML/JSON, owner reopens encrypted Secret, rotate, desktop/narrow',status:'PASS',scope_count:key.tenant_scopes.length});result.exit_code=0;
 }catch(error){result.exit_code=1;result.error=String(error);if(page&&!key){result.visible_messages=await page.locator('.t-message').allTextContents();result.create_input=await page.getByPlaceholder('例如：我的 Codex').inputValue().catch(()=>null);await page.screenshot({path:path.join(out,'failed-before-secret.png'),fullPage:true});}}finally{
 if(browser)await browser.close();result.finished_utc=new Date().toISOString();
 fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));process.exitCode=result.exit_code;
 }})();
