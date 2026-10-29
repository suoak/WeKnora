import { test, expect } from '@playwright/test'
import { fixtureApi } from '../agent-editor/fixtures'

test('personal Key reuse, client formats, legacy history, pagination and responsive themes', async ({ page }) => {
  const api = await fixtureApi(page)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  const mutations: string[] = []
  const credentials = Array.from({ length: 14 }, (_, index) => ({
    id: index + 1, name: index === 0 ? '个人知识 Key' : `历史凭证 ${index + 1}`,
    client_type: index === 1 ? 'workmate' : 'generic', scope_type: 'user_mcp',
    status: index > 9 ? 'revoked' : 'active', token_hint: 'kh_…1234', capabilities: index === 0 ? ['retrieve', 'enterprise_retrieve'] : ['retrieve', 'chat'],
    tenant_scopes: [{ tenant_id: 1, kb_scope_mode: 'all', knowledge_bases: [] }],
    created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
  }))
  await page.route('**/api/v1/mcp-api-keys**', async route => {
    if (route.request().method() !== 'GET') mutations.push(route.request().method())
    await route.fulfill({ json: {
      success: true, mcp_public_url: 'https://knowhub.example/mcp',
      data: route.request().url().includes('scope-options')
        ? [{ tenant_id: 1, tenant_name: '研发空间', role: 'owner', owned_knowledge_bases: [{ id: 'kb1', name: '知识库' }], shared_knowledge_bases: [] }]
        : credentials,
    } })
  })
  await page.goto('/platform/mcp-access')
  await expect(page.getByRole('heading', { name: '我的个人 MCP Key' })).toBeVisible()
  await expect(page.locator('.personal-summary')).toContainText('个人知识 Key')
  await expect(page.locator('.client-surface button')).toHaveCount(7)
  await expect(page.locator('.credential-card')).toHaveCount(0)
  await expect(page.locator('.client-surface')).not.toContainText('WorkMate')
  await expect(page.locator('.client-surface')).not.toContainText('Cursor')
  await page.screenshot({ path: 'test-results/mcp-access/desktop-light.png', fullPage: true })

  for (const name of ['Claude Code', 'Codex', 'GitHub Copilot', 'VS Code', 'WorkBuddy', 'Pi', '其他 MCP 客户端']) {
    await page.locator('.client-surface').getByRole('button', { name, exact: true }).click()
    const dialog = page.locator('.t-dialog:visible')
    await expect(dialog.locator('.t-select input').nth(1)).toHaveValue('个人知识 Key')
    await expect(dialog).toContainText('客户端待验证')
    const config = await dialog.locator('pre').innerText()
    expect(config).toContain('/api/v1/mcp/personal')
    if (name === 'Codex') expect(config).toContain('[mcp_servers.')
    else {
      const parsed = JSON.parse(config)
      expect(Boolean(parsed.servers)).toBe(name === 'VS Code' || name === 'GitHub Copilot')
    }
    await dialog.getByRole('textbox', { name: '已保存的 Secret' }).fill('synthetic-secret')
    if (!['Codex', 'VS Code', 'GitHub Copilot'].includes(name)) await expect(dialog.locator('pre')).toContainText('Bearer synthetic-secret')
    await dialog.getByRole('button', { name: /复制.*配置/ }).click()
    expect((await page.evaluate(() => navigator.clipboard.readText())).replaceAll('\r\n', '\n')).toBe(await dialog.locator('pre').innerText())
    await page.screenshot({ path: `test-results/mcp-access/client-${name.replaceAll(' ', '-')}.png` })
    await dialog.locator('.t-dialog__close').click()
  }
  expect(mutations).toEqual([])
  await page.getByRole('button', { name: /历史接入 ·/ }).click()
  await expect(page.locator('.credential-card')).toHaveCount(5)
  await expect(page.locator('.credential-list')).toContainText('WorkMate')
  await page.screenshot({ path: 'test-results/mcp-access/history-expanded.png', fullPage: true })
  await page.locator('.t-pagination__number').filter({ hasText: /^2$/ }).click()
  await expect(page.locator('.credential-list')).toContainText('历史凭证 6')
  await page.locator('.history-surface .t-radio-button').filter({ hasText: '已停用' }).click()
  await expect(page.locator('.credential-card')).toHaveCount(4)
  await page.getByRole('button', { name: /历史接入 ·/ }).click()
  for (const [width, theme] of [[1440, 'dark'], [1024, 'light'], [768, 'dark'], [390, 'light']] as const) {
    await page.setViewportSize({ width, height: 900 })
    await page.evaluate(value => document.documentElement.setAttribute('theme-mode', value), theme)
    await expect(page.locator('.personal-summary')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: `test-results/mcp-access/${width}-${theme}.png`, fullPage: true })
    const history = page.getByRole('button', { name: /历史接入 ·/ })
    await history.scrollIntoViewIfNeeded()
    await expect(history).toBeInViewport()
  }
  // Exercise the same CSS zoom mechanism used by the app's global UI scale.
  // These checks supplement reduced CSS viewports; they are not OS display scaling.
  await page.setViewportSize({ width: 1366, height: 768 })
  for (const zoom of [1, 1.25, 1.5]) {
    await page.evaluate(value => { document.documentElement.style.zoom = String(value) }, zoom)
    await page.locator('.client-surface').getByRole('button', { name: 'Pi', exact: true }).click()
    const dialog = page.locator('.t-dialog:visible')
    await dialog.getByRole('button', { name: /复制配置/ }).scrollIntoViewIfNeeded()
    await expect(dialog.getByRole('button', { name: /复制配置/ })).toBeInViewport()
    await expect(dialog.locator('.t-dialog__close')).toBeInViewport()
    const bounds = await dialog.boundingBox()
    expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(768)
    await dialog.locator('.config-steps li').last().scrollIntoViewIfNeeded()
    await expect(dialog.locator('.config-steps li').last()).toBeInViewport()
    await page.screenshot({ path: `test-results/mcp-access/1366-zoom-${zoom}.png` })
    await dialog.locator('.t-dialog__close').click()
  }
  expect(api.pageErrors).toEqual([])
})
