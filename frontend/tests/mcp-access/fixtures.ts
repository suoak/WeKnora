import { expect, type Page } from '@playwright/test'
import { fixtureApi } from '../agent-editor/fixtures'

export async function keyFixture(page: Page, noURL = false) {
  const api = await fixtureApi(page)
  const reveals: number[] = [], mutations: { path: string; body: any }[] = []
  const scope = [{ tenant_id: 1, kb_scope_mode: 'all', knowledge_bases: [] }]
  const rows: any[] = Array.from({ length: 8 }, (_, i) => ({
    id: i + 1, name: i === 2 ? '默认知识 Key' : i === 3 ? '指定知识 Key' : i === 4 ? '历史固定 Key' : `个人 Key ${i + 1}`,
    client_type: 'generic', scope_type: 'user_mcp', secret_available: true,
    status: i === 0 ? 'revoked' : 'active', expires_at: i === 1 ? '2020-01-01T00:00:00Z' : undefined,
    personal_scope_mode: i === 2 ? 'dynamic_all' : 'fixed', capabilities: i === 3 ? ['retrieve'] : ['retrieve', 'enterprise_retrieve'],
    tenant_scopes: i === 3 ? [{ tenant_id: 1, kb_scope_mode: 'selected', knowledge_bases: [{ knowledge_base_id: 'kb1', source_type: 'owned' }] }] : structuredClone(scope),
    created_at: '2026-10-01T00:00:00Z', updated_at: '',
  }))
  rows.push(...Array.from({ length: 19 }, (_, i) => ({
    id: 20 + i, name: `旧版凭证 ${i + 1}`, client_type: i === 0 ? 'workmate' : i === 1 ? 'cursor' : 'generic', scope_type: 'user_mcp',
    status: i === 18 ? 'revoked' : 'active', expires_at: i === 17 ? '2020-01-01T00:00:00Z' : undefined,
    capabilities: ['retrieve', 'chat'], tenant_scopes: structuredClone(scope), created_at: '', updated_at: '',
  })))
  const options = [{ tenant_id: 1, tenant_name: '研发空间', role: 'owner', owned_knowledge_bases: [{ id: 'kb1', name: '私人知识' }], shared_knowledge_bases: [] }]
  let pendingID: number | undefined, release: (() => void) | undefined
  await page.route('**/api/v1/mcp-api-keys**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    let data: any
    if (path.endsWith('/reveal')) {
      const id = Number(path.split('/').at(-2)); reveals.push(id)
      if (id === pendingID) await new Promise<void>(resolve => { release = resolve })
      data = { token: `synthetic-key-${id}` }
    } else if (path.endsWith('/scope-options')) data = options
    else if (request.method() === 'GET') data = rows.map(({ token, ...row }) => row)
    else {
      mutations.push({ path, body: request.postDataJSON() })
      data = { ...rows[2], id: 100, name: request.postDataJSON().name, token: 'synthetic-key-100' }
      rows.push(data)
    }
    await route.fulfill({ json: { success: true, data, mcp_public_url: noURL ? '' : new URL('/api/v1/mcp/personal', page.url()).href } })
  })
  return { ...api, rows, reveals, mutations, hold: (id: number) => { pendingID = id }, release: () => { pendingID = undefined; release?.() } }
}
export async function openKey(page: Page, id: number) {
  await page.locator(`.personal-key-card[data-key-id="${id}"]`).getByRole('button', { name: '连接 AI 工具', exact: true }).click()
  await expect(page.locator('.t-dialog:visible .connection-identity')).toHaveAttribute('data-key-id', String(id))
}
export async function chooseClient(page: Page, name: string) {
  await page.locator('.t-dialog:visible .client-picker').getByRole('button', { name, exact: true }).click()
  await expect(page.locator('.t-dialog:visible pre')).toBeVisible()
}
