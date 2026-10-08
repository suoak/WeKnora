import { test, expect } from '@playwright/test'
import { fixtureApi } from './fixtures'
import { saveAgent, preservedKeys } from './save-data'
for (const rewrite of [false,true]) test(`normal animation save/close/list reopen: ${rewrite ? 'six' : 'four'} prompts`, async ({page},info) => {
  const api = await fixtureApi(page)
  const original = {...saveAgent,config:{...saveAgent.config,multi_turn_enabled:rewrite,enable_rewrite:rewrite}}
  api.seedAgent(original)
  const lifecycle:any[]=[]
  page.on('framenavigated',frame=>{if(frame===page.mainFrame())lifecycle.push({kind:'navigation',time:Date.now(),url:frame.url()})})
  page.on('domcontentloaded',()=>lifecycle.push({kind:'DOMContentLoaded',time:Date.now()}))
  page.on('load',()=>lifecycle.push({kind:'load',time:Date.now()}))
  await page.addInitScript(()=>{
    const rows:any[]=[];(window as any).rc24Lifecycle=rows
    const record=(kind:string,detail:any={})=>rows.push({kind,time:performance.now(),url:location.href,...detail})
    let last=''
    new MutationObserver(()=>{
      const shell=document.querySelector('.settings-modal-shell') as any
      const button=document.querySelector('[data-guide="agent-create-submit"]')
      const state=JSON.stringify({classes:shell?.className||null,buttonConnected:!!button?.isConnected,visible:shell?.__vueParentComponent?.props?.visible})
      if(state!==last){record('DOM',JSON.parse(state));last=state}
    }).observe(document,{subtree:true,childList:true,attributes:true,attributeFilter:['class']})
    ;(window as any).__VUE_DEVTOOLS_GLOBAL_HOOK__={enabled:true,emit(event:string,...args:any[]){
      if(!['component:added','component:removed','component:updated'].includes(event))return
      const instance=args[3],name=instance?.type?.__name||instance?.type?.name
      if(['AgentEditorModal','SettingsModalShell'].includes(name))record(event,{name,visible:instance.props?.visible,isUnmounted:instance.isUnmounted})
    }}
    document.addEventListener('click',e=>{if((e.target as Element).closest('[data-guide="agent-create-submit"]'))record('save-click')},true)
    document.addEventListener('transitionend',e=>{if((e.target as Element).classList.contains('settings-modal-shell'))record('transitionend')},true)
  })
  try {
    await page.goto('/platform/agents?edit=rc24-isolated-agent')
    const shell=page.locator('.settings-modal-shell')
    await expect(shell.locator('.editor-initializing')).toBeHidden()
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(shell.locator('.prompts-outline__pill')).toHaveCount(rewrite?6:4)
    const input=shell.locator('.prompts-panel__pane:visible textarea').first()
    await input.fill('RC24 normal close cycle')
    expect(api.mutations).toHaveLength(0)
    await shell.getByRole('button',{name:'保存并关闭',exact:true}).click()
    await expect(shell).toBeHidden() // Standard assertion; no animation override/time extension.
    expect(api.mutations).toHaveLength(1)
    for(const key of preservedKeys) expect((api.saved().config as any)[key],key).toEqual((original.config as any)[key])
    // Reopen via actual list menu, without navigation, refresh or fresh instance.
    const card=page.locator('.agent-card:visible').filter({hasText:original.name}).first()
    await card.locator('.more-wrap').click()
    await page.locator('.popup-menu-item:visible').filter({hasText:'编辑'}).click()
    await expect(shell).toBeVisible()
    await expect(shell.locator('.editor-initializing')).toBeHidden()
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(input).toHaveValue('RC24 normal close cycle')
    expect(api.mutations).toHaveLength(1)
    expect(api.pageErrors).toEqual([])
  } finally {
    await info.attach('lifecycle',{body:JSON.stringify({navigation:lifecycle,dom:await page.evaluate(()=>(window as any).rc24Lifecycle)}),contentType:'application/json'})
    await info.attach('close-cycle-evidence',{body:JSON.stringify({mutations:api.mutations,persisted:api.saved(),responses:api.responses,console:api.consoleErrors,pageErrors:api.pageErrors}),contentType:'application/json'})
    await info.attach('DOM',{body:await page.content(),contentType:'text/html'})
  }
})
