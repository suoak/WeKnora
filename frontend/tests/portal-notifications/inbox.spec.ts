import { test, expect } from '@playwright/test'
import { fixtureApi } from '../agent-editor/fixtures'

// Browser interaction tests with explicit synthetic API data. These do not
// replace the required two-account acceptance against a deployed backend.
test('unified inbox preserves zero-count entry, read state, applications and invitations', async ({ page }) => {
  const fixture = await fixtureApi(page)
  let isRead = false, cancelled = false
  let count = 120
  const mutations: string[] = []
  await page.route('**/api/v1/me/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/me/notifications/unread-count') return route.fulfill({ json: { success: true, data: { unread_count: count } } })
    if (path === '/api/v1/me/notifications') return route.fulfill({ json: { success: true, data: isRead && new URL(route.request().url()).searchParams.get('unread') === 'true' ? [] : [{ id: 'n1', notification_type: 'access_request_rejected', reference_type: 'tenant_access_request', reference_id: 'r1', tenant_id: 7, title: '知识空间访问申请未通过', content: '审批意见：请补充业务背景', is_read: isRead, created_at: '2026-11-21T00:00:00Z' }], total: 1 } })
    if (/\/notifications\/(?:read-all|n1\/read)$/.test(path)) {
      mutations.push(path); isRead = true; count = 0
      return route.fulfill({ json: { success: true } })
    }
    if (path === '/api/v1/me/tenant-access-requests') return route.fulfill({ json: { success: true, data: [{ id: 'r2', tenant_id: 7, tenant_name: '概念2', reason: '研发需求检索', status: cancelled ? 'cancelled' : 'pending', created_at: '2026-11-21T00:00:00Z' }] } })
    if (path === '/api/v1/me/tenant-access-requests/r2/cancel') {
      mutations.push(path); cancelled = true
      return route.fulfill({ json: { success: true } })
    }
    if (path.endsWith('/invitations/pending-count')) return route.fulfill({ json: { success: true, data: { pending_count: 2 } } })
    if (path.endsWith('/invitations')) return route.fulfill({ json: { success: true, data: { invitations: [], total: 0 } } })
    return route.fallback()
  })
  await page.goto('/portal')
  const bell = page.getByRole('button', { name: '消息中心', exact: true })
  await expect(bell).toBeVisible()
  await expect(page.getByText('99+', { exact: true })).toBeVisible()
  await bell.click()
  await expect(page.getByText('审批意见：请补充业务背景', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '全部已读', exact: true }).click()
  await expect(page.getByRole('button', { name: '已读', exact: true })).toBeDisabled()
  await page.getByText('仅未读', { exact: true }).click()
  await expect(page.getByText('暂无消息通知', { exact: true })).toBeVisible()
  await page.getByText('我的申请', { exact: true }).click()
  await expect(page.getByText('研发需求检索', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '取消申请', exact: true }).click()
  await expect(page.getByText('已取消', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '空间邀请 (2)' }).click()
  await expect(page.getByText('没有待处理的邀请。', { exact: true })).toBeVisible()
  expect(mutations).toContain('/api/v1/me/notifications/read-all')
  expect(mutations).toContain('/api/v1/me/tenant-access-requests/r2/cancel')
  expect(fixture.pageErrors).toEqual([])
})

test('mobile dark portal shows a single bell with zero unread messages', async ({ page }) => {
  await fixtureApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.addInitScript(() => localStorage.setItem('theme-mode', 'dark'))
  await page.route('**/api/v1/me/notifications/unread-count', route => route.fulfill({ json: { success: true, data: { unread_count: 0 } } }))
  await page.goto('/portal')
  await page.evaluate(() => document.documentElement.setAttribute('theme-mode', 'dark'))
  const bell = page.getByRole('button', { name: '消息中心', exact: true })
  await expect(bell).toHaveCount(1)
  await expect(bell).toBeVisible()
  const rect = await bell.boundingBox()
  expect(rect).not.toBeNull()
  expect(rect!.x + rect!.width).toBeLessThanOrEqual(390)
  await bell.click()
  await expect(page.getByText('我的申请', { exact: true })).toBeVisible()
  await expect.poll(async () => {
    const drawer = await page.locator('.t-drawer__content-wrapper').filter({ has: page.locator('.inbox') }).boundingBox()
    return drawer ? drawer.x + drawer.width : Infinity
  }).toBeLessThanOrEqual(390)
  const close = page.locator('.t-drawer__close-btn').filter({ visible: true })
  await expect(close).toBeVisible()
  expect(await page.evaluate(() => Boolean(document.elementFromPoint(25, 25)?.closest('.mobile-nav-trigger')))).toBe(false)
  await page.screenshot({ path: 'test-results/portal-notifications/mobile-dark.png', animations: 'disabled' })
  await close.click()
  await expect(page.getByText('我的申请', { exact: true })).not.toBeVisible()
})

test('reading the last unread item on page two returns to the remaining first page', async ({ page }) => {
  await fixtureApi(page)
  let removed = false
  await page.route('**/api/v1/me/notifications**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/unread-count')) return route.fulfill({ json: { success: true, data: { unread_count: removed ? 20 : 21 } } })
    if (url.pathname.endsWith('/n21/read')) { removed = true; return route.fulfill({ json: { success: true } }) }
    const second = url.searchParams.get('page') === '2'
    const data = removed && second ? [] : [{ id: second ? 'n21' : 'n1', notification_type: 'access_request_rejected', reference_type: 'tenant_access_request', reference_id: 'r1', tenant_id: 7, title: second ? '最后一条未读消息' : '第一页消息', content: '分页测试', is_read: false, created_at: '2026-11-21T00:00:00Z' }]
    return route.fulfill({ json: { success: true, data, total: removed ? 20 : 21 } })
  })
  await page.goto('/portal')
  await page.getByRole('button', { name: '消息中心', exact: true }).click()
  await page.getByText('仅未读', { exact: true }).click()
  await page.locator('.t-pagination__btn-next').click()
  await expect(page.getByText('最后一条未读消息', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '标为已读', exact: true }).click()
  await expect(page.getByText('第一页消息', { exact: true })).toBeVisible()
})
