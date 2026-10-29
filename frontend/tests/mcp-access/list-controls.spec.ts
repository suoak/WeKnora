import { test, expect } from '@playwright/test'
import { keyFixture } from './fixtures'

for (const count of [0, 1, 2, 5, 6]) {
  test(`${count} personal Keys, uniform status and no historical credentials`, async ({ page }) => {
    const api = await keyFixture(page)
    const base = { ...api.rows[2] }
    api.rows.splice(0, api.rows.length, ...Array.from({ length: count }, (_, i) => ({ ...base, id: i + 1, name: `个人知识 Key ${i + 1}` })))
    await page.goto('/platform/mcp-access')
    await expect(page.locator('.personal-key-card')).toHaveCount(Math.min(5, count))
    await expect(page.locator('.history-surface')).toHaveCount(0)
    await expect(page.locator('.personal-surface .status-tabs')).toHaveCount(0)
    await expect(page.getByPlaceholder('搜索 Key 名称')).toHaveCount(count > 5 ? 1 : 0)
    await expect(page.locator('.personal-surface .t-pagination')).toHaveCount(count > 5 ? 1 : 0)
    await expect(page.locator('.list-heading')).toContainText(`共 ${count} 个 Key${count ? '，均已启用' : ''}`)
    if (!count) await expect(page.getByText('暂无个人 Key', { exact: true })).toBeVisible()
    await page.screenshot({ path: `test-results/mcp-access/keys-${count}.png`, fullPage: true, animations: 'disabled' })
    if (count === 6) {
      await page.locator('.personal-surface .t-pagination__number').filter({ hasText: /^2$/ }).click()
      await expect(page.locator('.personal-key-card')).toHaveCount(1)
      await expect(page.locator('.personal-key-card')).toHaveAttribute('data-key-id', '6')
    }
    if (count === 2) {
      await page.setViewportSize({ width: 390, height: 844 })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      await page.locator('.personal-key-card').last().getByRole('button', { name: '连接 AI 工具' }).scrollIntoViewIfNeeded()
      await expect(page.locator('.personal-key-card').last().getByRole('button', { name: '连接 AI 工具' })).toBeInViewport()
      await page.screenshot({ path: 'test-results/mcp-access/keys-2-mobile.png', animations: 'disabled', fullPage: true })
    }
    expect(api.pageErrors).toEqual([])
  })
}

test('small mixed list shows every Key; controls reset when search, filtering or data shrink', async ({ page }) => {
  const api = await keyFixture(page)
  await page.goto('/platform/mcp-access')
  await expect(page.locator('.personal-surface').locator('.t-tabs__nav-item').filter({ hasText: /已过期/ })).toBeVisible()
  await page.locator('.personal-surface .t-pagination__number').filter({ hasText: /^2$/ }).click()
  await page.locator('.personal-surface').locator('.t-tabs__nav-item').filter({ hasText: /已过期/ }).click()
  await expect(page.locator('.personal-key-card')).toHaveCount(1)
  await expect(page.locator('.personal-key-card')).toHaveAttribute('data-key-id', '2')
  await expect(page.locator('.personal-surface .t-pagination')).toHaveCount(0)
  await page.locator('.personal-surface').locator('.t-tabs__nav-item').filter({ hasText: /全部/ }).click()
  await page.getByPlaceholder('搜索 Key 名称').fill('指定')
  await expect(page.locator('.personal-key-card')).toHaveCount(1)
  await expect(page.locator('.personal-surface .t-pagination')).toHaveCount(0)
  await page.getByPlaceholder('搜索 Key 名称').fill('不存在的 Key')
  await expect(page.locator('.personal-key-card')).toHaveCount(0)
  await expect(page.getByText('当前筛选下没有个人 Key', { exact: true })).toBeVisible()
  await expect(page.locator('.personal-surface .t-pagination')).toHaveCount(0)
  // Empty personal search must not hide the independent raw historical set.
  await expect(page.locator('.history-surface')).toBeVisible()
  await page.getByRole('button', { name: /历史接入 ·/ }).click()
  await page.locator('.history-surface .t-pagination__number').filter({ hasText: /^4$/ }).click()
  await page.locator('.history-surface').locator('.t-tabs__nav-item').filter({ hasText: /已停用/ }).click()
  await expect(page.locator('.history-surface .credential-card')).toHaveCount(1)
  await expect(page.locator('.history-surface .t-pagination')).toHaveCount(0)
  await page.locator('.history-surface').locator('.t-tabs__nav-item').filter({ hasText: /全部/ }).click()
  await expect(page.locator('.history-surface .credential-card')).toHaveCount(5)
  await expect(page.locator('.history-surface .credential-list')).toContainText('WorkMate')
  await page.screenshot({ path: 'test-results/mcp-access/mixed-empty-search-history.png', fullPage: true, animations: 'disabled' })
  // Refresh to 5 mixed Keys and 1 legacy row while stale search/filter/page state exists.
  const small = api.rows.slice(0, 5), legacy = api.rows[8]
  api.rows.splice(0, api.rows.length, ...small, legacy)
  await page.locator('.personal-surface').getByRole('button', { name: /刷新/ }).click()
  await expect(page.locator('.personal-key-card')).toHaveCount(5)
  await expect(page.getByPlaceholder('搜索 Key 名称')).toHaveCount(0)
  await expect(page.locator('.personal-surface .status-tabs')).toHaveCount(0)
  await expect(page.locator('.personal-surface .t-pagination')).toHaveCount(0)
  await expect(page.locator('.personal-surface')).toContainText('已过期')
  await expect(page.locator('.personal-surface')).toContainText('已停用')
  await expect(page.locator('.history-surface .t-pagination')).toHaveCount(0)
  await expect(page.locator('.history-surface .status-tabs')).toHaveCount(0)
  await page.screenshot({ path: 'test-results/mcp-access/keys-5-mixed.png', fullPage: true, animations: 'disabled' })
  api.rows.splice(0, api.rows.length, ...small.slice(0, 2))
  await page.locator('.personal-surface').getByRole('button', { name: /刷新/ }).click()
  await expect(page.locator('.personal-key-card')).toHaveCount(2)
  await expect(page.locator('.history-surface')).toHaveCount(0)
  expect(api.pageErrors).toEqual([])
})
