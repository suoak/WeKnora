import { test, expect, type Page } from '@playwright/test'
import { fixtureApi } from './fixtures'

async function scopeFixture(page: Page, role: 'owner' | 'viewer' = 'owner') {
  const api = await fixtureApi(page)
  if (role === 'viewer') await page.addInitScript(() => localStorage.setItem('weknora_memberships', JSON.stringify([{ tenant_id: 1, role: 'viewer' }])))
  const managementRequests: Array<{ method: string; path: string }> = []
  let listReads = 0, deniedStatus = 403
  let mode: 'normal' | 'empty' | 'failed' | 'revoked' = 'normal', saveStatus = 200
  let endpoint = { id: 'scope-qa', name: 'Scope QA', tenant_id: 1, enabled: true, description: '', token_hint: '',
    knowledge_base_ids: ['own', 'shared'], tools: ['search_knowledge'], default_agent_id: '',
    rate_limit_per_minute: 60, path: '/mcp/scope-qa' }
  const puts: any[] = []
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (role === 'viewer' && path === '/api/v1/auth/me') return route.fulfill({ json: { success: true, data: {
      user: { id: 'local-agent-test-user', username: 'Isolated viewer', tenant_id: '1' },
      tenant: { id: '1', name: 'Isolated space' }, memberships: [{ tenant_id: 1, role: 'viewer' }],
    } } })
    if (path.startsWith('/api/v1/mcp-endpoints') && request.method() !== 'GET') {
      managementRequests.push({ method: request.method(), path })
      if (role === 'viewer') return route.fulfill({ status: deniedStatus, json: { success: false, message: 'Isolated management denied' } })
    }
    let data: unknown
    if (path === '/api/v1/knowledge-bases') data = mode === 'empty' ? [] : [{ id: 'own', name: 'Own QA' }]
    else if (path === '/api/v1/shared-knowledge-bases') {
      if (mode === 'failed') return route.fulfill({ status: 403, json: { success: false, message: 'Isolated denied' } })
      data = mode === 'normal' ? ['Team QA', 'Second Team QA'].map(org_name => ({
        knowledge_base: { id: 'shared', name: 'Shared QA' }, org_name, source_tenant_id: 2, permission: 'viewer',
      })) : []
    } else if (path === '/api/v1/mcp-endpoints/tools') data = { groups: ['retrieve'],
      tools: [{ name: 'search_knowledge', group: 'retrieve', destructive: false }], default_tools: ['search_knowledge'] }
    else if (path === '/api/v1/mcp-endpoints') { listReads++; data = [endpoint] }
    else if (path === '/api/v1/mcp-endpoints/scope-qa' && request.method() === 'PUT') {
      puts.push(request.postDataJSON())
      if (saveStatus !== 200) return route.fulfill({ status: saveStatus, json: { success: false, message: saveStatus === 409 ? 'Isolated scope conflict: reload before retry' : 'Isolated save failure' } })
      endpoint = { ...endpoint, ...request.postDataJSON() }
      data = endpoint
    } else return route.fallback()
    return route.fulfill({ json: { success: true, data } })
  })
  await page.goto('/platform/settings?section=integration-mcpserver')
  const card = page.locator('.mcp-server-panel .channel-card').filter({ hasText: 'Scope QA' })
  await expect(card).toBeVisible()
  const open = async () => { await card.click(); await expect(page.locator('.mcp-endpoint-drawer .t-select').first()).toBeVisible() }
  return { api, puts, open, managementRequests, reads: () => listReads, stored: () => structuredClone(endpoint),
    deny: (status: number) => deniedStatus = status,
    mode: (value: typeof mode) => mode = value, fail: (status: number) => saveStatus = status }
}
test('MCP component: shared origins dedup, failed save preserves selection, retry closes and reopens', async ({ page }) => {
  const f = await scopeFixture(page); await f.open()
  const drawer = page.locator('.mcp-endpoint-drawer:visible').last()
  await drawer.locator('.t-select').first().click()
  const shared = page.locator('.t-select-option').filter({ hasText: 'Shared QA' })
  await expect(shared).toHaveCount(1)
  await expect(shared).toContainText('来源空间 #2')
  await expect(shared).toContainText('Team QA, Second Team QA')
  await drawer.locator('input').first().click()
  await drawer.locator('input').first().fill('Scope QA edited')
  f.fail(500)
  await drawer.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => f.puts.length).toBe(1)
  await expect(drawer).toBeVisible()
  await expect(drawer.locator('input').first()).toHaveValue('Scope QA edited')
  expect(f.puts[0].knowledge_base_ids).toEqual(['own', 'shared'])
  f.fail(200)
  await drawer.getByRole('button', { name: '保存', exact: true }).click()
  await expect(drawer).toBeHidden()
  expect(f.puts.length).toBe(2)
  await page.locator('.mcp-server-panel .channel-card').filter({ hasText: 'Scope QA edited' }).click()
  const reopened = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(reopened.locator('input').first()).toHaveValue('Scope QA edited')
  expect(f.api.pageErrors).toEqual([])
})
test('MCP component: refresh failure removes shared snapshot, disables save, retry observes revocation', async ({ page }) => {
  const f = await scopeFixture(page); await f.open()
  const drawer = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  await drawer.getByRole('button', { name: '取消', exact: true }).click()
  await expect(drawer).toBeHidden()
  f.mode('failed'); await f.open()
  const next = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(next.getByRole('alert')).toContainText('知识库授权列表加载失败')
  await expect(next.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  f.mode('revoked'); await next.getByRole('button', { name: '重试', exact: true }).click()
  await expect(next.getByRole('alert')).toContainText('部分已选知识库已不可用')
  await expect(next.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(f.puts).toEqual([])
  expect(f.api.pageErrors).toEqual([])
})
test('MCP component: empty scope is explicit and does not silently delete stored IDs', async ({ page }) => {
  const f = await scopeFixture(page); f.mode('empty'); await f.open()
  const drawer = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(drawer.getByText('暂无可用知识库。', { exact: true })).toBeVisible()
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(f.puts).toEqual([])
  expect(f.api.pageErrors).toEqual([])
})

test('MCP component: CAS 409 preserves the scope draft; explicit reload and retry persist it', async ({ page }) => {
  const f = await scopeFixture(page); await f.open()
  const editDraft = async () => {
    const drawer = page.locator('.mcp-endpoint-drawer:visible').last()
    await drawer.locator('input').first().fill('Scope QA CAS draft')
    await drawer.locator('.t-select').first().click()
    await page.locator('.t-select-option').filter({ hasText: 'Shared QA' }).click()
    await drawer.locator('input').first().click()
    return drawer
  }
  const drawer = await editDraft()
  f.fail(409)
  await drawer.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('Isolated scope conflict: reload before retry', { exact: true })).toBeVisible()
  await expect(drawer).toBeVisible()
  await expect(drawer.locator('input').first()).toHaveValue('Scope QA CAS draft')
  await expect(drawer.locator('.t-select').first()).not.toContainText('Shared QA')
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  expect(f.puts).toHaveLength(1)
  expect(f.puts[0].knowledge_base_ids).toEqual(['own'])
  // The user explicitly discards/reloads the snapshot through the existing UI.
  await drawer.getByRole('button', { name: '取消', exact: true }).click()
  await expect(drawer).toBeHidden()
  // Explicit browser refresh reloads the current endpoint. Both the prior
  // cancel transition and the later successful save transition are asserted.
  await page.reload()
  await expect.poll(f.reads).toBeGreaterThan(1)
  await f.open()
  expect(f.puts).toHaveLength(1)
  const retry = await editDraft()
  f.fail(200)
  await retry.getByRole('button', { name: '保存', exact: true }).click()
  await expect(retry).toBeHidden()
  expect(f.puts).toHaveLength(2)
  expect(f.stored().knowledge_base_ids).toEqual(['own'])
  await page.locator('.mcp-server-panel .channel-card').filter({ hasText: 'Scope QA CAS draft' }).click()
  const reopened = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(reopened.locator('input').first()).toHaveValue('Scope QA CAS draft')
  await expect(reopened.locator('.t-select').first()).not.toContainText('Shared QA')
  expect(f.api.pageErrors).toEqual([])
})

test('MCP component: administrator management actions remain available', async ({ page }) => {
  const f = await scopeFixture(page)
  await expect(page.locator('.mcp-server-panel .channel-card--add')).toBeVisible()
  await expect(page.locator('.mcp-server-panel .channel-card__actions').first()).toBeVisible()
  await f.open()
  await expect(page.locator('.mcp-endpoint-drawer:visible').getByRole('button', { name: '保存', exact: true })).toBeEnabled()
  expect(f.managementRequests).toEqual([])
})

test('MCP component: viewer has no management controls; fixture denials never become success', async ({ page }) => {
  const f = await scopeFixture(page, 'viewer')
  await expect(page.locator('.mcp-server-panel .channel-card--add')).toHaveCount(0)
  await expect(page.locator('.mcp-server-panel .channel-card__action-btn')).toHaveCount(0)
  await f.open()
  const drawer = page.locator('.mcp-endpoint-drawer:visible').last()
  await expect(drawer.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  await expect(drawer.locator('.t-select').first().locator('input').first()).toBeDisabled()
  expect(f.managementRequests).toEqual([])
  // These synthetic responses characterize UI/transport only. The Go HTTP
  // test independently exercises production JWT auth, RBAC, handler and SQL.
  for (const status of [403, 401]) {
    f.deny(status)
    for (const [method, path] of [['POST', '/api/v1/mcp-endpoints'], ['PUT', '/api/v1/mcp-endpoints/scope-qa'],
      ['POST', '/api/v1/mcp-endpoints/scope-qa/rotate-token'], ['DELETE', '/api/v1/mcp-endpoints/scope-qa']]) {
      expect(await page.evaluate(async ({ method, path }) => (await fetch(path, {
        method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'denied' }),
      })).status, { method, path })).toBe(status)
    }
  }
  expect(f.managementRequests).toHaveLength(8)
  expect(f.stored().name).toBe('Scope QA')
  expect(f.api.pageErrors).toEqual([])
})
