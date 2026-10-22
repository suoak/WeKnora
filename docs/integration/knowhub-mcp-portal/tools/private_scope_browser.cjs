// Real deployed SPA + existing test APP. Only this run's synthetic keys are revoked.
const fs = require('node:fs'), path = require('node:path'), assert = require('node:assert/strict');
const { chromium } = require('E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021/frontend/node_modules/playwright');
const root = path.resolve(__dirname, '../../../..');
const out = path.join(root, 'docs/integration/knowhub-mcp-portal/evidence/private-scope/browser');
fs.mkdirSync(out, { recursive: true });
if (fs.readdirSync(out,{withFileTypes:true}).some(e=>e.isFile())) { const previous=path.join(out,'previous-'+Date.now());fs.mkdirSync(previous);for(const entry of fs.readdirSync(out,{withFileTypes:true}))if(entry.isFile())fs.renameSync(path.join(out,entry.name),path.join(previous,entry.name)); }
const login = JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-formal-private/private-scope-login.json'));
const result = { started_utc: new Date().toISOString(), url: 'http://172.30.232.42/platform/mcp-access', actions: [], synthetic_account: true, production_accessed: false };
const keys = []; let browser, page;
const digestKeys = rows => JSON.stringify(rows.filter(k => !keys.some(own => own.id === k.id)).map(k => ({ id:k.id, capabilities:k.capabilities, personal_scope_mode:k.personal_scope_mode, tenant_scopes:k.tenant_scopes, expires_at:k.expires_at, revoked_at:k.revoked_at })).sort((a,b)=>a.id-b.id));
(async () => { try {
  browser = await chromium.launch({ headless: true, args: ['--unsafely-treat-insecure-origin-as-secure=http://172.30.232.42'] });
  const context = await browser.newContext({ viewport: { width:1440, height:1000 } });
  await context.addInitScript(login => { localStorage.setItem('weknora_token',login.token);localStorage.setItem('weknora_user',JSON.stringify(login.user));localStorage.setItem('weknora_tenant',JSON.stringify(login.active_tenant));localStorage.setItem('weknora_memberships',JSON.stringify(login.memberships));localStorage.setItem('locale','zh-CN'); }, login);
  page = await context.newPage(); page.setDefaultTimeout(30000);
  const api = async (method, endpoint, body) => page.evaluate(async ({method,endpoint,body}) => { const r = await fetch(endpoint, { method, headers: { Authorization:'Bearer '+localStorage.getItem('weknora_token'), 'Content-Type':'application/json' }, body:body===undefined?undefined:JSON.stringify(body) }); return { status:r.status, body:await r.json() }; }, {method,endpoint,body});
  await page.goto(result.url); await page.getByRole('heading',{name:'MCP 接入',exact:true}).waitFor();
  try { await page.locator('.guide__close').waitFor({timeout:5000});await page.locator('.guide__close').click(); } catch(e) { if(await page.locator('.guide__close').isVisible())throw e; }
  const before = await api('GET','/api/v1/mcp-api-keys'); assert.equal(before.status,200);
  const original = digestKeys(before.body.data);
  const options = (await api('GET','/api/v1/mcp-api-keys/scope-options')).body.data;
  const home = options.flatMap(o => o.owned_knowledge_bases).find(k => k.id==='fm-private-home');
  const other = options.flatMap(o => o.owned_knowledge_bases).find(k => k.id==='fm-private-other');
  assert.ok(home && other, 'approved synthetic private fixtures missing');
  // User-entered historical Key names may contain "Portal". Check product
  // descriptions and range labels, without rewriting existing names.
  assert.doesNotMatch(await page.locator('.service-summary').innerText(), /Portal|公共知识|八个只读工具|动态全部个人授权空间/);
  for(const label of await page.locator('.personal-card .identity p').allTextContents())assert.doesNotMatch(label,/Portal|公共知识|动态全部个人授权空间/);
  await page.screenshot({path:path.join(out,'desktop.png'),fullPage:true});
  const open = async name => { await page.getByRole('button',{name:'创建个人 Key',exact:true}).first().click(); await page.getByPlaceholder('例如：我的 Codex').fill(name); return page.locator('.t-dialog:visible').filter({hasText:'填写名称、有效期和知识范围'}); };
  const create = async dialog => { const ready = page.waitForResponse(r => r.url().endsWith('/api/v1/mcp-api-keys') && r.request().method()==='POST').catch(()=>null); await dialog.getByRole('button',{name:'创建个人 Key',exact:true}).click(); const response=await ready; assert.ok(response,'create POST missing');assert.equal(response.status(),201); const key=(await response.json()).data; keys.push(key); assert.ok(!key.expires_at); await page.locator('pre').waitFor(); assert.ok((await page.locator('pre').innerText()).includes('bearer_token_env_var = "KNOWHUB_MCP_KEY"')); assert.ok(!(await page.locator('pre').innerText()).includes(key.token)); await page.getByRole('button',{name:'关闭',exact:true}).last().click(); return key; };
  const call = async (key,name,args={}) => page.evaluate(async ({token,name,args}) => { const r=await fetch('/api/v1/mcp/personal',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json',Accept:'application/json, text/event-stream'},body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/call',params:{name,arguments:args}})});let raw=await r.text();if(raw.startsWith('event:')||raw.startsWith('data:'))raw=raw.split('\n').find(s=>s.startsWith('data:')).slice(5);return {status:r.status,body:JSON.parse(raw)}; }, {token:key.token,name,args});
  const value = r => { assert.equal(r.status,200);assert.ok(!r.body.error&&!r.body.result?.isError);return JSON.parse(r.body.result.content[0].text); };
  const denied = r => r.status>=400 || !!r.body.error || !!r.body.result?.isError;
  let dialog = await open('Scope UI default '+Date.now());
  assert.equal(await dialog.getByRole('radio',{name:'默认范围',exact:true}).isChecked(),true);
  assert.doesNotMatch(await dialog.innerText(), /Portal|公共知识|动态全部个人授权空间/);
  await page.screenshot({path:path.join(out,'create-default.png'),fullPage:true});
  const normal=await create(dialog); assert.equal(normal.personal_scope_mode,'dynamic_all');assert.deepEqual([...normal.capabilities].sort(),['enterprise_retrieve','retrieve']);
  const defaults=value(await call(normal,'list_knowledge_bases')); const defaultIDs=JSON.stringify(defaults);
  assert.ok(defaultIDs.includes('fm-public-kb')&&defaultIDs.includes('fm-public-two')&&defaultIDs.includes('fm-private-home')&&defaultIDs.includes('fm-private-other'));
  result.actions.push({name:'default dynamic public + two private spaces; safe Codex template; never expires',status:'PASS'});
  dialog=await open('Scope UI selected '+Date.now()); await dialog.getByText('指定范围',{exact:true}).click();assert.ok(await dialog.getByRole('radio',{name:'指定范围',exact:true}).isChecked());
  await dialog.getByRole('button',{name:'创建个人 Key',exact:true}).click(); await page.getByText('请至少选择一个当前有权读取的知识库',{exact:true}).waitFor();
  assert.equal(await page.locator('.t-dialog:visible').filter({hasText:'填写名称、有效期和知识范围'}).count(),1);
  await dialog.getByText(home.name,{exact:true}).click();assert.ok(await dialog.getByRole('checkbox',{name:home.name,exact:true}).isChecked());
  await page.screenshot({path:path.join(out,'create-selected.png'),fullPage:true});
  const selected=await create(dialog); assert.equal(selected.personal_scope_mode,'fixed');assert.deepEqual(selected.capabilities,['retrieve']); assert.equal(selected.tenant_scopes.length,1);assert.equal(selected.tenant_scopes[0].kb_scope_mode,'selected');
  const selectedList=JSON.stringify(value(await call(selected,'list_knowledge_bases'))); assert.ok(selectedList.includes('fm-private-home')); assert.ok(!selectedList.includes('fm-public-')&&!selectedList.includes('fm-private-other'));
  assert.ok(value(await call(selected,'read_document',{knowledge_id:'fm-private-home-doc'})).chunks.length>0);
  assert.ok(denied(await call(selected,'read_document',{knowledge_id:'fm-public-two-doc'})));
  assert.ok(denied(await call(selected,'read_document',{knowledge_id:'fm-private-other-doc'})));
  result.actions.push({name:'selected exact private KB; empty selection rejected; public + unselected direct IDs denied',status:'PASS'});
  let card=page.locator('.personal-card').filter({hasText:selected.name}); await card.getByRole('button',{name:'管理',exact:true}).click();
  await page.getByRole('button',{name:'修改范围',exact:true}).click(); dialog=page.locator('.t-dialog:visible').filter({hasText:'填写名称、有效期和知识范围'});
  assert.equal(await dialog.getByRole('radio',{name:'默认范围',exact:true}).isDisabled(),true);
  await dialog.getByText(home.name,{exact:true}).click();await dialog.getByText(other.name,{exact:true}).click();assert.ok(!(await dialog.getByRole('checkbox',{name:home.name,exact:true}).isChecked()));assert.ok(await dialog.getByRole('checkbox',{name:other.name,exact:true}).isChecked());
  const updating=page.waitForResponse(r=>r.url().endsWith('/api/v1/mcp-api-keys/'+selected.id)&&r.request().method()==='PATCH');
  await dialog.getByRole('button',{name:'保存修改',exact:true}).click();assert.equal((await updating).status(),200);await page.getByText('指定范围已更新',{exact:true}).waitFor();
  const afterEdit=JSON.stringify(value(await call(selected,'list_knowledge_bases')));assert.ok(afterEdit.includes('fm-private-other')&&!afterEdit.includes('fm-private-home')&&!afterEdit.includes('fm-public-'));
  const after=await api('GET','/api/v1/mcp-api-keys');assert.equal(digestKeys(after.body.data),original);
  result.actions.push({name:'explicit edit changes only selected private scope; all historical Key scopes/expiry unchanged',status:'PASS'});
  await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(out,'narrow.png'),fullPage:true});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2));
  result.actions.push({name:'existing navigation and narrow layout',status:'PASS'});result.exit_code=0;
 }catch(e){result.exit_code=1;result.error=String(e);for(const key of keys)if(key.token)result.error=result.error.split(key.token).join('[REDACTED]');}
 finally {
  result.cleanup=[];
  if(page)for(const key of keys){const status=await page.evaluate(async ({id})=>(await fetch('/api/v1/mcp-api-keys/'+id,{method:'DELETE',headers:{Authorization:'Bearer '+localStorage.getItem('weknora_token')}})).status,{id:key.id}).catch(()=>0);result.cleanup.push({key_id:key.id,revoke_http:status});if(status!==200)result.exit_code=1;}
  if(browser)await browser.close();result.finished_utc=new Date().toISOString();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));process.exitCode=result.exit_code;
 }
})();
