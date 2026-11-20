import { test, expect, type Page } from '@playwright/test'

const row = (count: number) => ({ tenant_id: 1, display_name: '术语库', description: '', category: '',
  responsible_team: '', contact: '', stages: ['insight'], featured: false, knowledge_base_count: 1,
  file_count: 2, document_entry_count: count, uploaded_file_count: 2, asset_count_scope: 'space_total',
  access_state: 'accessible', current_role: 'owner', can_request_access: false,
  access_request_pending: false, membership_suspended: false, interaction_action: 'none' })

async function setup(page: Page) {
  const state = { count: 3, calls: 0, fail: false, hidden: false }
  await page.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort())
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/portal/spaces') {
      state.calls++
      if (state.fail) return route.fulfill({ status: 503, json: { error: 'synthetic failure' } })
      const value: any = row(state.count)
      if (state.hidden) { delete value.document_entry_count; delete value.uploaded_file_count; value.asset_count_scope = 'hidden' }
      return route.fulfill({ json: { data: [value] } })
    }
    if (path === '/api/v1/portal/stages') return route.fulfill({ json: { data: [{ key: 'insight', name: '洞察', display_order: 10 }] } })
    return route.fulfill({ json: { success: true, data: [] } })
  })
  await page.goto('/tests/portal-assets/index.html', { waitUntil: 'domcontentloaded' })
  await expect.poll(() => state.calls).toBeGreaterThan(0)
  await page.waitForFunction(() => (window as any).portalHarness?.portal.spaces.length === 1)
  return state
}

test('real Portal component refreshes after SPA return and browser focus for external writes', async ({ page }) => {
  const state = await setup(page)
  await expect(page.locator('.space-summary small').first()).toHaveText('1 个知识库 · 3 篇文档')
  const initial = state.calls
  await page.evaluate(() => (window as any).portalHarness.router.push('/knowledge'))
  state.count = 4 // represents an external MCP/Feishu write, no browser mutation event
  await page.evaluate(() => (window as any).portalHarness.router.push('/portal'))
  await expect.poll(() => state.calls).toBeGreaterThan(initial)
  await expect(page.locator('.space-summary small').first()).toHaveText('1 个知识库 · 4 篇文档')
  state.count = 5
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(page.locator('.space-summary small').first()).toHaveText('1 个知识库 · 5 篇文档')
})

test('upload, manual create, delete and knowledge-base invalidations refresh the active Portal', async ({ page }) => {
  const state = await setup(page)
  for (const count of [4, 5, 4, 0]) {
    state.count = count
    await page.evaluate(() => (window as any).portalHarness.invalidatePortalAssets())
    await expect(page.locator('.space-summary small').first()).toHaveText(`1 个知识库 · ${count} 篇文档`)
  }
})

test('logout and user/tenant changes clear data immediately, including late responses', async ({ page }) => {
  await setup(page)
  let release!: () => void
  const held = new Promise<void>(resolve => { release = resolve })
  let requested = false
  await page.route('**/api/v1/portal/spaces*', async route => {
    requested = true
    await held
    await route.fulfill({ json: { data: [row(77)] } })
  })
  await page.evaluate(() => { void (window as any).portalHarness.portal.loadOverviewSpaces() })
  await expect.poll(() => requested).toBe(true)
  await page.evaluate(() => (window as any).portalHarness.auth.logout())
  release()
  await page.waitForTimeout(100)
  expect(await page.evaluate(() => (window as any).portalHarness.portal.spaces.length)).toBe(0)
  expect(await page.evaluate(() => (window as any).portalHarness.portal.overviewSpaces.length)).toBe(0)
  await page.evaluate(() => {
    const h = (window as any).portalHarness
    h.portal.spaces = [ { tenant_id: 99 } ]; h.auth.setUser({ id: 'other', username: 'other' })
  })
  expect(await page.evaluate(() => (window as any).portalHarness.portal.spaces.length)).toBe(0)
})

test('visibility activation refreshes external changes and hidden-tab polling stays bounded', async ({ page }) => {
  const state = await setup(page)
  await page.clock.install()
  await page.evaluate(() => Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }))
  const before = state.calls
  await page.clock.fastForward(60_000)
  expect(state.calls).toBe(before)
  state.count = 9
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(page.locator('.space-summary small').first()).toHaveText('1 个知识库 · 9 篇文档')
})

test('failed initialization is retryable and hidden totals are never replaced by legacy file_count', async ({ page }) => {
  const state = await setup(page)
  state.fail = true
  await page.evaluate(() => (window as any).portalHarness.portal.initialize())
  expect(await page.evaluate(() => (window as any).portalHarness.portal.initialized)).toBe(false)
  state.fail = false; state.hidden = true
  await page.evaluate(() => (window as any).portalHarness.portal.initialize())
  await expect(page.locator('.space-summary small').first()).toHaveText('1 个知识库 · — 篇文档')
  expect(await page.evaluate(() => (window as any).portalHarness.portal.initialized)).toBe(true)
})

test('late overview requests cannot overwrite a newer overview or filtered list', async ({ page }) => {
  await setup(page)
  const held: Array<(value: any) => void> = []
  await page.route('**/api/v1/portal/spaces*', async route => {
    const count = await new Promise<number>(resolve => held.push(resolve))
    await route.fulfill({ json: { data: [row(count)] } })
  })
  await page.evaluate(() => { const p = (window as any).portalHarness.portal; void p.loadOverviewSpaces(); void p.loadOverviewSpaces() })
  await expect.poll(() => held.length).toBe(2)
  held[1](7)
  await expect.poll(() => page.evaluate(() => (window as any).portalHarness.portal.overviewSpaces[0]?.document_entry_count)).toBe(7)
  held[0](1)
  await page.waitForTimeout(100)
  expect(await page.evaluate(() => (window as any).portalHarness.portal.overviewSpaces[0]?.document_entry_count)).toBe(7)
  await page.evaluate(() => {
    const p = (window as any).portalHarness.portal
    p.search = 'old-filter'; void p.loadSpaces()
    p.search = ''; void p.loadOverviewSpaces()
  })
  await expect.poll(() => held.length).toBe(4)
  held[3](11)
  await expect.poll(() => page.evaluate(() => (window as any).portalHarness.portal.spaces[0]?.document_entry_count)).toBe(11)
  held[2](2)
  await page.waitForTimeout(100)
  expect(await page.evaluate(() => (window as any).portalHarness.portal.spaces[0]?.document_entry_count)).toBe(11)
})

test('folder zero, subtree, filtered and whole-KB scopes have explicit labels', async ({ page }) => {
  await setup(page)
  await page.evaluate(() => (window as any).portalHarness.router.push('/knowledge'))
  await expect(page.locator('#app')).toContainText('当前文件夹 0 篇 · 全库 339 篇')
  await page.evaluate(() => { (window as any).portalHarness.scope.value = { count: 12, wholeCount: 339, filtered: false } })
  await expect(page.locator('#app')).toContainText('当前文件夹 12 篇 · 全库 339 篇')
  await page.evaluate(() => { (window as any).portalHarness.scope.value = { count: 2, wholeCount: 339, filtered: true } })
  await expect(page.locator('#app')).toContainText('筛选结果 2 篇 · 全库 339 篇')
})
