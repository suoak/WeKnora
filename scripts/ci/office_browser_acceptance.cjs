// Actual deployed SPA through a pinned SSH tunnel, never a component harness.
const {chromium}=require('../../frontend/node_modules/playwright');
const fs=require('fs'),path=require('path'),assert=require('node:assert/strict');
const privateDir=path.resolve(__dirname,'../../docs/qa/office-test-deployment/private');
const out=path.join(privateDir,'browser');fs.mkdirSync(out,{recursive:true});
const session=JSON.parse(fs.readFileSync(path.join(privateDir,'browser-session.json')));
const candidate=session.source_sha;
assert.match(candidate,/^(ca1c058c0887b9b2c00899bb6c7da6201761415a|668d067ec75a2a351ace4f7f0797be77af2b24ae)$/);
(async()=>{
 const browser=await chromium.launch({headless:true});
 const page=await browser.newPage({viewport:{width:1440,height:1000},locale:'zh-CN'});
 await page.addInitScript(s=>{
  localStorage.setItem('weknora_token',s.token);localStorage.setItem('weknora_user',JSON.stringify(s.user));
  if(s.tenant)localStorage.setItem('weknora_tenant',JSON.stringify(s.tenant));
  localStorage.setItem('weknora_memberships',JSON.stringify(s.memberships));localStorage.setItem('locale','zh-CN');
 },session);
 let requests=[];
 page.on('request',r=>{if(r.method()==='POST'&& /\/knowledge\/file|\/attachments/.test(r.url()))requests.push({method:r.method(),path:new URL(r.url()).pathname});});
 const results=[];
 const record=(name,result,extra={})=>{const row={case:name,result,source_sha:candidate,executed_utc:new Date().toISOString(),...extra};results.push(row);console.log(JSON.stringify(row));};
 try {
  await page.goto('http://127.0.0.1:49681/platform/knowledge-bases/'+session.kb_id,{waitUntil:'domcontentloaded',timeout:60000});
  await page.waitForTimeout(4000);
  const skipGuide=page.getByText('跳过引导',{exact:true});
  if(await skipGuide.count())await skipGuide.click();
  await page.screenshot({path:path.join(out,'initial.png'),fullPage:true});
  fs.writeFileSync(path.join(out,'initial-text.txt'),await page.locator('body').innerText());
  const input=page.locator('.kb-upload-source-dropdown input[type=file]:not([webkitdirectory])').first();
  await input.waitFor({state:'attached',timeout:15000});
  const fixtures='E:/code/WeKnora-office-drm/internal/officeguard/testdata';
  for(const [name,file,notice] of [
   ['browser_password_no_request',path.join(fixtures,'vendor/example_password.docx'),'打开密码'],
   ['browser_drm_no_request',path.join(fixtures,'drm.xlsx'),'DRM'],
   ['browser_real_user_drm_no_request',path.join(privateDir,'samples/user-drm.xlsx'),'DRM']]){
   requests=[];await input.setInputFiles(file);await page.waitForTimeout(1800);
   const text=await page.locator('body').innerText();
   const pass=requests.length===0&&text.includes(notice);
   record(name,pass?'PASS':'FAIL',{upload_requests:requests,notice_observed:text.includes(notice)});
   await page.screenshot({path:path.join(out,name+'.png'),fullPage:true});
  }
  requests=[];
  await input.setInputFiles([path.join(privateDir,'samples/browser-normal.docx'),path.join(fixtures,'vendor/example_password.xlsx'),path.join(privateDir,'samples/browser-protected.xlsx'),path.join(fixtures,'drm.xlsx')]);
  await page.waitForTimeout(1800);
  fs.writeFileSync(path.join(out,'batch-dialog-text.txt'),await page.locator('body').innerText());
  await page.screenshot({path:path.join(out,'batch-selected.png'),fullPage:true});
  const confirm=page.locator('.upload-confirm-modal:visible .modal-footer button').last();
  if(await confirm.count()){await confirm.click();await page.waitForTimeout(8000);}
  record('browser_mixed_batch',requests.length===2?'PASS':'FAIL',{upload_requests:requests});
  await page.screenshot({path:path.join(out,'batch-after.png'),fullPage:true});
 }catch(e){record('browser_harness','FAIL',{error:String(e),url:page.url()});}
 finally{fs.writeFileSync(path.join(out,'results.json'),JSON.stringify(results,null,2));await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
