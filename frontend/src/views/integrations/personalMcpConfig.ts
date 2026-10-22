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
export async function testPersonalMcpConnection(url: string, token: string, fetcher = fetch): Promise<string[]> {
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
  if (JSON.stringify(names) !== JSON.stringify(['grep_chunks', 'list_documents', 'list_knowledge_bases', 'read_document', 'search_knowledge', 'wiki_index', 'wiki_read_page', 'wiki_search'])) throw new Error('只读工具范围与预期不一致')
  return names
}
