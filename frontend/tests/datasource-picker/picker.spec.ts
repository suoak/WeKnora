import { test, expect } from '@playwright/test'

test('large child lists stay virtual and preserve unrendered siblings when unchecked', async ({ page }) => {
  const errors: string[] = []
  const childOffsets: number[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (!url.pathname.endsWith('/resources')) return route.fulfill({ json: { success: true, data: [] } })
    const parent = url.searchParams.get('parent_id')
    if (!parent) return route.fulfill({ json: { items: [{ external_id: '1', name: 'Engineering', type: 'space', has_children: true, metadata: { space_key: 'ENG' } }] } })
    const start = Number(url.searchParams.get('cursor') || 0)
    childOffsets.push(start)
    return route.fulfill({ json: {
      items: Array.from({ length: 50 }, (_, offset) => ({
        external_id: `page:1:${start + offset + 1}`, parent_id: '1',
        name: `Page ${start + offset + 1}`, type: 'page', has_children: false,
      })),
      next_cursor: start + 50 < 1000 ? String(start + 50) : '',
    } })
  })
  await page.goto('/tests/datasource-picker/index.html')
  await page.getByRole('button', { name: '下一步', exact: true }).click()
  const root = page.getByRole('treeitem').filter({ hasText: /^Engineering$/ })
  await expect(root).toBeVisible()
  await root.click()
  await root.locator('.resource-picker__expand').click()
  await expect.poll(() => childOffsets.length).toBe(1)
  await expect(page.getByRole('treeitem', { name: 'Page 1', exact: true })).toBeVisible()
  await page.getByRole('treeitem', { name: 'Page 1', exact: true }).click()
  await expect(page.locator('.resource-picker__count')).toContainText('999')
  expect(childOffsets).toHaveLength(20)
  expect(await page.locator('.resource-picker__row').count()).toBeLessThan(80)
  expect(await page.locator('.resource-picker__selected .t-tag').count()).toBeLessThanOrEqual(50)
  const list = page.locator('.resource-picker__list')
  // Dynamic row heights are measured as new rows enter the viewport. Continue
  // scrolling while that measurement settles, just as repeated wheel input does.
  await expect(async () => {
    await list.evaluate(element => { element.scrollTop = element.scrollHeight })
    await expect(page.getByRole('treeitem', { name: 'Page 1000', exact: true })).toBeInViewport()
  }).toPass({ timeout: 10000 })
  await expect(page.getByRole('treeitem', { name: 'Page 1000', exact: true })).toHaveClass(/is-checked/)
  await page.screenshot({ path: 'test-results/datasource-picker/large-list.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: 'test-results/datasource-picker/mobile.png', fullPage: true })
  expect(errors).toEqual([])
})

test('saved page tags reveal an off-page path without listing sibling pages', async ({ page }) => {
  const requests: string[] = []
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    if (!url.pathname.endsWith('/resources')) return route.fulfill({ json: { success: true, data: [] } })
    requests.push(url.search)
    if (url.searchParams.has('reveal_id')) return route.fulfill({ json: { items: [
      { external_id: '2', name: 'Other Space', type: 'space', has_children: true, metadata: { space_key: 'OTHER' } },
      { external_id: 'page:2:10', parent_id: '2', name: 'Ancestor', type: 'page', has_children: true },
      { external_id: 'page:2:30', parent_id: 'page:2:10', name: 'Saved Page', type: 'page', has_children: true },
    ] } })
    return route.fulfill({ json: { items: [{ external_id: '1', name: 'Engineering', type: 'space', has_children: true, metadata: { space_key: 'ENG' } }] } })
  })
  await page.goto('/tests/datasource-picker/index.html?selected=page%3A2%3A30')
  await page.getByRole('button', { name: '下一步', exact: true }).click()
  await page.getByRole('button', { name: 'page:2:30', exact: true }).click()
  await expect(page.getByRole('treeitem').filter({ hasText: /^Saved Page$/ })).toBeInViewport()
  await expect(page.getByRole('treeitem').filter({ hasText: /^Saved Page$/ })).toHaveClass(/is-checked/)
  expect(requests).toHaveLength(2)
  expect(requests.some(query => query.includes('parent_id'))).toBe(false)
})

test('system Confluence shows no identity fields and sends only a profile reference', async ({ page }) => {
  const mutations: any[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname.endsWith('/types')) return route.fulfill({ json: [{ type: 'confluence', system_account: { enabled: true, base_url: 'http://confluence.internal' } }] })
    if (request.method() === 'POST') mutations.push(request.postDataJSON())
    if (url.pathname.endsWith('/validate-credentials')) return route.fulfill({ json: { status: 'connected' } })
    if (url.pathname.endsWith('/datasource')) return route.fulfill({ json: { id: 'created-system-source' } })
    if (url.pathname.endsWith('/resources')) return route.fulfill({ json: { items: [{ external_id: '1', name: 'Engineering', type: 'space', has_children: true, metadata: { space_key: 'ENG' } }] } })
    return route.fulfill({ json: { success: true, data: [] } })
  })
  await page.goto('/tests/datasource-picker/index.html?create=1')
  await page.getByText('Confluence', { exact: true }).click()
  await expect(page.getByText('使用系统内置账号连接，无需填写账号或密码。凭证由系统管理员维护。', { exact: true })).toBeVisible()
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
  await expect(page.getByText('用户名或邮箱', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: 'test-results/datasource-picker/system-account.png', fullPage: true, animations: 'disabled' })
  await page.getByRole('button', { name: '下一步', exact: true }).click()
  await expect(page.getByRole('treeitem').filter({ hasText: /^Engineering$/ })).toBeVisible()
  const validation = mutations.find(item => item.type === 'confluence' && item.credentials)
  expect(validation.credentials).toEqual({ credential_source: 'system' })
  const creation = mutations.find(item => item.config)
  expect(creation.config.settings.credential_source).toBe('system')
  expect(JSON.stringify(mutations)).not.toMatch(/"(?:username|password|api_token)"/)
})

test('editing a system connection exposes no credential replacement or removal controls', async ({ page }) => {
  await page.route('**/api/v1/datasource/types', route => route.fulfill({ json: [{ type: 'confluence', system_account: { enabled: true, base_url: 'http://confluence.internal' } }] }))
  await page.goto('/tests/datasource-picker/index.html?system=1')
  await expect(page.getByText('使用系统内置账号连接，无需填写账号或密码。凭证由系统管理员维护。', { exact: true })).toBeVisible()
  await expect(page.locator('.credential-actions')).toHaveCount(0)
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
})
