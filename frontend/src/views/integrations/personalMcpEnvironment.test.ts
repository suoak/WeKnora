import assert from 'node:assert/strict'
import test from 'node:test'
import { canCheckPersonalMcpInBrowser, personalMcpEnvironmentURL, ISOLATED_PERSONAL_MCP_URL } from './personalMcpConfig'
import { personalClientConfiguration, mcpClients } from './mcpClientPresets'

test('all clients keep their configuration and credential when changing only the endpoint', () => {
  const internal = 'http://10.51.134.114/api/v1/mcp/personal'
  assert.equal(personalMcpEnvironmentURL(internal, 'intranet'), internal)
  assert.equal(personalMcpEnvironmentURL(internal, 'isolated'), ISOLATED_PERSONAL_MCP_URL)
  for (const client of mcpClients) {
    const original = personalClientConfiguration(client.value, internal, 'synthetic-environment-key')
    const isolated = personalClientConfiguration(client.value, personalMcpEnvironmentURL(internal, 'isolated'), 'synthetic-environment-key')
    assert.equal(isolated, original.replace(internal, ISOLATED_PERSONAL_MCP_URL))
  }
})

test('browser checks do not treat cross-origin or mixed-content restrictions as MCP failure', () => {
  assert.equal(canCheckPersonalMcpInBrowser(ISOLATED_PERSONAL_MCP_URL, 'http://knowhub.ruijie.com.cn/platform/mcp-access'), false)
  assert.equal(canCheckPersonalMcpInBrowser('http://knowhub.ruijie.com.cn/api/v1/mcp/personal', 'https://knowhub.ruijie.com.cn/'), false)
  assert.equal(canCheckPersonalMcpInBrowser('http://knowhub.ruijie.com.cn/api/v1/mcp/personal', 'http://knowhub.ruijie.com.cn/platform/mcp-access'), true)
})
