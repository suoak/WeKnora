import type { Page } from '@playwright/test'

export const menuKeys = ['basic', 'prompts', 'suggestions', 'conversation', 'knowledge', 'websearch', 'tools', 'mcp', 'skills', 'multimodal', 'model']
export const testAgent = {
  id: 'rc24-isolated-agent', name: 'RC24 隔离验收智能体', description: 'Original description', tenant_id: 1, creator_id: 'local-agent-test-user',
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  config: {
    agent_mode: 'smart-reasoning', agent_type: 'custom', model_id: 'local-chat-model', rerank_model_id: 'local-rerank-model',
    system_prompt: '保留原有提示词', kb_selection_mode: 'none', allowed_tools: ['thinking', 'legacy_unknown_tool'],
    mcp_selection_mode: 'selected', mcp_services: ['local-mcp'], skills_selection_mode: 'selected', selected_skills: ['existing-unavailable-skill'],
    sandbox_config_id: '', temperature: 0.7, reasoning_effort: 'off',
  },
}
export const modelRows = [
  { id: 'local-chat-model', name: 'Local Chat', type: 'KnowledgeQA', source: 'remote', parameters: {}, status: 'active' },
  { id: 'local-chat-model-2', name: 'Second Chat', type: 'KnowledgeQA', source: 'remote', parameters: {}, status: 'active' },
  { id: 'local-rerank-model', name: 'Local Rerank', type: 'Rerank', source: 'remote', parameters: {}, status: 'active' },
]
export async function fixtureApi(page: Page, scenario: 'normal' | 'empty' | 'failure' | 'forbidden' | 'unavailable' | 'stopped' = 'normal') {
  let currentScenario = scenario
  let savedAgent = structuredClone(testAgent)
  let saveStatus = 200
  const mutations: Array<{ method: string; path: string; body: any }> = []
  const consoleErrors: string[] = [], pageErrors: string[] = [], responses: Array<{ url: string; status: number }> = []
  const user = { id: testAgent.creator_id, username: '本地验收', tenant_id: '1', preferences: { language: 'zh-CN' } }
  const tenant = { id: '1', name: '隔离测试空间', owner_id: user.id }
  await page.addInitScript(({ user, tenant }) => {
    window.addEventListener('unhandledrejection', event => console.error('Unhandled rejection details', JSON.stringify(event.reason)))
    for (const [key, value] of Object.entries({ locale: 'zh-CN', weknora_token: 'local-fixture-token', weknora_user: JSON.stringify(user), weknora_tenant: JSON.stringify(tenant), weknora_memberships: JSON.stringify([{ tenant_id: 1, role: 'owner' }]), 'weknora:new-user-guide-done:v1': '1', 'weknora:contextual-guide-agent-list:v1': '1', 'weknora:contextual-guide-agent-create:v1': '1' })) localStorage.setItem(key, value)
  }, { user, tenant })
  page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })
  page.on('pageerror', error => pageErrors.push(error.message))
  page.on('response', response => responses.push({ url: response.url(), status: response.status() }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (request.method() !== 'GET') {
      if (!['POST', 'PUT'].includes(request.method()) || !/^\/api\/v1\/agents(?:\/rc24-isolated-agent)?$/.test(path)) throw new Error(`Unexpected mutation: ${request.method()} ${path}`)
      const body = request.postDataJSON()
      mutations.push({ method: request.method(), path, body })
      if (saveStatus !== 200) return route.fulfill({ status: saveStatus, json: { success: false, message: 'Isolated save failure' } })
      savedAgent = { ...savedAgent, ...body, id: testAgent.id }
      return route.fulfill({ json: { success: true, data: savedAgent } })
    }
    const dependency = ['/models', '/knowledge-bases', '/web-search-providers', '/sandbox-configs', '/mcp-services', '/agents/type-presets', '/tenants/kv/prompt-templates', '/tenants/kv/storage-engine-config', '/system/storage-engine-status', '/agents/placeholders', '/tenants/kv/retrieval-config', '/skills/catalog', '/system/parser-engines'].some(p => path === `/api/v1${p}`)
    if (dependency && ['failure', 'forbidden'].includes(currentScenario)) {
      const status = currentScenario === 'forbidden' ? 403 : 500
      return route.fulfill({ status, json: { success: false, message: 'Isolated test failure' } })
    }
    let data: any = []
    if (path.endsWith('/auth/me')) data = { user, tenant, memberships: [{ tenant_id: 1, role: 'owner' }] }
    else if (path === '/api/v1/agents') data = [savedAgent]
    else if (path === '/api/v1/organizations') data = { organizations: [] }
    else if (path.endsWith('/capabilities')) data = { edition: 'standard', capabilities: {} }
    else if (path.endsWith('/prompt-templates')) data = { system_prompt: [], agent_system_prompt: [], context_template: [], rewrite: [], fallback: [], intent_prompts: [] }
    else if (path.endsWith('/placeholders')) data = { all: [], system_prompt: [], agent_system_prompt: [], context_template: [], rewrite_system_prompt: [], rewrite_prompt: [], fallback_prompt: [] }
    else if (path.endsWith('/retrieval-config')) data = {}
    else if (path.endsWith('/storage-engine-status')) data = { engines: [], allowed_providers: [] }
    else if (path.endsWith('/storage-engine-config')) data = {}
    else if (path === '/api/v1/models') data = currentScenario === 'empty' || currentScenario === 'unavailable' ? []
      : currentScenario === 'stopped' ? modelRows.map(row => ({ ...row, status: row.id === 'local-chat-model' ? 'download_failed' : row.status })) : modelRows
    else if (path === '/api/v1/mcp-services') data = currentScenario === 'empty' ? [] : [{ id: 'local-mcp', name: 'Local MCP', enabled: true }]
    else if (path === '/api/v1/knowledge-bases') data = currentScenario === 'empty' ? [] : [{ id: 'local-kb', name: 'Test KB', type: 'document', capabilities: { vector: true, keyword: true, wiki: false } }]
    return route.fulfill({ json: { success: true, data, total: 0 } })
  })
  return { mutations, consoleErrors, pageErrors, responses, recover: () => currentScenario = 'normal', setScenario: (value: typeof scenario) => currentScenario = value, setSaveStatus: (value: number) => { saveStatus = value }, saved: () => savedAgent, seedAgent: (value: any) => { savedAgent = structuredClone(value) } }
}
