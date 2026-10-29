import test from 'node:test'
import assert from 'node:assert/strict'
import { personalClientConfiguration, mcpClients } from './mcpClientPresets.ts'

const url = 'https://knowhub.example/mcp/old?tenant=99'
test('personal clients use the unified URL and never manufacture tenant authorization', () => {
  for (const client of mcpClients.filter(client => client.value !== 'codex')) {
    const config = JSON.parse(personalClientConfiguration(client.value, url, 'secret-"value'))
    const server = config.servers?.knowhub || config.mcpServers.knowhub
    assert.equal(server.url, 'https://knowhub.example/api/v1/mcp/personal')
    assert.equal(server.headers['X-Tenant-ID'], undefined)
    assert.equal(server.headers['X-API-Key'], undefined)
    if (client.value === 'copilot' || client.value === 'vscode') {
      assert.equal(config.inputs[0].password, true)
      assert.equal(server.headers.Authorization, 'Bearer ${input:knowhub-key}')
      assert.equal(JSON.stringify(config).includes('secret-'), false)
    } else assert.equal(server.headers.Authorization, 'Bearer secret-"value')
    assert.equal(server.type, client.value === 'pi' ? undefined : client.value === 'workbuddy' ? 'streamableHttp' : 'http')
  }
})
test('Codex reuses the existing environment credential contract', () => {
  const config = personalClientConfiguration('codex', url, 'must-not-be-stored')
  assert.match(config, /bearer_token_env_var = "KNOWHUB_MCP_KEY"/)
  assert.doesNotMatch(config, /must-not-be-stored|X-Tenant-ID/)
})
test('only the seven approved clients are recommended and Pi is pinned', () => {
  assert.deepEqual(mcpClients.map(client => client.value), ['claude', 'codex', 'copilot', 'vscode', 'workbuddy', 'pi', 'generic'])
  const pi = mcpClients.find(client => client.value === 'pi')!
  assert.match(pi.steps.join(' '), /@earendil-works\/pi-coding-agent@1.4.0/)
  assert.match(pi.location, /~\/\.pi\/agent\/mcp.json/)
  assert.match(pi.steps.join(' '), /pi mcp list/)
})
