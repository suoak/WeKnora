// One personal address covers dynamically published public KBs plus independent
// private entitlements. It never manufactures a target workspace membership.
export function personalMcpURL(publicURL: string): string {
  if (!publicURL) return ''
  const url = new URL(publicURL)
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) throw new Error('MCP 地址配置无效')
  url.pathname = '/api/v1/mcp/personal'
  url.search = ''
  url.hash = ''
  return url.toString()
}

export type PersonalMcpEnvironment = 'intranet' | 'isolated'
export const ISOLATED_PERSONAL_MCP_URL = 'https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal'

export function personalMcpEnvironmentURL(publicURL: string, environment: PersonalMcpEnvironment): string {
  return environment === 'isolated' ? ISOLATED_PERSONAL_MCP_URL : personalMcpURL(publicURL)
}

// Browser origin policy is independent of MCP reachability from an AI client.
// Do not retrieve or send a Secret for a browser check that CORS would block.
export function canCheckPersonalMcpInBrowser(url: string, pageURL: string): boolean {
  return new URL(url).origin === new URL(pageURL).origin
}

export function personalMcpConfig(publicURL: string, token: string) {
  return JSON.stringify({ mcpServers: { knowhub: {
    type: 'http', url: personalMcpURL(publicURL), headers: { Authorization: `Bearer ${token}` },
  } } }, null, 2)
}

// This is the bearer_token_env_var contract exercised by the real Codex CLI
// acceptance fixture. Keep the Secret out of the reusable TOML template.
export function personalCodexConfig(publicURL: string): string {
  return `[mcp_servers.knowhub]\nurl = ${JSON.stringify(personalMcpURL(publicURL))}\nbearer_token_env_var = "KNOWHUB_MCP_KEY"\n`
}

// Browser-to-MCP protocol test, independent of an agent/LLM. No localStorage,
// query-string credential, generic proxy or external model is involved.
export async function testPersonalMcpConnection(url: string, token: string, fetcher = fetch, environment: PersonalMcpEnvironment = 'intranet'): Promise<string[]> {
  const call = async (method: string, params: object, id: number) => {
    const response = await fetcher(url, { method: 'POST', credentials: 'omit', cache: 'no-store',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': '2025-03-26' },
      body: JSON.stringify({ jsonrpc: '2.0', id, method, params }), signal: AbortSignal.timeout(10000) })
    if (response.status === 401) throw new Error('凭据已过期、撤销或身份无效')
    if (response.status === 403) throw new Error('个人 MCP 尚未启用，或企业身份/授权校验未通过')
    if (!response.ok) throw new Error(`MCP 连接失败（${response.status}）`)
    const text = await response.text()
    const rpc = response.headers.get('content-type')?.includes('text/event-stream')
      ? JSON.parse(text.split('\n').find(line => line.startsWith('data:'))?.slice(5) || '{}') : JSON.parse(text)
    if (rpc.error || rpc.id !== id || !rpc.result) throw new Error('MCP 协议响应无效')
    return rpc.result
  }
  await call('initialize', { protocolVersion: '2025-03-26', capabilities: {}, clientInfo: { name: 'knowhub-connection-check', version: '1' } }, 1)
  const result = await call('tools/list', {}, 2)
  const names = (result.tools || []).map((tool: { name: string }) => tool.name).sort()
  const expected = ['grep_chunks', 'list_documents', 'list_knowledge_bases', 'read_document', 'search_knowledge', 'wiki_index', 'wiki_read_page', 'wiki_search']
  if (environment === 'intranet') expected.push('list_writable_knowledge_bases', 'add_document', 'upload_document', 'get_document_processing_status')
  if (JSON.stringify(names) !== JSON.stringify(expected.sort())) throw new Error(environment === 'isolated' ? '只读工具范围与预期不一致' : '云外知识查询和新增工具范围与预期不一致')
  return names
}
