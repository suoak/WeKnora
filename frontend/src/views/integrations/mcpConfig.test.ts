import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { buildMCPServers, claudeCommands, serverName, stringifyMCPConfig } from './mcpConfig.ts'

const accessKeysSource = readFileSync(new URL('./MCPAccessKeys.vue', import.meta.url), 'utf8')

test('builds isolated entries for duplicate and unicode workspace names', () => {
  const spaces = [
    { tenantId: 1, tenantName: '研发 空间' },
    { tenantId: 2, tenantName: '研发 空间' },
  ]
  const config = buildMCPServers('https://x/mcp', 'sk-secret', spaces)
  assert.equal(Object.keys(config.mcpServers).length, 2)
  assert.equal((config.mcpServers as any)[serverName(spaces[1])].headers['X-Tenant-ID'], '2')
})

test('serializes quotes and non-ASCII names as valid JSON', () => {
  const json = stringifyMCPConfig('https://x.example/mcp?q="ok"', 'sk-"secret', [
    { tenantId: 7, tenantName: '产品“知识”库' },
  ])
  const parsed = JSON.parse(json)
  const connection = Object.values(parsed.mcpServers)[0] as any
  assert.equal(connection.headers.Authorization, 'Bearer sk-"secret')
  assert.equal(connection.url, 'https://x.example/mcp?q="ok"')
})

test('generates one Claude Code HTTP command per workspace', () => {
  const commands = claudeCommands('https://x/mcp', 'sk-secret', [
    { tenantId: 1, tenantName: '产品' },
    { tenantId: 2, tenantName: '研发' },
  ])
  assert.equal(commands.split('\n').length, 2)
  assert.match(commands, /--transport http/)
  assert.match(commands, /X-Tenant-ID: 2/)
})

test('keeps full JSON copy separate from Claude command copy', () => {
  assert.match(accessKeysSource, /copySensitive\(jsonConfig\)[^>]*>复制全部 JSON/)
  assert.match(accessKeysSource, /copySensitive\(commands\)[^>]*>复制 Claude 命令/)
  assert.doesNotMatch(accessKeysSource, /copySensitive\(activeText\)/)
})
