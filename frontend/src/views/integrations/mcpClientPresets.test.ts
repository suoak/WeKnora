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
      assert.equal(config.inputs, undefined)
      assert.equal(server.headers.Authorization, 'Bearer secret-"value')
      assert.equal(JSON.stringify(config).includes('${input:'), false)
    } else assert.equal(server.headers.Authorization, 'Bearer secret-"value')
    assert.equal(server.type, client.value === 'pi' ? undefined : client.value === 'workbuddy' ? 'streamableHttp' : 'http')
  }
})

test('all personal client copies embed the supplied Secret in the appropriate user configuration', () => {
  for (const client of mcpClients) {
    const config = personalClientConfiguration(client.value, url, 'do-not-commit-this-secret')
    assert.equal(config.includes('do-not-commit-this-secret'), true)
    assert.doesNotMatch(config, /\$\{input:|\$\{KNOWHUB_MCP_KEY\}|bearer_token_env_var/)
  }
  assert.match(mcpClients[0].location, /用户级/)
})
test('Codex complete user configuration uses supported static HTTP headers', () => {
  const config = personalClientConfiguration('codex', url, 'must-not-be-stored')
  assert.match(config, /http_headers = \{ Authorization = "Bearer must-not-be-stored" \}/)
  assert.doesNotMatch(config, /bearer_token_env_var|X-Tenant-ID/)
})

test('VS Code and Copilot complete user configurations contain the supplied Key directly', () => {
  for (const client of ['vscode', 'copilot'] as const) {
    for (const secret of ['key-A', 'key-B']) {
      const config = JSON.parse(personalClientConfiguration(client, url, secret))
      assert.equal(config.servers.knowhub.headers.Authorization, `Bearer ${secret}`)
      assert.equal(config.inputs, undefined)
    }
    assert.match(mcpClients.find(item => item.value === client)!.location, /用户级/)
  }
})
test('only the seven approved clients are recommended and Pi is pinned', () => {
  assert.deepEqual(mcpClients.map(client => client.value), ['claude', 'codex', 'copilot', 'vscode', 'workbuddy', 'pi', 'generic'])
  const pi = mcpClients.find(client => client.value === 'pi')!
  assert.match(pi.steps.join(' '), /@earendil-works\/pi-coding-agent@1.4.0/)
  assert.match(pi.location, /~\/\.pi\/agent\/mcp.json/)
  assert.match(pi.steps.join(' '), /pi mcp list/)
})
