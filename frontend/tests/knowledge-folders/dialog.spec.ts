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
    await expect(page.locator('.folder-create-path-icon path').first()).toBeVisible()
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
