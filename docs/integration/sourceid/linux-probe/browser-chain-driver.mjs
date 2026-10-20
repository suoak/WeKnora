import {createRequire} from 'node:module';
const require=createRequire(new URL('../../../../frontend/package.json', import.meta.url));const {chromium}=require('playwright');
const callback='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback';
let browser,context;const evidence=[];
try {
 browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true,args:['--host-resolver-rules=MAP knowhub.ruijie.com.cn 127.0.0.1, EXCLUDE localhost','--no-proxy-server','--disable-logging','--log-level=3'],chromiumSandbox:true});
 context=await browser.newContext({acceptDownloads:false});const page=await context.newPage();const cdp=await context.newCDPSession(page);await cdp.send('Network.enable');const peers=[];
 cdp.on('Network.responseReceived',e=>{const u=new URL(e.response.url);if(u.hostname==='knowhub.ruijie.com.cn'&&u.pathname!=='/synthetic-form')peers.push({scheme:u.protocol,ip:e.response.remoteIPAddress,port:e.response.remotePort,status:e.response.status});});
 try{const sid=await page.goto('https://sid.ruijie.com.cn/',{waitUntil:'domcontentloaded',timeout:25000});evidence.push({sid_public_page_status:sid.status(),sid_hsts_present:!!sid.headers()['strict-transport-security'],sid_hsts_include_subdomains:/includesubdomains/i.test(sid.headers()['strict-transport-security']||''),credentials_submitted:false});}catch{evidence.push({sid_public_page_read:'FAILED',credentials_submitted:false});}
 await page.route('https://example.com/**',route=>route.fulfill({status:200,contentType:'text/html',body:'Synthetic authorization placeholder; no provider request.'}));
 const started=page.waitForResponse(r=>r.url()==='http://knowhub.ruijie.com.cn/probe/start');
 await page.goto('http://knowhub.ruijie.com.cn/probe/start',{waitUntil:'domcontentloaded',timeout:15000});
 const start=await started;const startQuery=new URL(start.headers().location).searchParams;
 evidence.push({synthetic_start_status:start.status(),state_present:startQuery.has('state'),nonce_present:startQuery.has('nonce'),pkce_S256:startQuery.get('code_challenge_method')==='S256',synthetic_authorization_target_intercepted_locally:true,real_sid_authorize_tested:false});
 const url=callback+'?code=synthetic-browser-private-code&state=synthetic-browser-wrong-state&client_secret=synthetic-browser-private-secret&token=synthetic-browser-private-token&personal=synthetic-browser-private-personal';
 await page.setExtraHTTPHeaders({'Authorization':'Bearer synthetic-browser-private-token','Cookie':'test=synthetic-browser-private-cookie'});
 const reply=await page.goto(url,{waitUntil:'domcontentloaded',timeout:20000});const body=await reply.json();
 evidence.push({callback_base_exact:page.url().split('?')[0]===callback,callback_status:reply.status(),state_validation:body.validation?.state===false,completed:body.validation?.completed===true,no_sensitive_value_in_response:!JSON.stringify(body).includes('synthetic-browser-'),response_keys:Object.keys(body),callback_handled_by_independent_probe:reply.status()===400&&body.validation?.state===false});
 // A browser-only Mock response tests HTTPS -> HTTP navigation. No unknown SID
 // path is requested and the production service response is not modified.
 await page.route('https://sid.ruijie.com.cn/',route=>route.fulfill({status:302,headers:{location:url,'cache-control':'no-store'},body:''}));
 const redirected=await page.goto('https://sid.ruijie.com.cn/',{waitUntil:'domcontentloaded',timeout:20000});
 evidence.push({mock_https_to_http_redirect_status:redirected.status(),mock_redirect_callback_base_exact:page.url().split('?')[0]===callback,real_sid_authorize_tested:false});
 await page.route('http://knowhub.ruijie.com.cn/synthetic-form',route=>route.fulfill({status:200,contentType:'text/html',body:'<form method="POST" action="/api/v1/auth/oidc/callback"><input name="client_secret" value="synthetic-browser-private-secret"><input name="code" value="synthetic-browser-private-code"><input name="token" value="synthetic-browser-private-token"><input name="personal" value="synthetic-browser-private-personal"><button>Submit synthetic data</button></form>'}));
 await page.goto('http://knowhub.ruijie.com.cn/synthetic-form',{waitUntil:'domcontentloaded'});
 const postResponse=page.waitForResponse(r=>r.url()===callback&&r.request().method()==='POST');await page.locator('button').click();const post=await postResponse;
 evidence.push({sensitive_synthetic_post_body_status:post.status(),post_body_response_has_no_sensitive_value:!JSON.stringify(await post.json()).includes('synthetic-browser-')});
 const missing=await page.goto('http://knowhub.ruijie.com.cn/probe/callback?code=synthetic-browser-private-code&state=synthetic-browser-wrong-state',{waitUntil:'domcontentloaded',timeout:10000});evidence.push({unconfigured_callback_status:missing.status()});
 evidence.push({browser_version:browser.version(),callback_response_peers:peers,production_callback_ip_seen:peers.some(x=>x.ip==='10.51.134.114'),all_callback_connections_loopback:peers.length>0&&peers.every(x=>x.ip==='127.0.0.1'&&x.port===80&&x.scheme==='http:'),cookies_har_passwords_exported:false});
 if(reply.status()!==400||body.validation?.state!==false||peers.some(x=>x.ip!=='127.0.0.1'))process.exitCode=1;
}catch(e){evidence.push({error_class:e.constructor.name});process.exitCode=1;}
finally{if(context)await context.close();if(browser)await browser.close();evidence.push({dedicated_browser_closed:true});console.log(JSON.stringify(evidence));}
