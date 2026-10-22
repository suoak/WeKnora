import test from 'node:test'
import assert from 'node:assert/strict'
import { personalCodexConfig, personalMcpConfig, personalMcpURL, testPersonalMcpConnection } from './personalMcpConfig.ts'

test('personal config has one address and never invents target memberships', () => {
  const config = JSON.parse(personalMcpConfig('https://knowhub.example.invalid/mcp/team?old=true', 'synthetic-token'))
  assert.deepEqual(Object.keys(config.mcpServers), ['knowhub'])
  assert.equal(config.mcpServers.knowhub.url, 'https://knowhub.example.invalid/api/v1/mcp/personal')
  assert.deepEqual(config.mcpServers.knowhub.headers, { Authorization: 'Bearer synthetic-token' })
  assert.throws(() => personalMcpURL('https://user:password@example.invalid/mcp'))
})

test('Codex template uses the tested personal URL and an environment credential', () => {
  const config = personalCodexConfig('https://knowhub.example.invalid/old/team?tenant=99')
  assert.equal(config, '[mcp_servers.knowhub]\nurl = "https://knowhub.example.invalid/api/v1/mcp/personal"\nbearer_token_env_var = "KNOWHUB_MCP_KEY"\n')
  assert.doesNotMatch(config, /X-Tenant-ID|tenant=|http_headers/)
})

test('connection check actually initializes and verifies the exact read-only tools', async () => {
  const methods: string[] = []
  const fetcher = (async (_url: string, init: RequestInit) => {
    const request = JSON.parse(init.body as string)
    methods.push(request.method)
    assert.equal((init.headers as Record<string, string>).Authorization, 'Bearer synthetic-token')
    assert.equal(init.credentials, 'omit')
    const result = request.method === 'initialize' ? { protocolVersion: '2025-03-26' }
      : { tools: ['search_knowledge', 'list_knowledge_bases', 'list_documents', 'read_document', 'grep_chunks', 'wiki_search', 'wiki_read_page', 'wiki_index'].map(name => ({ name })) }
    return new Response(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }), { headers: { 'Content-Type': 'application/json' } })
  }) as typeof fetch
  assert.equal((await testPersonalMcpConnection('https://example.invalid/mcp', 'synthetic-token', fetcher)).length, 8)
  assert.deepEqual(methods, ['initialize', 'tools/list'])
  await assert.rejects(testPersonalMcpConnection('https://example.invalid/mcp', 'synthetic-token', (async () => new Response('', { status: 401 })) as typeof fetch), /凭据/)
})
