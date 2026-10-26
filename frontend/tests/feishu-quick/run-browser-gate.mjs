// Actual Vue dialog, mocked HTTP only. Never opens a real OAuth session.
import assert from 'node:assert/strict'
import { mkdirSync } from 'node:fs'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from '@playwright/test'
import net from 'node:net'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const output = process.env.FEISHU_UI_EVIDENCE_DIR
if (output) mkdirSync(output, { recursive: true })
const port = await new Promise((resolve, reject) => {
  const probe = net.createServer()
  probe.once('error', reject)
  probe.listen(0, '127.0.0.1', () => { const port = probe.address().port; probe.close(() => resolve(port)) })
})
const entry = `
import {createApp,ref,nextTick,h} from 'vue';
import TDesign from 'tdesign-vue-next';
import 'tdesign-vue-next/es/style/index.css';
import {createRouter,createMemoryHistory} from 'vue-router';
import Dialog from '/src/views/knowledge/components/FeishuImportDialog.vue';
import Management from '/src/views/knowledge/FeishuImportManagement.vue';
import i18n from '/src/i18n/index.ts';
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:{render:()=>null}}]});
const params=new URLSearchParams(location.search);
const app=createApp({setup(){const visible=ref(false);nextTick(()=>visible.value=true);return()=>params.has('history')?h(Management):h(Dialog,{visible:visible.value,'onUpdate:visible':value=>visible.value=value,kbId:params.has('choose')?'':'existing-team-kb'})}});
app.use(router);app.use(TDesign);app.use(i18n);app.mount('#app');`
const server = await createServer({
  root, configFile: false, plugins: [vue(), {
    name: 'feishu-quick-browser-harness',
    resolveId(id) { if (id === '/__feishu-gate-entry.js') return '\0feishu-gate' },
    load(id) { if (id === '\0feishu-gate') return entry },
    configureServer(server) {
      server.middlewares.use('/__feishu-gate', async (_req, response) => {
        response.setHeader('Content-Type', 'text/html; charset=utf-8')
        response.end(await server.transformIndexHtml('/__feishu-gate', '<html><body><div id="app"></div><script type="module" src="/__feishu-gate-entry.js"></script></body></html>'))
      })
    },
  }], resolve: { alias: { '@': resolve(root, 'src') } },
  define: { __FRONTEND_VERSION__: '"test"', __FRONTEND_COMMIT__: '"test"' },
  optimizeDeps: { entries:[], include:['vue','vue-router','tdesign-vue-next'] },
  server: { host: '127.0.0.1', port, strictPort:true },
})
let browser
try {
  await server.listen()
  const address = server.httpServer.address()
  const origin = `http://127.0.0.1:${address.port}`
  browser = await chromium.launch({ headless: true })
  const page = await browser.newPage()
  page.setDefaultTimeout(20000)
  const errors = [], submissions = [], requests = []
  let targets = [{id:'existing-team-kb',name:'Existing target',requires_sharing_approval:true}], progressReads=0, continued=false
  page.on('pageerror', error => { errors.push(error.message); console.error('Browser:',error.message) })
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    requests.push(path)
    let body = []
    if (path === '/api/v1/me/feishu') body = { v2:true,available:true,configured:true,connected:true,test_http_callback:true }
    if (path === '/api/v1/me/feishu/connections') body = [{kind:'user_oauth',connected:true,id:'synthetic-connection'}]
    if (path === '/api/v1/me/feishu/import-targets') body = targets
    if (path === '/api/v1/me/feishu/batches') body = [{id:'progress-batch',kb_id:'existing-team-kb',created_at:new Date().toISOString()}]
    if (path === '/api/v1/feishu/batches/progress-batch') {
      progressReads++
      const done=progressReads>1
      body={batch:{id:'progress-batch',kb_id:'existing-team-kb',auth_mode:'user_oauth'},status:done?'succeeded':'running',items:[{id:'item',ordinal:0,status:done?'succeeded':'dispatched',stage:done?'completed':'embedding',last_progress_at:new Date().toISOString(),retry_after:'2026-01-01T00:00:00Z',index_verified:done,knowledge_id:'verified-document',chunk_count:6,vector_count:done?6:0}],counts:{}}
      if(page.url().includes('failure') && !continued) body={batch:{id:'progress-batch',kb_id:'existing-team-kb',auth_mode:'user_oauth'},target_name:'Existing target',can_continue_publication:true,status:'partial_failed',items:[{id:'item',ordinal:0,status:'blocked',error_category:'sharing_approval_required'}],counts:{blocked:1}}
    }
    if (path === '/api/v1/feishu/batches/progress-batch/continue') {
      assert.equal(request.method(),'POST')
      assert.deepEqual(request.postDataJSON(),{publish_to_readers:true})
      continued=true; body={accepted:true}
    }
    if (path === '/api/v1/me/feishu/imports') {
      const payload = request.postDataJSON()
      submissions.push({ payload, key:request.headers()['idempotency-key'] })
      body = {batch:{id:'synthetic-batch',kb_id:'private-kb',auth_mode:'user_oauth'},items:payload.links.map((url,index)=>({id:String(index),url,status:'pending'})),counts:{pending:payload.links.length},status:'running'}
    }
    await route.fulfill({status:path === '/api/v1/me/feishu/imports'?202:200,contentType:'application/json',body:JSON.stringify(body)})
  })
  await page.goto(origin + '/__feishu-gate')
  console.log('Harness loaded')
  await page.getByText('已连接飞书账号', {exact:true}).waitFor()
  assert.equal(await page.getByText('权限预检', {exact:true}).count(), 0)
  assert.equal(await page.getByText('创建私人知识库', {exact:true}).count(), 0)
  assert.equal(await page.locator('.t-select').count(), 1)
  const links = Array.from({length:10}, (_,i)=>`https://team.feishu.cn/${['docx','sheets','base','wiki'][i%4]}/token${i}`)
  await page.locator('textarea').fill([...links, links[0]+'?from=share','invalid',''].join('\n'))
  await page.getByText('1 条链接格式有误，将跳过；其余链接仍可导入。').waitFor()
  await page.getByRole('button', {name:/一键导入/}).click()
  await page.getByText('已添加 10 篇文档到导入队列，系统将在后台自动处理。可以关闭页面。', {exact:true}).waitFor()
  assert.equal(submissions.length, 1)
  assert.equal(submissions[0].payload.links.length, 10)
  assert.equal(submissions[0].payload.kb_id, 'existing-team-kb')
  assert.equal(submissions[0].payload.publish_to_readers, true)
  await page.getByText(/点击“一键导入”，即同意/).waitFor()
  assert.match(submissions[0].key, /^[a-f0-9]{32}$/)
  assert.equal(requests.some(path => path.includes('preview')), false)
  for (const width of [1280,390]) {
    await page.setViewportSize({width,height:900})
    const bounds = await page.locator('.t-dialog').boundingBox()
    assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width + 1, `dialog fits ${width}px`)
    const overflowing = await page.locator('.quick-import').evaluate(el=>el.scrollWidth > el.clientWidth + 1)
    assert.equal(overflowing, false)
    if (output) await page.screenshot({path:resolve(output,`quick-import-${width}.png`),fullPage:true})
  }
  await page.goto(origin + '/__feishu-gate?choose')
  await page.locator('textarea').fill(links[0])
  await page.getByText('已连接飞书账号', {exact:true}).waitFor()
  assert.equal(await page.getByRole('button',{name:/一键导入/}).isDisabled(),true)
  await page.locator('.quick-import .t-select').click()
  await page.locator('.t-select-option').filter({hasText:'Existing target'}).click()
  await page.getByRole('button',{name:/一键导入/}).click()
  await page.getByText('已添加 1 篇文档到导入队列，系统将在后台自动处理。可以关闭页面。',{exact:true}).waitFor()
  assert.equal(submissions[1].payload.kb_id,'existing-team-kb')
  targets=[{id:'existing-team-kb',name:'Private target',requires_sharing_approval:false}]
  await page.goto(origin + '/__feishu-gate')
  await page.getByText('已连接飞书账号',{exact:true}).waitFor()
  await page.locator('textarea').fill(links[0])
  await page.getByRole('button',{name:/一键导入/}).click()
  await page.getByText(/已添加 1 篇文档到导入队列/).waitFor()
  assert.equal(submissions[2].payload.publish_to_readers,false)
  assert.equal(await page.getByText(/点击“一键导入”，即同意/).count(),0)
  targets=[]
  await page.goto(origin + '/__feishu-gate?choose')
  await page.getByText(/暂无可写知识库/).waitFor()
  assert.equal(await page.getByRole('button',{name:/一键导入/}).isDisabled(),true)
  assert.equal(requests.some(path=>path.includes('private-knowledge-base')),false)
  await page.goto(origin + '/__feishu-gate?history')
  await page.getByText('第 1 篇：正在建立索引',{exact:true}).waitFor()
  await page.getByText('第 1 篇：已验证成功入库',{exact:true}).waitFor()
  assert.equal(progressReads,2)
  await page.getByRole('link',{name:'在知识库中查看'}).waitFor()
  assert.equal(await page.getByText(/自动重试时间/).count(),0)
  await page.goto(origin + '/__feishu-gate?history&failure')
  await page.getByText('本批任务尚未发布，点击“一键继续导入”即可继续。',{exact:true}).waitFor()
  await page.getByRole('button',{name:'一键继续导入',exact:true}).click()
  await page.getByText('已恢复原导入任务，系统将在后台自动处理。可以关闭页面。',{exact:true}).waitFor()
  assert.equal(requests.filter(path=>path.endsWith('/continue')).length,1)
  assert.equal(requests.some(path=>path.includes('preview') || path.includes('sharing-approvals') || path.endsWith('/retry')),false)
  assert.deepEqual(errors, [])
  await page.close()
  console.log(JSON.stringify({status:'PASS',qualification:'mocked_http_actual_vue_dialog_and_history',links:10,submissions:submissions.length,explicit_target_required:true,empty_targets_blocked:true,automatic_progress_reads:progressReads,preview_calls:0,viewports:[1280,390],real_oauth:'NOT_EXECUTED'}))
} catch (error) {
  console.error(error)
  throw error
} finally {
  if (browser) await browser.close()
  await Promise.race([server.close(), new Promise(resolve=>setTimeout(resolve,3000))])
}
