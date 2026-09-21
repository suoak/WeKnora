import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { buildMCPServers, claudeCommands, serverName, stringifyMCPConfig, verifiedConfigPreset } from './mcpConfig.ts'

const accessKeysSource = readFileSync(new URL('./MCPAccessKeys.vue', import.meta.url), 'utf8')

test('builds isolated entries for duplicate and unicode workspace names', () => {
  const spaces = [
    { tenantId: 1, tenantName: '研发 空间' },
    { tenantId: 2, tenantName: '研发 空间' },
  ]
  const config = buildMCPServers('https://x/mcp', 'sk-secret', spaces)
  assert.equal(Object.keys(config.mcpServers).length, 2)
  const first = (config.mcpServers as any)[serverName(spaces[0])]
  const second = (config.mcpServers as any)[serverName(spaces[1])]
  assert.equal(first.headers['X-API-Key'], 'sk-secret')
  assert.equal(second.headers['X-API-Key'], 'sk-secret')
  assert.equal(first.headers['X-Tenant-ID'], '1')
  assert.equal(second.headers['X-Tenant-ID'], '2')
  assert.equal(first.headers.Authorization, undefined)
  assert.equal(second.headers.Authorization, undefined)
})

test('serializes quotes and non-ASCII names as valid JSON', () => {
  const json = stringifyMCPConfig('https://x.example/mcp?q="ok"', 'sk-"secret', [
    { tenantId: 7, tenantName: '产品“知识”库' },
  ])
  const parsed = JSON.parse(json)
  const connection = Object.values(parsed.mcpServers)[0] as any
  assert.equal(connection.headers['X-API-Key'], 'sk-"secret')
  assert.equal(connection.headers['X-Tenant-ID'], '7')
  assert.equal(connection.headers.Authorization, undefined)
  assert.equal(connection.url, 'https://x.example/mcp?q="ok"')
})

test('generates one Claude Code HTTP command per workspace', () => {
  const commands = claudeCommands('https://x/mcp', 'sk-secret', [
    { tenantId: 1, tenantName: '产品' },
    { tenantId: 2, tenantName: '研发' },
  ])
  assert.equal(commands.split('\n').length, 2)
  assert.match(commands, /--transport http/)
  assert.equal(commands.match(/X-API-Key: sk-secret/g)?.length, 2)
  assert.match(commands, /X-Tenant-ID: 1/)
  assert.match(commands, /X-Tenant-ID: 2/)
  assert.doesNotMatch(commands, /Authorization|Bearer/)
})

test('uses verified client transport names and downgrades unverified presets', () => {
  const spaces = [{ tenantId: 1, tenantName: '公共库' }]
  const workbuddy = buildMCPServers('https://x/mcp', 'secret', spaces, 'workbuddy')
  const cursor = buildMCPServers('https://x/mcp', 'secret', spaces, 'cursor')
  assert.equal((Object.values(workbuddy.mcpServers)[0] as any).type, 'streamableHttp')
  assert.equal((Object.values(cursor.mcpServers)[0] as any).type, 'http')
  assert.equal(verifiedConfigPreset('workbuddy'), false)
  assert.equal(verifiedConfigPreset('cursor'), false)
  assert.equal(verifiedConfigPreset('workmate'), false)
  assert.equal(verifiedConfigPreset('codebuddy'), false)
})

test('all user MCP HTTP client presets use API-key authentication', () => {
  const spaces = [{ tenantId: 9, tenantName: 'Space 9' }]
  for (const client of ['workbuddy', 'workmate', 'cursor', 'codebuddy', 'generic'] as const) {
    const config = buildMCPServers('https://x/mcp', 'shared-token', spaces, client)
    const connection = Object.values(config.mcpServers)[0]
    assert.equal(connection.headers['X-API-Key'], 'shared-token', client)
    assert.equal(connection.headers['X-Tenant-ID'], '9', client)
    assert.equal('Authorization' in connection.headers, false, client)
  }
})

test('copies generated configuration without revealing an existing secret', () => {
  assert.match(accessKeysSource, />复制配置模板</)
  assert.match(accessKeysSource, />复制全部配置</)
  assert.match(accessKeysSource, /YOUR_MCP_KEY/)
  assert.match(accessKeysSource, /完整 Secret 只显示这一次/)
  assert.doesNotMatch(accessKeysSource, /reveal/i)
})
