import { test, expect } from '@playwright/test'
import { fixtureApi, testAgent } from './fixtures'

for (const [name, multi, rewrite, kb, empty, scenario] of [
  ['four', false, false, true, false, 'normal'],
  ['six', true, true, true, false, 'normal'],
  ['six empty prompts', true, true, true, true, 'empty'],
  ['six API failure', true, true, true, false, 'failure'],
  ['multi off rewrite on', false, true, true, false, 'normal'],
  ['multi on rewrite off', true, false, true, false, 'normal'],
  ['no KB three', false, false, false, true, 'normal'],
  ['no KB five', true, true, false, false, 'normal'],
] as const) {
  test(`prompt navigation: ${name}`, async ({ page }, info) => {
    const api = await fixtureApi(page, scenario)
    api.seedAgent({ ...testAgent, config: { ...testAgent.config, agent_mode: 'quick-answer', allowed_tools: [], kb_selection_mode: kb ? 'selected' : 'none', knowledge_bases: kb ? ['local-kb'] : [], system_prompt: empty ? '' : 'Existing system', context_template: empty ? '' : 'Existing context', multi_turn_enabled: multi, enable_rewrite: rewrite, rewrite_prompt_system: empty ? '' : 'Existing rewrite system', rewrite_prompt_user: empty ? '' : 'Existing rewrite user {{query}}', fallback_strategy: 'model', fallback_prompt: empty ? '' : 'Existing fallback {{query}}' } })
    try {
      await page.goto('/platform/agents?edit=rc24-isolated-agent')
      const shell = page.locator('.settings-modal-shell').last()
      await expect(shell.locator('[data-guide="agent-editor-nav-prompts"]')).toBeVisible({ timeout: 60000 })
      await expect(shell.locator('.editor-initializing')).toBeHidden({ timeout: 60000 })
      await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
      const navigation = shell.locator('.prompts-outline__pill')
      await expect(navigation).toHaveCount(3 + (kb ? 1 : 0) + (multi && rewrite ? 2 : 0))
      const values: Record<string, string> = { '系统提示词': empty ? '' : 'Existing system', '上下文模板': empty ? '' : 'Existing context', '改写 · 系统': empty ? '' : 'Existing rewrite system', '改写 · 用户': empty ? '' : 'Existing rewrite user {{query}}', '检索兜底': empty ? '' : 'Existing fallback {{query}}' }
      const labels = (await navigation.allTextContents()).map(label => label.trim())
      for (const label of labels) {
        await navigation.getByText(label, { exact: true }).click()
        const pane = shell.locator('.prompts-panel__pane:visible')
        await expect(pane).toHaveCount(1)
        await expect(pane).toBeVisible()
        if (label === '意图提示词') await expect(pane).toContainText('意图')
        else {
          await expect(pane.locator('textarea').first()).toHaveValue(values[label])
          await pane.locator('textarea').first().fill(`Unsaved ${label} {{query}}`)
        }
      }
      for (const label of labels.filter(label => label !== '意图提示词')) {
        await navigation.getByText(label, { exact: true }).click()
        await expect(shell.locator('.prompts-panel__pane:visible textarea').first()).toHaveValue(`Unsaved ${label} {{query}}`)
      }
      expect(api.mutations).toEqual([])
      expect(api.pageErrors).toEqual([])
      expect(api.consoleErrors.filter(e => /TypeError|Unhandled|panel failed/.test(e))).toEqual([])
    } finally {
      await info.attach('console-network', { body: JSON.stringify({ responses: api.responses, consoleErrors: api.consoleErrors, pageErrors: api.pageErrors, mutations: api.mutations }), contentType: 'application/json' })
      await info.attach('DOM', { body: await page.content(), contentType: 'text/html' })
      await page.screenshot({ path: info.outputPath('navigation.png'), animations: 'allow' }).catch(async error => info.attach('screenshot-error', { body: String(error) }))
    }
  })
}

test('prompt navigation: switching smart and quick modes retains unsaved rewrite fields', async ({ page }, info) => {
  const api = await fixtureApi(page)
  api.seedAgent({ ...testAgent, config: { ...testAgent.config, agent_mode: 'quick-answer', allowed_tools: [], kb_selection_mode: 'selected', knowledge_bases: ['local-kb'], system_prompt: 'Existing system', context_template: 'Existing context', multi_turn_enabled: true, enable_rewrite: true, rewrite_prompt_system: 'Existing rewrite system', rewrite_prompt_user: 'Existing rewrite user {{query}}' } })
  try {
    await page.goto('/platform/agents?edit=rc24-isolated-agent')
    const shell = page.locator('.settings-modal-shell').last()
    await expect(shell.locator('[data-guide="agent-editor-nav-prompts"]')).toBeVisible({ timeout: 60000 })
    await expect(shell.locator('.editor-initializing')).toBeHidden({ timeout: 60000 })
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    for (const label of ['改写 · 系统', '改写 · 用户']) {
      await shell.locator('.prompts-outline__pill').getByText(label, { exact: true }).click()
      await shell.locator('.prompts-panel__pane:visible textarea').fill(`Unsaved ${label} {{query}}`)
    }
    await shell.locator('[data-guide="agent-editor-nav-basic"]').click()
    await shell.locator('[data-guide="agent-create-mode"]').getByText('智能推理', { exact: true }).click()
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(shell.locator('.prompts-outline__pill')).toHaveCount(0)
    await expect(shell.locator('.prompts-panel__pane:visible')).toHaveCount(1)
    await expect(shell.locator('.prompts-panel__pane:visible textarea')).toHaveValue('Existing system')
    await shell.locator('[data-guide="agent-editor-nav-basic"]').click()
    await shell.locator('[data-guide="agent-create-mode"]').getByText('快速问答', { exact: true }).click()
    await shell.locator('[data-guide="agent-editor-nav-prompts"]').click()
    await expect(shell.locator('.prompts-outline__pill')).toHaveCount(6)
    for (const label of ['改写 · 系统', '改写 · 用户']) {
      await shell.locator('.prompts-outline__pill').getByText(label, { exact: true }).click()
      await expect(shell.locator('.prompts-panel__pane:visible textarea')).toHaveValue(`Unsaved ${label} {{query}}`)
    }
    expect(api.mutations).toEqual([])
    expect(api.pageErrors).toEqual([])
    expect(api.consoleErrors.filter(e => /TypeError|Unhandled|panel failed/.test(e))).toEqual([])
  } finally {
    await info.attach('console-network', { body: JSON.stringify({ responses: api.responses, consoleErrors: api.consoleErrors, pageErrors: api.pageErrors, mutations: api.mutations }), contentType: 'application/json' })
    await info.attach('DOM', { body: await page.content(), contentType: 'text/html' })
    await page.screenshot({ path: info.outputPath('navigation.png'), animations: 'allow' })
  }
})
