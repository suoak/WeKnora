import { test, expect } from '@playwright/test'
import { fixtureApi } from '../agent-editor/fixtures'

test('reopen encrypted personal Secret, verify protocol, clear state and create with existing scope contract', async ({ page }) => {
  const api = await fixtureApi(page)
  const mutations: { path: string; body: any }[] = []
  const key = { id: 1, name: '可复用个人 Key', scope_type: 'user_mcp', client_type: 'generic', status: 'active',
    secret_available: true, personal_scope_mode: 'dynamic_all', token_hint: '…fixture', capabilities: ['retrieve', 'enterprise_retrieve'],
    tenant_scopes: [{ tenant_id: 1, kb_scope_mode: 'all', knowledge_bases: [] }], created_at: '', updated_at: '' }
  const options = [{ tenant_id: 1, tenant_name: '研发空间', role: 'owner', owned_knowledge_bases: [{ id: 'kb1', name: '私人知识' }], shared_knowledge_bases: [] }]
  await page.route('**/api/v1/mcp-api-keys**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (request.method() !== 'GET') mutations.push({ path, body: request.postDataJSON() })
    const data = path.endsWith('/reveal') ? { token: 'synthetic-reopened-secret' }
      : path.endsWith('/scope-options') ? options : request.method() === 'POST' ? { ...key, id: 2, token: 'synthetic-created-secret' } : [key]
    await route.fulfill({ json: { success: true, data, mcp_public_url: 'http://127.0.0.1:4219/api/v1/mcp/personal' } })
  })
  const rpcMethods: string[] = []
  await page.route('**/api/v1/mcp/personal', async route => {
    const rpc = route.request().postDataJSON()
    rpcMethods.push(rpc.method)
    expect(route.request().headers().authorization).toBe('Bearer synthetic-reopened-secret')
    const result = rpc.method === 'initialize' ? { protocolVersion: '2025-03-26', capabilities: { tools: {} }, serverInfo: { name: 'synthetic', version: '1' } }
      : { tools: ['grep_chunks', 'list_documents', 'list_knowledge_bases', 'read_document', 'search_knowledge', 'wiki_index', 'wiki_read_page', 'wiki_search'].map(name => ({ name })) }
    await route.fulfill({ json: { jsonrpc: '2.0', id: rpc.id, result } })
  })
  await page.goto('/platform/mcp-access')
  await page.getByRole('button', { name: 'Pi', exact: true }).click()
  const dialog = page.locator('.t-dialog:visible')
  await expect(dialog.locator('pre')).toContainText('Bearer synthetic-reopened-secret')
  await dialog.getByRole('button', { name: '验证 MCP 协议', exact: true }).click()
  await expect(dialog).toContainText('MCP 协议通过：')
  await expect(dialog).toContainText('尚未验证客户端会话及知识检索')
  expect(rpcMethods).toEqual(['initialize', 'tools/list'])
  expect(mutations.map(item => item.path)).toEqual(['/api/v1/mcp-api-keys/1/reveal'])
  await dialog.locator('.t-dialog__close').click()
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('synthetic-reopened-secret')
  await page.getByRole('button', { name: 'Pi', exact: true }).click()
  await expect(dialog.locator('pre')).toContainText('Bearer synthetic-reopened-secret')
  await expect(dialog.getByText('MCP 协议通过：', { exact: false })).toHaveCount(0)
  await dialog.locator('.t-dialog__close').click()
  await page.getByRole('button', { name: '创建个人 Key', exact: true }).click()
  const create = page.locator('.t-dialog:visible')
  await create.locator('input').first().fill('新的个人 Key')
  await create.getByRole('button', { name: '创建个人 Key', exact: true }).click()
  const payload = mutations.find(item => item.path === '/api/v1/mcp-api-keys')!.body
  expect(payload).toMatchObject({ name: '新的个人 Key', client_type: 'generic', personal_scope_mode: 'dynamic_all', capabilities: ['retrieve', 'enterprise_retrieve'], never_expires: true })
  expect(payload.tenant_scopes).toEqual([{ tenant_id: 1, kb_scope_mode: 'all', knowledge_bases: [] }])
  await expect(page.locator('.t-dialog:visible pre')).toContainText('synthetic-created-secret')
  expect(api.pageErrors).toEqual([])
})
