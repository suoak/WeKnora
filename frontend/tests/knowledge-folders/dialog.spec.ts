import { expect, test } from '@playwright/test'
const endpoint = '**/api/v1/knowledge-bases/kb-1/knowledge/folders'
const emptyTree = { data: { root_document_count: 0, total_document_count: 0, folders: [] } }
const kbName = 'Production folder retrieval baseline'
const parent = '第一层/第二层/第三层/Picker-persisted-1795712543459'
const url = '/tests/knowledge-folders/index.html?'+new URLSearchParams({kbName,parent})

test('long creation locations remain readable on desktop and mobile', async ({ page }) => {
  await page.route(endpoint, route => route.fulfill({json:emptyTree}))
  for (const viewport of [{width:1280,height:800},{width:375,height:700}]) {
    await page.setViewportSize(viewport)
    await page.goto(url)
    await page.getByRole('button',{name:'新建文件夹',exact:true}).last().click()
    const input = page.getByLabel('文件夹名称',{exact:true})
    await expect(input).toBeFocused()
    await expect(input).toHaveAttribute('placeholder','请输入文件夹名称')
    await expect(page.locator('.folder-create-path')).toHaveAttribute('title',kbName+' / '+parent.split('/').join(' / '))
    await expect(page.locator('.folder-create-path-segment')).toHaveText([kbName,'…','第三层','Picker-persisted-1795712543459'])
    await expect(page.locator('.folder-create-heading-icon path').first()).toBeVisible()
    await expect(page.getByRole('button',{name:'创建',exact:true})).toBeDisabled()
    const box = await page.locator('.document-folder-create-dialog').boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(15)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width - 15)
    expect(await page.locator('.folder-create-path').evaluate(e=>e.scrollWidth<=e.clientWidth)).toBeTruthy()
    const border = await input.evaluate(e=>{const s=getComputedStyle(e);return [s.borderTopWidth,s.borderRightWidth,s.borderBottomWidth,s.borderLeftWidth]})
    expect(border).toEqual(['1px','1px','1px','1px'])
    await expect(input).toBeInViewport()
    await page.locator('.document-folder-create-dialog').screenshot({path:`test-results/knowledge-folders/create-dialog-${viewport.width}.png`,animations:'disabled'})
    await page.keyboard.press('Escape')
    await expect(input).toBeHidden()
  }
})

test('keyboard creation preserves the full parent and locks the form while pending', async ({ page }) => {
  let release: (()=>void) | undefined
  const posts: any[] = []
  await page.route(endpoint, async route => {
    if(route.request().method()==='POST') {
      posts.push(route.request().postDataJSON())
      await new Promise<void>(resolve=>{release=resolve})
      await route.fulfill({status:201,json:{data:{path:parent+'/子目录'}}})
    } else await route.fulfill({json:emptyTree})
  })
  await page.goto(url)
  await page.getByRole('button',{name:'新建文件夹',exact:true}).last().click()
  const input=page.getByLabel('文件夹名称',{exact:true})
  await input.fill('子目录')
  await input.press('Enter')
  await expect(input).toBeDisabled()
  await expect(page.getByRole('button',{name:'取消',exact:true})).toBeDisabled()
  await page.keyboard.press('Enter')
  expect(posts).toEqual([{parent_path:parent,name:'子目录'}])
  release!()
  await expect(input).toBeHidden()
})

test('duplicate errors stay associated with the input and clear on editing', async ({ page }) => {
  await page.route(endpoint, route => route.fulfill(route.request().method()==='POST'
    ? {status:409,json:{message:'Folder exists'}} : {json:emptyTree}))
  await page.goto('/tests/knowledge-folders/index.html')
  await page.getByRole('button',{name:'新建文件夹',exact:true}).last().click()
  const input=page.getByLabel('文件夹名称',{exact:true})
  await input.fill('重复目录')
  await page.getByRole('button',{name:'创建',exact:true}).click()
  await expect(input).toHaveAttribute('aria-invalid','true')
  await expect(input).toHaveAttribute('aria-describedby','document-folder-name-error')
  await expect(page.locator('#document-folder-name-error')).toHaveText('此位置已存在同名文件夹。')
  await input.fill('不同目录')
  await expect(input).toHaveAttribute('aria-invalid','false')
  await expect(page.locator('#document-folder-name-error')).toHaveCount(0)
})

test('folder dialog follows the existing knowledge-base dialog frame in light and dark themes', async ({page}) => {
  await page.route(endpoint, route=>route.fulfill({json:emptyTree}))
  await page.route('**/api/v1/knowledge-bases/kb-1/tags**', route=>route.fulfill({json:{data:[],total:0}}))
  const frame=async(selector:string,title:string)=>{
    const root=page.locator(selector)
    await expect(root).toBeVisible()
    await root.screenshot({animations:'disabled'})
    return root.evaluate((e,titleSelector)=>{
      const style=(selector:string)=>getComputedStyle(e.querySelector(selector)!)
      const box=getComputedStyle(e), header=style('.t-dialog__header'), body=style('.t-dialog__body'), close=style('.t-dialog__close'), heading=style(titleSelector), button=style('button.t-button--theme-primary')
      const icon=style(titleSelector==='.batch-tag-title'?'.batch-tag-heading-icon':'.folder-create-heading-icon')
      return {radius:box.borderRadius,font:box.fontFamily,width:box.width,headerLeft:header.paddingLeft,headerTop:header.paddingTop,bodyLeft:body.paddingLeft,bodyRight:body.paddingRight,closeWidth:close.width,closeHeight:close.height,titleSize:heading.fontSize,titleWeight:heading.fontWeight,iconSize:icon.fontSize,buttonHeight:button.height,buttonSize:button.fontSize,background:box.backgroundColor}
    },title)
  }
  for(const theme of ['light','dark']) {
    await page.goto('/tests/knowledge-folders/index.html?reference=tags')
    await page.evaluate(t=>document.documentElement.setAttribute('theme-mode',t),theme)
    await page.getByRole('button',{name:'新建文件夹',exact:true}).last().click()
    const reference=await frame('.batch-tag-dialog','.batch-tag-title')
    await page.locator('.batch-tag-dialog').screenshot({path:`test-results/knowledge-folders/reference-tag-dialog-${theme}.png`,animations:'disabled'})
    await page.goto(url)
    await page.evaluate(t=>document.documentElement.setAttribute('theme-mode',t),theme)
    await page.getByRole('button',{name:'新建文件夹',exact:true}).last().click()
    expect(await frame('.document-folder-create-dialog','.folder-create-title')).toEqual(reference)
    await expect(page.locator('.folder-create-location')).toHaveCSS('background-color','rgba(0, 0, 0, 0)')
    await page.locator('.document-folder-create-dialog').screenshot({path:`test-results/knowledge-folders/create-dialog-${theme}.png`,animations:'disabled'})
  }
})

test('access request cards show usernames and safe fallback while approvals still use request IDs', async ({page}) => {
  let approved=false
  const mutations:any[]=[]
  const rows=[
    {id:'request-alice',applicant_user_id:'9f1173ca-b862-4cc1-9329-26c307a3bb26',applicant_name:'陈晓明'},
    {id:'request-deleted',applicant_user_id:'deleted-user',applicant_name:''},
    {id:'request-old-api',applicant_user_id:'legacy-user'},
    {id:'request-escaped',applicant_user_id:'escaped-user',applicant_name:'<img src=x onerror=alert(1)>'},
  ]
  await page.route('**/api/v1/tenants/7/access-requests**',async route=>{
    if(route.request().method()==='POST') {
      mutations.push({path:new URL(route.request().url()).pathname,body:route.request().postDataJSON()})
      approved=true
      return route.fulfill({json:{success:true}})
    }
    return route.fulfill({json:{success:true,data:rows.map(r=>({...r,tenant_id:7,status:approved&&r.id==='request-alice'?'approved':'pending',reason:'项目资料查阅',created_at:'2026-11-26T07:50:00Z'}))}})
  })
  await page.route('**/api/v1/me/notifications/unread-count',route=>route.fulfill({json:{success:true,data:{unread_count:0}}}))
  await page.goto('/tests/knowledge-folders/index.html?requests')
  const alice=page.locator('.request-list article').filter({hasText:'陈晓明'})
  await expect(alice.locator('strong')).toHaveText('申请人 陈晓明')
  await expect(alice.locator('strong')).toHaveAttribute('title',rows[0].applicant_user_id)
  await expect(page.locator('.request-list')).not.toContainText(rows[0].applicant_user_id)
  await expect(page.locator('.request-list strong').filter({hasText:'姓名暂不可用'})).toHaveCount(2)
  await expect(page.locator('.request-list')).toContainText(rows[3].applicant_name!)
  await expect(page.locator('.request-list img')).toHaveCount(0)
  await alice.screenshot({path:'test-results/knowledge-folders/access-request-username.png',animations:'disabled'})
  await page.screenshot({path:'test-results/knowledge-folders/access-request-names.png',animations:'disabled'})
  await alice.getByRole('button',{name:'批准',exact:true}).click()
  await expect(alice).toHaveCount(0)
  expect(mutations).toEqual([{path:'/api/v1/tenants/7/access-requests/request-alice/approve',body:{review_note:''}}])
})
