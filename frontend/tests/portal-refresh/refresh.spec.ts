import { test, expect, type Page } from '@playwright/test'

async function setup(page: Page) {
  let reads = 0, bellReads = 0, mode = 'normal', count = 2
  let release: (() => void) | undefined
  const gate = () => new Promise<void>(resolve => { release = resolve })
  const spaces = () => Array.from({ length: 12 }, (_, index) => ({
    tenant_id: index + 1, display_name: `Space ${index + 1}`, description: 'Portal regression', category: 'ntos',
    responsible_team: 'Test', contact: 'Test', stages: index < 8 ? ['design'] : [], featured: false,
    knowledge_base_count: 1, file_count: count, document_entry_count: count, asset_count_scope: 'space_total',
    access_state: index === 1 ? 'discoverable' : 'accessible', current_role: index === 1 ? null : 'viewer',
    can_request_access: index === 1, access_request_pending: false, membership_suspended: false, interaction_action: 'enter',
  }))
  await page.addInitScript(() => {
    localStorage.setItem('weknora_token', 'synthetic-only')
    localStorage.setItem('weknora_user', JSON.stringify({ id: 'fixture', username: 'Fixture', tenant_id: '1' }))
    localStorage.setItem('weknora_tenant', JSON.stringify({ id: '1', name: 'Fixture' }))
    localStorage.setItem('locale', 'zh-CN')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
  })
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/portal/spaces') {
      reads++
      if (mode === 'slow') await gate()
      if (mode === 'offline') return route.abort('internetdisconnected')
      if (mode === 'denied') return route.fulfill({ status: 403, json: { success: false } })
      return route.fulfill({ json: { success: true, data: mode === 'empty' ? [] : spaces() } })
    }
    if (path === '/api/v1/portal/stages') return route.fulfill({ json: { success: true, data: [{ key: 'design', display_order: 1, name: 'Design' }] } })
    if (path.includes('unread-count')) { bellReads++; return route.fulfill({ json: { success: true, data: { unread_count: 0 } } }) }
    return route.fulfill({ json: { success: true, data: [], total: 0 } })
  })
  await page.goto('/tests/portal-refresh/index.html')
  await expect(page.locator('.space-summary').first()).toBeVisible()
  await expect(page.locator('.portal-page .t-skeleton')).toHaveCount(0)
  return { reads: () => reads, bellReads: () => bellReads, mode: (value: string) => { mode = value }, release: () => release?.(), count: (value: number) => { count = value } }
}
const store = (page: Page, code: string) => page.evaluate(code => {
  const portal = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('portal')
  return new Function('portal', code)(portal)
}, code)

test('slow, unchanged and failed refresh preserve card DOM, scroll, focus and open access dialog', async ({ page }) => {
  const api = await setup(page)
  const restricted = page.locator('.space-summary').filter({ hasText: 'Space 2' }).first()
  await restricted.click()
  const dialog = page.locator('.t-dialog:visible')
  await expect(dialog).toBeVisible()
  await dialog.locator('textarea').fill('Keep this draft')
  await page.evaluate(() => {
    const card = document.querySelector('.space-summary') as any; card.__original = true
    document.querySelector('.portal-page')!.scrollTop = 150
  })
  api.mode('slow')
  await store(page, 'void portal.initialize(true)')
  await expect.poll(() => api.reads()).toBe(2)
  await expect(page.locator('.portal-page .t-skeleton')).toHaveCount(0)
  await expect(dialog.locator('textarea')).toBeFocused()
  await expect(dialog.locator('textarea')).toHaveValue('Keep this draft')
  api.release()
  await expect.poll(() => store(page, 'return portal.refreshing')).toBe(false)
  expect(await page.evaluate(() => (document.querySelector('.space-summary') as any).__original)).toBe(true)
  expect(await page.locator('.portal-page').evaluate(el => el.scrollTop)).toBe(150)
  api.mode('offline'); await store(page, 'return portal.initialize(true)')
  await expect(dialog.locator('textarea')).toHaveValue('Keep this draft')
  await expect(page.locator('.portal-page .t-skeleton')).toHaveCount(0)
  expect(await store(page, 'return portal.overviewError')).toBe(false)
  api.mode('normal'); api.count(3); await store(page, 'return portal.initialize(true)')
  expect(await store(page, 'return portal.overviewSpaces[0].document_entry_count')).toBe(3)
  await page.screenshot({ path: 'test-results/portal-refresh/dialog-after-refresh.png' })
})

test('five minute freshness deduplicates focus/visibility bursts, pauses while hidden and cleans up on unmount', async ({ page }) => {
  await page.clock.install()
  const api = await setup(page)
  await page.evaluate(() => { for (let i = 0; i < 20; i++) { window.dispatchEvent(new Event('focus')); document.dispatchEvent(new Event('visibilitychange')) } })
  expect(api.reads()).toBe(1)
  await page.clock.runFor(299000); expect(api.reads()).toBe(1)
  await page.clock.runFor(1500); await expect.poll(() => api.reads()).toBe(2)
  await expect.poll(() => store(page, 'return portal.refreshing')).toBe(false)
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')) })
  await page.clock.runFor(600000); expect(api.reads()).toBe(2)
  await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); document.dispatchEvent(new Event('visibilitychange')); window.dispatchEvent(new Event('focus')) })
  await expect.poll(() => api.reads()).toBe(3)
  await expect.poll(() => store(page, 'return portal.refreshing')).toBe(false)
  await page.evaluate(() => (document.querySelector('#app') as any).__vue_app__.unmount())
  await page.clock.runFor(600000); expect(api.reads()).toBe(3)
})

test('empty success, denial and session change never restore old authorized data', async ({ page }) => {
  const api = await setup(page)
  api.mode('empty'); await store(page, 'return portal.initialize(true)')
  expect(await store(page, 'return portal.initialLoading')).toBe(false)
  await expect(page.locator('.portal-page .t-skeleton')).toHaveCount(0)
  api.mode('normal'); await store(page, 'return portal.initialize(true)')
  api.mode('denied'); await store(page, 'return portal.initialize(true)')
  expect(await store(page, 'return portal.overviewSpaces.length')).toBe(0)
  await expect(page.locator('.space-summary')).toHaveCount(0)
  api.mode('normal'); await store(page, 'return portal.initialize(true)')
  api.mode('slow'); await store(page, 'void portal.initialize(true)')
  await expect.poll(() => api.reads()).toBe(6)
  await page.evaluate(() => (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('auth').logout())
  api.release()
  await expect(page.locator('.space-summary')).toHaveCount(0)
  expect(await store(page, 'return portal.overviewSpaces.length')).toBe(0)
})

test('confirmed global privilege downgrade clears snapshots even when the user identity is unchanged', async ({ page }) => {
  const api = await setup(page)
  await page.evaluate(() => {
    const auth = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia._s.get('auth')
    auth.setUser({ ...auth.user, is_system_admin: true, can_access_all_tenants: true })
  })
  await expect.poll(() => store(page, 'return portal.overviewSpaces.length')).toBe(12)
  const before = api.reads()
  api.mode('slow'); await store(page, 'void portal.initialize(true)')
  await expect.poll(() => api.reads()).toBe(before + 1)
  const cleared = await page.evaluate(() => {
    const pinia = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia
    const auth = pinia._s.get('auth')
    auth.setUser({ ...auth.user, is_system_admin: false, can_access_all_tenants: false })
    return pinia._s.get('portal').overviewSpaces.length
  })
  expect(cleared).toBe(0)
  api.mode('empty'); api.release()
  await expect.poll(() => api.reads()).toBe(before + 2)
  await expect.poll(() => store(page, 'return portal.initialLoading')).toBe(false)
  await expect(page.locator('.space-summary')).toHaveCount(0)
})

test('confirmed membership grants refresh immediately and revocation clears synchronously', async ({ page }) => {
  const api = await setup(page), before = api.reads()
  const retained = await page.evaluate(() => {
    const pinia = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia
    pinia._s.get('auth').setMemberships([{ tenant_id: 2, role: 'viewer' }])
    return pinia._s.get('portal').overviewSpaces.length
  })
  expect(retained).toBe(12)
  await expect.poll(() => api.reads()).toBe(before + 1)
  await expect.poll(() => store(page, 'return portal.refreshing')).toBe(false)
  await expect(page.locator('.portal-page .t-skeleton')).toHaveCount(0)
  api.mode('empty')
  const cleared = await page.evaluate(() => {
    const pinia = (document.querySelector('#app') as any).__vue_app__.config.globalProperties.$pinia
    pinia._s.get('auth').setMemberships([])
    return pinia._s.get('portal').overviewSpaces.length
  })
  expect(cleared).toBe(0)
  await expect.poll(() => api.reads()).toBe(before + 2)
})
