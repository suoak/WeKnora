const fs = require('node:fs');const path = require('node:path');
const root=path.resolve(__dirname,'../../../..');const {_electron}=require(path.join(root,'frontend/node_modules/playwright'));
const out=path.join(root,'docs/integration/knowhub-mcp-portal/evidence/batch7/clients');fs.mkdirSync(out,{recursive:true});
const runtime=JSON.parse(fs.readFileSync('E:/AIData/Codex/knowhub-b7-clients/connection-runtime.json','utf8'));
const env={};for(const name of ['PATH','SystemRoot','WINDIR','COMSPEC','TEMP','TMP','APPDATA','LOCALAPPDATA','USERPROFILE','HOME','ProgramFiles','ProgramFiles(x86)','NUMBER_OF_PROCESSORS','PROCESSOR_ARCHITECTURE'])if(process.env[name])env[name]=process.env[name];
Object.assign(env,{CSBU_WORKMATE_E2E_TEST:'1',CSBU_WORKMATE_E2E_USER_DATA_DIR:'E:/AIData/Codex/knowhub-b7-clients/workmate-isolated-profile',CSBU_WORKMATE_DISABLE_AUTO_UPDATE:'1'});
const record={client:'CSBU WorkMate',expected_version:'2.4.0',candidate:runtime.candidate,isolated_profile:true,actual_tools_call:false,started_utc:new Date().toISOString()};let app;
(async()=>{try{
 app=await _electron.launch({executablePath:'E:/Program Files/csbuwork/CSBU WorkMate/CSBU WorkMate.exe',env,timeout:120000});
 const page=await app.firstWindow({timeout:120000});await page.waitForLoadState('domcontentloaded');
 record.app=await app.evaluate(({app,BrowserWindow})=>{for(const w of BrowserWindow.getAllWindows())w.hide();return {version:app.getVersion(),userData:app.getPath('userData')}});
 await page.waitForTimeout(3000);
 record.page=await page.evaluate(()=>({url:location.href,backendPort:window.__backendPort,backendStartupFailed:window.__backendStartupFailed,bridgeKeys:Object.keys(window.electronAPI||{}),body:document.body.innerText.slice(0,1000)}));
 const result=await page.evaluate(async connection=>{
   const base='http://127.0.0.1:'+window.__backendPort;
   const response=await fetch(base+'/api/mcp/test-connection',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:'knowhub-b7-synthetic',transport:{type:'http',url:connection.url,headers:{Authorization:'Bearer '+connection.token}}})});
   return {status:response.status,body:await response.text()};
 },runtime);
 record.connection=result;
 await page.screenshot({path:path.join(out,'workmate-isolated.png')});
 record.exit_code=result.status===200?0:1;
}catch(error){record.exit_code=1;record.error=String(error).replaceAll(runtime.token,'[REDACTED]');}
finally{if(app)await app.close().catch(()=>{});record.finished_utc=new Date().toISOString();fs.writeFileSync(path.join(out,'workmate-result.json'),JSON.stringify(record,null,2).replaceAll(runtime.token,'[REDACTED]'));console.log(JSON.stringify({client:record.client,exit_code:record.exit_code,status:record.connection?.status,version:record.app?.version}));}})();
