// Actual callback/dialog Vue components. Provider HTTP is mocked; no real consent claimed.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from '@playwright/test'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { mkdirSync, writeFileSync } from 'node:fs'
import net from 'node:net'
const root=resolve(dirname(fileURLToPath(import.meta.url)),'../..')
const output=process.env.FEISHU_UI_EVIDENCE_DIR
const port=await new Promise((resolve,reject)=>{const probe=net.createServer();probe.once('error',reject);probe.listen(0,'127.0.0.1',()=>{const p=probe.address().port;probe.close(()=>resolve(p))})})
if(output)mkdirSync(output,{recursive:true})
const entry=`
import {createApp,h,onMounted,ref} from 'vue';import {createRouter,createWebHistory,RouterView} from 'vue-router';
import TDesign from 'tdesign-vue-next';import 'tdesign-vue-next/es/style/index.css';
import Callback from '/src/views/auth/FeishuCallback.vue';import Dialog from '/src/views/knowledge/components/FeishuImportDialog.vue';
import {takeFeishuDraft} from '/src/utils/feishuOAuthFlow';import i18n from '/src/i18n/index.ts';
const Import={setup(){const visible=ref(false),draft=ref('');onMounted(()=>{const p=takeFeishuDraft(location.pathname+location.search,'');if(p){draft.value=p.url;visible.value=true}});return()=>h(Dialog,{visible:visible.value,kbId:'',initialUrl:draft.value,'onUpdate:visible':v=>visible.value=v})}};
const router=createRouter({history:createWebHistory(),routes:[{path:'/feishu/callback',component:Callback},{path:'/platform/knowledge-bases',component:Import},{path:'/platform/feishu/imports',component:{render:()=>h('p','Import history')}}]});
const app=createApp({render:()=>h(RouterView)});app.use(router);app.use(TDesign);app.use(i18n);await router.isReady();app.mount('#app');`
const server=await createServer({root,configFile:false,plugins:[vue(),{name:'oauth-callback-browser',resolveId(id){if(id==='/__callback-entry.js')return '\0callback-entry'},load(id){if(id==='\0callback-entry')return entry},configureServer(s){s.middlewares.use(async(req,res,next)=>{if(!['/feishu/callback','/platform/knowledge-bases','/platform/feishu/imports'].includes(req.url.split('?')[0]))return next();res.setHeader('Content-Type','text/html; charset=utf-8');res.end(await s.transformIndexHtml(req.url,'<html><head><meta name="viewport" content="width=device-width, initial-scale=1"><style>html,body{margin:0}</style></head><body><div id="app"></div><script type="module" src="/__callback-entry.js"></script></body></html>'))})}}],resolve:{alias:{'@':resolve(root,'src')}},define:{__FRONTEND_VERSION__:'"test"',__FRONTEND_COMMIT__:'"test"'},server:{host:'127.0.0.1',port,strictPort:true},optimizeDeps:{entries:[],include:['vue','vue-router','tdesign-vue-next','tdesign-icons-vue-next','vue-i18n','axios']}})
let browser
const checks=[]
setTimeout(()=>{console.error('Callback browser deadline');process.exit(1)},180000).unref()
try{
 await server.listen();const origin=`http://127.0.0.1:${server.httpServer.address().port}`
 browser=await chromium.launch({headless:true,executablePath:process.env.FEISHU_UI_BROWSER_PATH})
 for(const mode of ['first','supplement','cancel','expired','session','network-committed','network-unconfirmed','repeated','timeout','narrow']){
  const context=await browser.newContext({viewport:mode==='narrow'?{width:390,height:844}:{width:1280,height:800}})
  const page=await context.newPage();page.setDefaultTimeout(18000);page.setDefaultNavigationTimeout(60000);const errors=[];let exchanges=0,connected=['supplement','network-unconfirmed'].includes(mode),revision=connected?'old':'',imports=0
  page.on('pageerror',e=>errors.push(e.message))
  await context.addInitScript(({mode})=>{localStorage.setItem('weknora_token','synthetic-session');localStorage.setItem('weknora_user',JSON.stringify({id:'owner'}));if(!sessionStorage.getItem('seeded')){sessionStorage.setItem('seeded','true');sessionStorage.setItem('knowhub_feishu_pending',JSON.stringify({kbId:'',url:'https://team.feishu.cn/docx/document1\nhttps://team.feishu.cn/wiki/document2',path:'/platform/knowledge-bases?ignored=1',createdAt:Date.now(),owner:'owner'}));sessionStorage.setItem('feishu-import-target-draft','existing-kb');sessionStorage.setItem('knowhub_feishu_callback',JSON.stringify({started:mode==='repeated',before:['supplement','network-unconfirmed'].includes(mode)?'old':'',createdAt:Date.now(),owner:'owner'}))}},{mode})
  await page.route('**/api/v1/**',async route=>{
   const path=new URL(route.request().url()).pathname
   let status=200,body={}
   if(path==='/api/v1/me/feishu/callback'){
    exchanges++
    if(mode==='expired'){status=400;body={error:'invalid_or_expired_oauth_state'}}
    else if(mode==='session'){status=401;body={error:'authentication_required'}}
    else if(mode==='timeout'){await new Promise(r=>setTimeout(r,24000));return route.abort().catch(()=>{})}
    else if(mode==='network-unconfirmed')return route.abort('failed')
    else {connected=true;revision='new';body={connected:true};if(mode==='network-committed')return route.abort('failed')}
   }else if(path==='/api/v1/me/feishu'){body={configured:true,available:true,v2:true,connected,authorization_revision:revision}}
   else if(path.endsWith('/connections'))body=[{kind:'user_oauth',connected,id:'connection'}]
   else if(path.endsWith('/import-targets'))body=[{id:'existing-kb',name:'Existing KB',requires_sharing_approval:false}]
   else if(path==='/api/v1/me/feishu/imports'){imports++;body={}}
   await route.fulfill({status,contentType:'application/json',body:JSON.stringify(body)}).catch(()=>{})
  })
  const query=mode==='cancel'?'?error=access_denied':'?code=synthetic-only&state=synthetic-only'
  await page.goto(origin+'/feishu/callback'+query,{waitUntil:'domcontentloaded'})
  if(['first','supplement','network-committed','narrow'].includes(mode)){
   await page.getByRole('heading',{name:'飞书账号连接成功',exact:true}).waitFor()
   assert.equal(new URL(page.url()).search,'')
   if(output)await page.screenshot({path:resolve(output,`callback-${mode}.png`)})
   await page.waitForURL(origin+'/platform/knowledge-bases')
   await page.getByRole('button',{name:/一键导入/}).waitFor()
   assert.match(await page.locator('textarea').inputValue(),/document1[\s\S]*document2/)
   assert.equal(await page.locator('.t-select input').inputValue(),'Existing KB')
   assert.equal(imports,0);assert.equal(exchanges,1)
   assert.equal(await page.evaluate(()=>sessionStorage.getItem('knowhub_feishu_pending')),null)
   assert.equal(await page.evaluate(()=>sessionStorage.getItem('knowhub_feishu_callback')),null)
  }else{
   await page.getByRole('heading',{name:'飞书账号未连接完成',exact:true}).waitFor({timeout:mode==='timeout'?50000:18000})
   if(mode==='cancel')await page.getByText(/已取消飞书授权/).waitFor()
   if(mode==='expired')await page.getByText(/授权已过期或已使用/).waitFor()
   if(mode==='session')await page.getByRole('button',{name:'登录 KnowHub',exact:true}).waitFor()
   if(['network-unconfirmed','repeated','timeout'].includes(mode))await page.getByText(/无法确认连接结果/).waitFor()
   const count=exchanges;await page.reload({waitUntil:'domcontentloaded'});await page.getByRole('heading',{name:'飞书账号未连接完成',exact:true}).waitFor();assert.equal(exchanges,count,'refresh must not replay code')
   assert.equal(imports,0)
  }
  const size=await page.evaluate(()=>({width:document.documentElement.scrollWidth,viewport:window.innerWidth}));assert.ok(size.width<=size.viewport,JSON.stringify({mode,...size}))
  assert.deepEqual(errors,[]);checks.push({mode,passed:true,exchanges,imports});await context.close()
 }
 const result={status:'PASS',qualification:'actual_vue_browser_mocked_provider_http',real_oauth:'NOT_EXECUTED',checks}
 if(output)writeFileSync(resolve(output,'callback-browser.json'),JSON.stringify(result,null,2))
 console.log(JSON.stringify(result))
}finally{if(browser)await browser.close();await Promise.race([server.close(),new Promise(r=>setTimeout(r,3000))])}
