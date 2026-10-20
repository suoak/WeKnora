// No SID, auth URL, credentials, cookies, query-bearing callback or HAR.
// Fixed-IP proxy makes it impossible for this check to connect to production.
import {createRequire} from 'node:module';
import http from 'node:http';
import fs from 'node:fs/promises';
import crypto from 'node:crypto';
const require=createRequire('E:/code/WeKnora/frontend/package.json');
const {chromium}=require('playwright');
const host='knowhub.ruijie.com.cn', ip='172.30.232.42';
const peers=[],rows=[],checks={};let browser,context;
const allowed=p=>p==='/'||p==='/login'||p==='/api/v1/auth/oidc/callback'||p==='/api/v1/auth/config'||p==='/api/v1/auth/oidc/config'||p.startsWith('/assets/')||/\.(?:js|css|svg|png|ico|webmanifest|woff2?)$/.test(p);
const server=http.createServer((req,res)=>{
 const u=new URL(req.url,'http://'+host);
 if(req.method!=='GET'||u.search||req.headers.authorization||req.headers.cookie||!allowed(u.pathname)) {res.writeHead(403);res.end();return;}
 const upstream=http.request({host:ip,port:80,path:u.pathname,method:'GET',headers:{Host:host,Accept:req.headers.accept||'*/*'}},r=>{
  const parts=[];r.on('data',b=>parts.push(b));r.on('end',()=>{
   const raw=Buffer.concat(parts);
   rows.push({path:u.pathname,status:r.statusCode,upstream_ip:r.socket?.remoteAddress||ip,upstream_port:80,bytes:raw.length});
   if(u.pathname==='/api/v1/auth/oidc/config'){
    try {const b=JSON.parse(raw);checks.config={status:r.statusCode,encoding:b.sourceid_basic_encoding,callback:b.redirect_uri};}catch{checks.config={parse:false};}
   }
   if(u.pathname==='/api/v1/auth/oidc/callback')checks.callback={status:r.statusCode,invalid_state:(r.headers.location||'').includes('oidc_error=invalid_state')};
   const h={...r.headers};delete h['set-cookie'];delete h['content-length'];res.writeHead(r.statusCode,h);res.end(raw);
  });
 });upstream.on('error',()=>{res.writeHead(502);res.end();});upstream.end();
});
let result;
try{
 await new Promise((resolve,reject)=>server.once('error',reject).listen(0,'127.0.0.1',resolve));
 const port=server.address().port;
 browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true,args:[`--host-resolver-rules=MAP ${host}:80 127.0.0.1:${port}, EXCLUDE localhost`,'--no-proxy-server','--disable-logging','--log-level=3']});
 context=await browser.newContext({acceptDownloads:false,serviceWorkers:'block'});
 await context.route('**/*',route=>{
  const u=new URL(route.request().url());
  return u.hostname===host&&u.protocol==='http:'&&!u.search&&allowed(u.pathname)?route.continue():route.abort();
 });
 const page=await context.newPage();const cdp=await context.newCDPSession(page);await cdp.send('Network.enable');
 cdp.on('Network.responseReceived',e=>{const u=new URL(e.response.url);if(u.hostname===host)peers.push({path:u.pathname,ip:e.response.remoteIPAddress,port:e.response.remotePort,status:e.response.status});});
 const login=await page.goto('http://'+host+'/login',{waitUntil:'networkidle',timeout:25000});
 checks.page={status:login.status(),oa_button_visible:await page.getByRole('button',{name:/OA/}).count()>0};
 for(const p of ['/api/v1/auth/config','/api/v1/auth/oidc/config'])await page.evaluate(async path=>{await fetch(path,{credentials:'omit'});},p);
 await page.goto('http://'+host+'/api/v1/auth/oidc/callback',{waitUntil:'domcontentloaded',timeout:20000});
 const scripts=rows.filter(x=>x.path.startsWith('/assets/')&&x.path.endsWith('.js')&&x.status===200);
 if(login.status()!==200||scripts.length===0||checks.config?.encoding!=='oauth_form'||checks.callback?.status!==302||!checks.callback.invalid_state||!peers.every(x=>x.ip==='127.0.0.1'&&x.port===port))throw new Error('route_assertion_failed');
 result={result:'PASS',utc:new Date().toISOString(),checks,static_js_success_count:scripts.length,actual_browser_peers:peers,fixed_upstream:ip+':80',upstream_request_summary:rows,system_proxy_bypassed:true,global_hosts_changed:false,sid_requests:0,authorization_started:false,existing_sso_used:false,credentials_or_cookies_read:false,callback_code_submitted:false,production_destination_possible_in_proxy:false,profile_scope:'fresh disposable routing check; not future real-login profile'};
}catch(e){result={result:'FAIL',failure_class:e.constructor.name,authorization_started:false};process.exitCode=1;}
finally{
 if(context)await context.close();if(browser)await browser.close();await new Promise(resolve=>server.close(resolve));
 result.temporary_proxy_closed=true;result.temporary_browser_closed=true;
 await fs.writeFile('docs/integration/sourceid/evidence/prompt-login-browser-route-preflight-20261016.json',JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result));
}
