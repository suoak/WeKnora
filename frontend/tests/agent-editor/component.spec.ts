import { test, expect } from '@playwright/test'
import { fixtureApi, modelRows, testAgent } from './fixtures'

for (const scenario of ['normal', 'empty', 'failure'] as const) {
  test(`configured sandbox skills: ${scenario}`, async ({ page }) => {
    const api = await fixtureApi(page)
    await page.route('**/api/v1/sandbox-configs', route => route.fulfill({ json: { data: [{ id: 'isolated-sandbox', name: 'Isolated Sandbox', sandbox_type: 'docker', config: {} }] } }))
    await page.route('**/api/v1/skills/catalog', route => route.fulfill({ json: { data: scenario === 'normal' ? [{ id: 'isolated-catalog', name: 'isolated-skill', description: 'Isolated skill description', installations: [{ sandbox_config_id: 'isolated-sandbox', skill_id: 'isolated-install', status: 'ready', enabled: true }] }] : [] } }))
    await page.route('**/api/v1/skills?*', route => route.fulfill(scenario === 'failure'
      ? { status: 500, json: { message: 'Isolated installed skills failure' } }
      : { json: { skills_available: true, data: scenario === 'empty' ? [] : [{ name: 'isolated-skill', description: 'Isolated skill description' }] } }))
    await page.goto('/tests/agent-editor/component.html')
    await page.waitForFunction(() => (window as any).editorHarness)
    await page.evaluate(agent => (window as any).editorHarness.open(agent), { ...testAgent, config: { ...testAgent.config, sandbox_config_id: 'isolated-sandbox' } })
    const shell = page.locator('.settings-modal-shell')
    await expect(shell.locator('.editor-initializing')).toBeHidden()
    await shell.locator('[data-guide="agent-editor-nav-skills"]').click()
    const panel = shell.locator('.section:visible')
    await expect(panel).toBeVisible()
    await expect(panel.locator('.sandbox-config-select input')).toHaveValue('Isolated Sandbox')
    if (scenario === 'normal') await expect(panel).toContainText('isolated-skill')
    if (scenario === 'failure') await expect(panel).toContainText('配置数据加载失败')
    expect(api.responses.some(response => response.url.includes('/api/v1/skills?'))).toBe(true)
    expect(api.mutations).toEqual([])
    expect(api.pageErrors).toEqual([])
  })
}

for (const scenario of ['normal', 'empty', 'failure'] as const) {
  test(`model and tools components: ${scenario}`, async ({ page }) => {
    const api = await fixtureApi(page, scenario)
    await page.goto('/tests/agent-editor/component.html')
    await page.waitForFunction(() => (window as any).editorHarness)
    await page.evaluate(agent => (window as any).editorHarness.open(agent), testAgent)
    const shell = page.locator('.settings-modal-shell')
    await expect(shell.locator('.editor-initializing')).toBeHidden()
    await shell.locator('[data-guide="agent-editor-nav-model"]').click()
    const panel = shell.locator('.section:visible')
    await expect(panel.getByRole('heading', { name: '模型配置', exact: true })).toBeVisible()
    if (scenario === 'normal') await expect(panel.locator('[data-guide="agent-create-model"]')).toContainText('Local Chat')
    else await expect(panel.locator('[data-guide="agent-create-model"] input')).toHaveValue('local-chat-model')
    if (scenario === 'empty') await expect(panel).toContainText('暂无可用对话模型')
    if (scenario === 'failure') {
      await expect(panel).toContainText('配置数据加载失败')
      api.recover()
      await panel.getByRole('button', { name: '重试', exact: true }).first().click()
      await expect(panel).not.toContainText('配置数据加载失败')
      await expect(panel).toContainText('Local Chat')
    }
    await shell.locator('[data-guide="agent-editor-nav-tools"]').click()
    await expect(panel.getByRole('heading', { name: '工具配置', exact: true })).toBeVisible()
    await expect(panel.locator('.tool-card')).not.toHaveCount(0)
    await expect(panel).toContainText('思考')
    expect(api.mutations).toEqual([])
    expect(api.pageErrors).toEqual([])
    expect(api.consoleErrors.filter(error => /TypeError|Unhandled|panel failed/.test(error))).toEqual([])
  })
}

test('one failed child is isolated and can recover without losing other panels', async ({ page }) => {
  const api = await fixtureApi(page)
  await page.goto('/tests/agent-editor/component.html')
  await page.waitForFunction(() => (window as any).editorHarness)
  await page.evaluate(() => (window as any).editorHarness.boundary())
  await expect(page.getByRole('alert')).toContainText('模型配置显示失败')
  await expect(page.getByText('Other panel remains available')).toBeVisible()
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('Recovered child')).toBeVisible()
  expect(api.pageErrors).toEqual([])
  expect(api.consoleErrors.some(error => error.includes('isolated model child failure'))).toBe(true)
})

test('stopped model remains visible, disabled, and never replaces the saved model', async ({ page }) => {
  const api = await fixtureApi(page, 'stopped')
  await page.goto('/tests/agent-editor/component.html')
  await page.waitForFunction(() => (window as any).editorHarness)
  await page.evaluate(agent => (window as any).editorHarness.open(agent), testAgent)
  const shell = page.locator('.settings-modal-shell')
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toContainText('已配置模型不可用（local-chat-model）')
  await expect(shell.locator('[data-guide="agent-create-model"]')).toContainText('Local Chat')
  await shell.locator('[data-guide="agent-create-model"] .t-select').click()
  await expect(page.locator('.t-select-option.t-is-disabled').filter({ hasText: 'Local Chat' })).toBeVisible()
  expect(api.mutations).toEqual([])
})

test('loading and partial model failure leave successful knowledge/tool resources available', async ({ page }) => {
  const api = await fixtureApi(page)
  let release!: () => void
  const gate = new Promise<void>(resolve => release = resolve)
  await page.route('**/api/v1/models', async route => {
    await gate
    await route.fulfill({ status: 500, json: { success: false, message: 'Isolated model failure' } })
  })
  await page.goto('/tests/agent-editor/component.html')
  await page.waitForFunction(() => (window as any).editorHarness)
  await page.evaluate(agent => (window as any).editorHarness.open(agent), testAgent)
  const shell = page.locator('.settings-modal-shell')
  await expect(shell.locator('.editor-initializing')).toBeVisible()
  release()
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.locator('[data-guide="agent-editor-nav-tools"]').click()
  await expect(shell.locator('.section:visible')).not.toContainText('配置数据加载失败')
  await expect(shell.locator('.section:visible .tool-card')).not.toHaveCount(0)
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toContainText('配置数据加载失败')
  await page.unroute('**/api/v1/models')
  const retryGate = new Promise<void>(resolve => release = resolve)
  await page.route('**/api/v1/models', async route => {
    await retryGate
    await route.fulfill({ json: { success: true, data: modelRows } })
  })
  await shell.locator('.section:visible').getByRole('button', { name: '重试', exact: true }).click()
  await expect(shell.locator('.section:visible [role="status"]')).toBeVisible()
  release()
  await expect(shell.locator('.section:visible')).toContainText('Local Chat')
  expect(api.mutations).toEqual([])
})
