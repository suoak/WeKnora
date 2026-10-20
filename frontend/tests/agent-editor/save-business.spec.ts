import { test, expect, type Page } from '@playwright/test'
import { fixtureApi, testAgent } from './fixtures'
import { saveAgent, preservedKeys } from './save-data'

async function freshEditor(page: Page) {
  await page.goto('/tests/agent-editor/component.html')
  await page.waitForFunction(() => (window as any).editorHarness)
  const returned = await page.evaluate(async () => {
    const response = await fetch('/api/v1/agents')
    const body = await response.json()
    await (window as any).editorHarness.open(body.data[0])
    return body.data[0]
  })
  const shell = page.locator('.settings-modal-shell')
  await expect(shell.locator('.editor-initializing')).toBeHidden({ timeout: 60000 })
  return { shell, returned }
}
for (const [mode, scenario] of [['quick-answer', 'normal'], ['quick-answer', 'empty'], ['smart-reasoning', 'normal']] as const) {
  test(`independent persisted save: ${mode} ${scenario}`, async ({ page }, info) => {
    const api = await fixtureApi(page, scenario)
    const agent = { ...saveAgent, config: { ...saveAgent.config, agent_mode: mode, allowed_tools: mode === 'smart-reasoning' ? testAgent.config.allowed_tools : [] } }
    api.seedAgent(agent)
    try {
      let { shell } = await freshEditor(page)
      for (const menu of ['model', 'tools', 'mcp', 'skills', 'prompts']) {
        // Tools/MCP are not offered in quick mode; only visit actual menus.
        const link = shell.locator(`[data-guide="agent-editor-nav-${menu}"]`)
        if (await link.count()) await link.click()
      }
      expect(api.mutations).toEqual([])
      await shell.locator('.prompts-panel__pane:visible textarea').first().fill('Persisted edited system')
      const response = page.waitForResponse(r => r.request().method() === 'PUT' && r.url().endsWith('/agents/rc24-isolated-agent'))
      await shell.getByRole('button', { name: '保存并关闭', exact: true }).click()
      expect((await response).status()).toBe(200)
      expect(api.mutations).toHaveLength(1)
      expect(api.mutations[0].method).toBe('PUT')
      expect(api.saved().config.system_prompt).toBe('Persisted edited system')
      for (const key of preservedKeys) expect((api.saved().config as any)[key], key).toEqual((agent.config as any)[key])

      // Deliberately fresh instance + GET of persisted fixture data. This is NOT
      // normal user close/reopen E2E and makes no animation completion claim.
      const reopened = await freshEditor(page)
      shell = reopened.shell
      expect(reopened.returned.config).toEqual(api.saved().config)
      await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
      await expect(shell.locator('.prompts-panel__pane:visible textarea').first()).toHaveValue('Persisted edited system')
      if (mode === 'quick-answer') {
        for (const [label, field] of [['上下文模板', 'context_template'], ['改写 · 系统', 'rewrite_prompt_system'], ['改写 · 用户', 'rewrite_prompt_user'], ['检索兜底', 'fallback_prompt']]) {
          await shell.locator('.prompts-outline__pill').getByText(label, { exact: true }).click()
          await expect(shell.locator('.prompts-panel__pane:visible textarea').first()).toHaveValue((agent.config as any)[field])
        }
      }
      expect(api.mutations).toHaveLength(1)
      expect(api.pageErrors).toEqual([])
      expect(api.consoleErrors.filter(e => /TypeError|Unhandled|panel failed/.test(e))).toEqual([])
    } finally {
      await info.attach('save-evidence', { body: JSON.stringify({ mutations: api.mutations, persisted: api.saved(), responses: api.responses, console: api.consoleErrors, pageErrors: api.pageErrors }), contentType: 'application/json' })
      await info.attach('DOM', { body: await page.content(), contentType: 'text/html' })
    }
  })
}

test('failed save retains draft; one explicit retry persists it', async ({ page }, info) => {
  const api = await fixtureApi(page)
  api.seedAgent(saveAgent)
  api.setSaveStatus(500)
  try {
    const { shell } = await freshEditor(page)
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    const input = shell.locator('.prompts-panel__pane:visible textarea').first()
    await input.fill('Retry preserved draft')
    const response = page.waitForResponse(r => r.request().method() === 'PUT')
    await shell.getByRole('button', { name: '保存并关闭', exact: true }).click()
    expect((await response).status()).toBe(500)
    await expect(shell.getByRole('button', { name: '保存并关闭', exact: true })).toBeEnabled()
    await expect(shell).toBeVisible()
    await expect(input).toHaveValue('Retry preserved draft')
    expect(api.saved().config.system_prompt).toBe(saveAgent.config.system_prompt)
    expect(api.mutations).toHaveLength(1)
    await shell.locator('[data-guide="agent-editor-nav-model"]').click()
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(input).toHaveValue('Retry preserved draft')
    expect(api.mutations).toHaveLength(1)
    api.setSaveStatus(200)
    const retry = page.waitForResponse(r => r.request().method() === 'PUT')
    await shell.getByRole('button', { name: '保存并关闭', exact: true }).click()
    expect((await retry).status()).toBe(200)
    expect(api.mutations).toHaveLength(2)
    expect(api.saved().config.system_prompt).toBe('Retry preserved draft')
    for (const key of preservedKeys) expect((api.saved().config as any)[key], key).toEqual((saveAgent.config as any)[key])
    const fresh = await freshEditor(page)
    await fresh.shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(fresh.shell.locator('.prompts-panel__pane:visible textarea').first()).toHaveValue('Retry preserved draft')
    expect(api.mutations).toHaveLength(2)
    expect(api.pageErrors).toEqual([])
    expect(api.consoleErrors.filter(e => /TypeError|Unhandled|panel failed/.test(e))).toEqual([])
  } finally {
    await info.attach('save-evidence', { body: JSON.stringify({ mutations: api.mutations, persisted: api.saved(), responses: api.responses, console: api.consoleErrors, pageErrors: api.pageErrors }), contentType: 'application/json' })
    await info.attach('DOM', { body: await page.content(), contentType: 'text/html' })
  }
})
