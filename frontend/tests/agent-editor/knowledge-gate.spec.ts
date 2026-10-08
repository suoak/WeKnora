import { test,expect } from '@playwright/test'
import { fixtureApi,testAgent } from './fixtures'
const kb={id:'local-kb',tenant_id:1,creator_id:testAgent.creator_id,name:'RC24 isolated KB',type:'document',embedding_model_id:'local-embedding',summary_model_id:'local-chat-model',chunking_config:{},indexing_strategy:{},capabilities:{vector:true,keyword:true,wiki:false}}
for(const scenario of ['normal','empty','failure','readonly'] as const) test(`knowledge documents/filter/state: ${scenario}`,async({page},info)=>{
  const api=await fixtureApi(page)
  await page.route('**/api/v1/knowledge-bases/local-kb**',async route=>{
    const path=new URL(route.request().url()).pathname
    if(path.endsWith('/knowledge/folders'))return route.fulfill({json:{success:true,data:{folders:[],root_count:scenario==='normal'?1:0}}})
    if(path.endsWith('/tags'))return route.fulfill({json:{success:true,data:[],total:0}})
    if(path.endsWith('/knowledge')) {
      if(scenario==='failure')return route.fulfill({status:500,json:{success:false,message:'Isolated knowledge list failure'}})
      const data=scenario==='empty'?[]:[{id:'local-doc',knowledge_base_id:kb.id,title:'RC24 document.txt',file_name:'RC24 document.txt',file_type:'txt',type:'file',parse_status:'completed',summary_status:'completed',description:'Isolated document',created_at:'2026-10-01T00:00:00Z',updated_at:'2026-10-01T00:00:00Z',tags:[]}]
      return route.fulfill({json:{success:true,data,total:data.length}})
    }
    if(path===`/api/v1/knowledge-bases/${kb.id}`)return route.fulfill({json:{success:true,data:{...kb,...(scenario==='readonly'?{creator_id:'another-user',my_permission:'viewer'}:{})}}})
    return route.fulfill({json:{success:true,data:[]}})
  })
  try{
    await page.goto('/platform/knowledge-bases/local-kb')
    await expect(page.locator('.doc-filter-toggle')).toBeVisible()
    await expect(page.locator('.doc-folder-path__count')).toHaveText(`共 ${scenario==='normal'||scenario==='readonly'?1:0} 个文档`)
    if(scenario==='empty'||scenario==='failure'){
      const guide=page.locator('.guide[role="dialog"]')
      await expect(guide).toBeVisible()
      await guide.locator('.guide__close').click()
      await expect(guide).toBeHidden()
    }
    await page.locator('.doc-filter-toggle').click()
    await expect(page.locator('#document-filters')).toBeVisible()
    await expect(page.locator('#document-filters strong')).toHaveText('筛选')
    await expect(page.locator('#document-filters').getByRole('button',{name:'清除筛选',exact:true})).toBeVisible()
    for(const key of ['knowledgeBase.documentCount','knowledgeBase.filters','knowledgeBase.clearFilters'])await expect(page.locator('body')).not.toContainText(key)
    if(scenario==='normal'||scenario==='readonly')await expect(page.locator('body')).toContainText('RC24 document')
    if(scenario==='readonly')await expect(page.locator('.doc-filter-actions')).toHaveCount(0)
    if(scenario==='failure')expect(api.responses.some(r=>r.status===500)).toBe(true)
    expect(api.pageErrors).toEqual([])
    expect(api.consoleErrors.filter(e=>/TypeError|Unhandled/.test(e))).toEqual([])
    expect(api.mutations).toHaveLength(0)
  }finally{await info.attach('knowledge-evidence',{body:JSON.stringify({responses:api.responses,console:api.consoleErrors,pageErrors:api.pageErrors}),contentType:'application/json'})}
})
test('viewer edits another creator Agent as read-only without update requests',async({page})=>{
  const api=await fixtureApi(page);api.seedAgent({...testAgent,creator_id:'another-user'})
  await page.addInitScript(()=>localStorage.setItem('weknora_memberships',JSON.stringify([{tenant_id:1,role:'viewer'}])))
  await page.route('**/api/v1/auth/me',route=>route.fulfill({json:{success:true,data:{user:{id:testAgent.creator_id,username:'Local viewer',tenant_id:'1'},tenant:{id:'1',name:'Local'},memberships:[{tenant_id:1,role:'viewer'}]}}}))
  await page.goto('/platform/agents?edit=rc24-isolated-agent')
  const shell=page.locator('.settings-modal-shell')
  await expect(shell).toBeVisible()
  await expect(shell.locator('[data-guide="agent-create-submit"]')).toHaveCount(0)
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toBeVisible()
  expect(api.mutations).toHaveLength(0)
})
