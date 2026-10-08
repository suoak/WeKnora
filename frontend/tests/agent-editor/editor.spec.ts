import { test, expect } from '@playwright/test'
import { fixtureApi, menuKeys, testAgent } from './fixtures'

for (const mode of ['create', 'edit'] as const) {
  for (const scenario of ['normal', 'empty', 'failure'] as const) {
    test(`${mode} editor: all menus with ${scenario} data`, async ({ page }, testInfo) => {
      // Keep the existing creation prerequisite: a chat model must be present at entry.
      // Then simulate deletion/failure when the editor refreshes its dependencies.
      const api = await fixtureApi(page, mode === 'create' ? 'normal' : scenario)
      const initialModels = mode === 'create' ? page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/models') : null
      await page.goto(mode === 'edit' ? '/platform/agents?edit=rc24-isolated-agent' : '/platform/agents')
      if (mode === 'create') {
        const response = await initialModels!
        expect(response.status()).toBe(200)
        expect((await response.json()).data.length).toBeGreaterThan(0)
        api.setScenario(scenario)
        await page.locator('[data-guide="agent-list-create"]').first().click()
      }
      const shell = page.locator('.settings-modal-shell').last()
      await expect(shell.locator('[data-guide="agent-editor-nav-model"]')).toBeVisible({ timeout: 60000 })
      await expect(shell.locator('.editor-initializing')).toBeHidden({ timeout: 60000 })
      if (mode === 'create') await shell.getByText('智能推理', { exact: true }).click()
      const keys = mode === 'edit' ? [...menuKeys, 'share'] : menuKeys
      for (const key of keys) {
        await shell.locator(`[data-guide="agent-editor-nav-${key}"]`).click()
        const panel = shell.locator('.section:visible')
        await expect(panel).toHaveCount(1)
        expect((await panel.innerText()).trim().length, key).toBeGreaterThan(10)
        await expect(panel).toBeVisible()
        if (scenario === 'normal' && mode === 'edit' && ['model', 'tools'].includes(key)) {
          await page.screenshot({ path: testInfo.outputPath(`${key}.png`), timeout: 120000 })
        }
      }
      expect(api.mutations).toEqual([])
      expect(api.pageErrors).toEqual([])
      expect(api.consoleErrors.filter(error => /TypeError|Unhandled|panel failed/.test(error))).toEqual([])
      await testInfo.attach('network-and-console', { body: JSON.stringify({ responses: api.responses, console: api.consoleErrors, pageErrors: api.pageErrors }), contentType: 'application/json' })
    })
  }
}

test('quick answer exposes retrieval, and disabled/missing models retain stored identifiers', async ({ page }) => {
  const api = await fixtureApi(page, 'unavailable')
  await page.goto('/tests/agent-editor/component.html')
  await page.waitForFunction(() => (window as any).editorHarness)
  const agent = { ...testAgent, config: { ...testAgent.config, agent_mode: 'quick-answer', kb_selection_mode: 'selected', knowledge_bases: ['local-kb'] } }
  await page.evaluate(agent => (window as any).editorHarness.open(agent), agent)
  const shell = page.locator('.settings-modal-shell')
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.locator('[data-guide="agent-editor-nav-retrieval"]').click()
  await expect(shell.locator('.section:visible')).toContainText('检索策略')
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toContainText('local-chat-model')
  await expect(shell.locator('.section:visible')).toContainText('local-rerank-model')
  expect(api.mutations).toEqual([])
})

test('forbidden resources show permission messages and retry without losing the draft', async ({ page }) => {
  const api = await fixtureApi(page, 'forbidden')
  await page.goto('/platform/agents?edit=rc24-isolated-agent')
  const shell = page.locator('.settings-modal-shell').last()
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toContainText('无权读取')
  api.recover()
  await shell.locator('.section:visible').getByRole('button', { name: '重试', exact: true }).first().click()
  await expect(shell.locator('.section:visible')).toContainText('Local Chat')
  expect(api.mutations).toEqual([])
})

test('edit, save, reopen: models, tools, MCP and unavailable skill choices survive', async ({ page }) => {
  const api = await fixtureApi(page)
  await page.goto('/platform/agents?edit=rc24-isolated-agent')
  const shell = page.locator('.settings-modal-shell').last()
  await expect(shell.locator('[data-guide="agent-editor-nav-model"]')).toBeVisible()
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.locator('.section:visible textarea').fill('Updated isolated description')
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await shell.locator('[data-guide="agent-create-model"] .t-select').click()
  await page.getByText('Second Chat', { exact: true }).last().click()
  await shell.getByRole('button', { name: '保存并关闭', exact: true }).click()
  await expect(shell).toBeHidden()
  expect(api.mutations).toHaveLength(1)
  const config = api.mutations[0].body.config
  expect(config.model_id).toBe('local-chat-model-2')
  expect(config.rerank_model_id).toBe(testAgent.config.rerank_model_id)
  expect(config.allowed_tools).toEqual(testAgent.config.allowed_tools)
  expect(config.mcp_services).toEqual(testAgent.config.mcp_services)
  expect(config.selected_skills).toEqual(testAgent.config.selected_skills)
  expect(config.system_prompt).toBe(testAgent.config.system_prompt)
  await page.goto('/platform/agents?edit=rc24-isolated-agent')
  await expect(shell.locator('[data-guide="agent-editor-nav-model"]')).toBeVisible()
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await expect(shell.locator('.section:visible textarea')).toHaveValue('Updated isolated description')
  await shell.locator('[data-guide="agent-editor-nav-model"]').click()
  await expect(shell.locator('.section:visible')).toContainText('Second Chat')
  expect(api.mutations).toHaveLength(1)
})

test('create, save and reopen an isolated new agent', async ({ page }) => {
  const api = await fixtureApi(page)
  await page.goto('/platform/agents')
  await page.locator('[data-guide="agent-list-create"]').first().click()
  const shell = page.locator('.settings-modal-shell').last()
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await shell.getByText('智能推理', { exact: true }).click()
  await shell.locator('.name-input input').fill('RC24 New isolated agent')
  await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
  await shell.locator('.section:visible textarea').first().fill('Isolated new system prompt')
  await shell.getByRole('button', { name: '创建智能体', exact: true }).click()
  await expect.poll(() => api.mutations.length).toBe(1)
  expect(api.mutations[0].method).toBe('POST')
  expect(api.saved().config.system_prompt).toBe('Isolated new system prompt')
  await shell.locator('.close-btn').click()
  await page.goto('/platform/agents?edit=rc24-isolated-agent')
  await expect(shell.locator('.editor-initializing')).toBeHidden()
  await expect(shell.locator('.name-input input')).toHaveValue('RC24 New isolated agent')
  await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
  await expect(shell.locator('.section:visible textarea').first()).toHaveValue('Isolated new system prompt')
  expect(api.mutations).toHaveLength(1)
})
