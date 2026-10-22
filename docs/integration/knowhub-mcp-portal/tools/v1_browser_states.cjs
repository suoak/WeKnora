// UI-only failure/empty states are explicitly simulated; auth tests are separate.
const fs=require('node:fs'),path=require('node:path'),crypto=require('node:crypto');
const root=path.resolve(__dirname,'../../../..'),{chromium}=require(path.join(root,'frontend/node_modules/playwright'));
const cfg=JSON.parse(fs.readFileSync(path.join(root,'docs/integration/knowhub-mcp-portal/evidence/v1/current.json')));
const runtime=JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-v1-private/'+cfg.candidate_sha256.slice(0,16)+'/client.json'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/v1/browser-layout-'+cfg.candidate_sha256.slice(0,16),Date.now().toString());fs.mkdirSync(out,{recursive:true});
const result={candidate_sha256:cfg.candidate_sha256,harness_sha256:crypto.createHash('sha256').update(fs.readFileSync(__filename)).digest('hex'),actions:[]};
(async()=>{let browser;try{
 browser=await chromium.launch({headless:true});const context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addInitScript(login=>{localStorage.setItem('weknora_token',login.token);localStorage.setItem('weknora_user',JSON.stringify(login.user));localStorage.setItem('weknora_tenant',JSON.stringify(login.active_tenant));localStorage.setItem('weknora_memberships',JSON.stringify(login.memberships));localStorage.setItem('locale','zh-CN')},runtime.login);
 const page=await context.newPage();page.setDefaultTimeout(15000);let state='failure';
 await page.route('**/api/v1/mcp-api-keys',async route=>{
  if(route.request().method()!=='GET')return route.continue();
  if(state==='failure')return route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'SYNTHETIC UI failure fixture'})});
  if(state==='empty')return route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({data:[],mcp_public_url:runtime.url})});
  return route.continue();
 });
 await page.goto(runtime.url+'/platform/mcp-access');await page.getByRole('heading',{name:'MCP 接入',exact:true}).waitFor();
 try{await page.locator('.guide__close').waitFor({timeout:4000});await page.locator('.guide__close').click();}catch(e){if(await page.locator('.guide__close').isVisible())throw e;}
 await page.getByText('加载失败',{exact:true}).waitFor();state='real';await page.getByRole('button',{name:'重试',exact:true}).click();await page.locator('.personal-card').first().waitFor();
 result.actions.push({name:'simulated GET503 -> actual retry and real credential list',status:'PASS'});
 await page.getByRole('heading',{name:'MCP 接入',exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:path.join(out,'desktop-real-page.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});await page.waitForTimeout(400);await page.getByRole('heading',{name:'MCP 接入',exact:true}).scrollIntoViewIfNeeded();
 if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2))throw Error('narrow overflow');
 await page.screenshot({path:path.join(out,'narrow-real-page.png'),fullPage:true});result.actions.push({name:'actual 1440px and 390px responsive layout after transition',status:'PASS'});
 state='empty';await page.getByRole('button',{name:'刷新',exact:true}).click();await page.getByRole('heading',{name:'连接你的企业知识',exact:true}).waitFor();
 await page.screenshot({path:path.join(out,'synthetic-empty-state.png'),fullPage:true});result.actions.push({name:'explicitly simulated empty credential-list UI only',status:'PASS'});result.exit_code=0;
 }catch(e){result.exit_code=1;result.error=String(e)}finally{if(browser)await browser.close();fs.writeFileSync(path.join(out,'result.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));process.exitCode=result.exit_code;}})();
