import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const view = readFileSync(new URL('./MCPAccessKeys.vue', import.meta.url), 'utf8')
const api = readFileSync(new URL('../../api/mcpAccessKeys.ts', import.meta.url), 'utf8')
const lifecycle = readFileSync(new URL('./mcpAccessLifecycle.ts', import.meta.url), 'utf8')

test('Access Center presents credentials as integrations rather than plaintext API keys', () => {
  assert.match(view, /我的接入配置/)
  assert.match(view, /WorkBuddy/)
  assert.match(view, /WorkMate/)
  assert.match(view, /Cursor/)
  assert.match(view, /CodeBuddy/)
  assert.match(view, /Generic MCP/)
  assert.match(view, /token_hint/)
  assert.doesNotMatch(api, /api_key: string/)
})

test('create flow defaults to least privilege and supports multi-space per-KB scope', () => {
  assert.match(view, /ref<MCPCapability\[]>\(\['retrieve', 'chat'\]\)/)
  assert.doesNotMatch(view, /value: 'ingest'|value: 'manage_kbs'/)
  assert.match(view, /selectedTenantIds/)
  assert.match(view, /kb_scope_mode/)
  assert.match(view, /source_type: 'shared'/)
  assert.match(view, /wizardSteps = \['客户端', '空间', '知识库', '能力', '有效期'\]/)
})

test('frontend adapter uses M1 PATCH, rotate, revoke and scope-options contracts', () => {
  assert.match(api, /patch\(`\/api\/v1\/mcp-api-keys\/\$\{id\}`/)
  assert.match(api, /post\(`\/api\/v1\/mcp-api-keys\/\$\{id\}\/rotate`/)
  assert.match(api, /del\(`\/api\/v1\/mcp-api-keys\/\$\{id\}`/)
  assert.match(api, /get\('\/api\/v1\/mcp-api-keys\/scope-options'\)/)
})

test('secret is one-time only and forgotten credentials require rotation', () => {
  assert.match(view, /Secret 创建后可持续使用，直至过期、被撤销或重新生成/)
  assert.match(view, /完整 Secret 仅在创建或重新生成时显示一次/)
  assert.match(view, /服务端不会保存可再次查看的明文 Secret/)
  assert.match(view, /重新生成 Secret/)
  assert.match(view, /旧 Secret 将立即失效/)
  assert.match(view, /rotateMCPAccessKey/)
  assert.doesNotMatch(api, /reveal/i)
  assert.doesNotMatch(view, /localStorage|sessionStorage|indexedDB|console\./)
  assert.match(view, /clearPlaintextToken/)
  assert.match(view, /onBeforeUnmount\(clearPlaintextToken\)/)
})

test('edit flow normalizes the nullable API response before opening the wizard', () => {
  assert.match(view, /credentialToEditDraft\(key\)/)
  assert.match(view, /editorVisible\.value = true/)
  assert.match(view, /updateMCPAccessKey\(editingID\.value, payload\)/)
  assert.match(view, /await load\(\)/)
})

test('expired rotate requires an explicit future date or never-expires choice', () => {
  assert.match(view, /lifecycleState\(rotateKey\) === 'expired'/)
  assert.match(view, /必须明确选择未来有效期或永不过期/)
  assert.match(view, /rotateExpiryMode\.value === 'custom'/)
  assert.doesNotMatch(view, /key\.status === 'expired' \? \{ expires_at_unix: Math\.floor\(Date\.now\(\) \/ 1000\) \+ 90/)
})

test('revoked credentials keep history and only expose reconnect plus details', () => {
  assert.match(view, /credentialLifecycleActions\(key\)/)
  assert.match(view, /action === 'reconnect'/)
  assert.match(view, /lifecycleState\(selectedKey\) !== 'revoked'/)
  assert.match(view, /v-else theme="primary" @click="requestReconnect\(selectedKey\)"/)
  assert.doesNotMatch(view, /已撤销|撤销接入/)
})

test('reconnect uses the existing create API, reveals first, and preserves exact scope for backend validation', () => {
  assert.match(view, /credentialToReconnectPayload\(reconnectKey\.value\)/)
  assert.match(view, /createReconnectAndReveal\(payload, createMCPAccessKey/)
  assert.match(view, /showSecret\(data\); reconnectVisible\.value = false/)
  assert.match(view, /不会缩减授权范围/)
  assert.match(lifecycle, /tenant_scopes: cloneTenantScopes\(credential\.tenant_scopes\)/)
  assert.doesNotMatch(lifecycle, /availableKBRefs|scopeOptions/)
})

test('status filters and all UI state decisions use the lifecycle helper', () => {
  assert.match(view, /statusFilter = ref<CredentialStatusFilter>\('all'\)/)
  assert.match(view, /credentialMatchesStatusFilter/)
  assert.match(view, /getCredentialLifecycleState/)
  assert.match(view, /全部/)
  assert.match(view, /已启用/)
  assert.match(view, /已停用/)
})

test('standalone access center has responsive desktop and compact layouts without embedded mode', () => {
  assert.match(view, /<main class="mcp-access-center">/)
  assert.doesNotMatch(view, /embedded/)
  assert.match(view, /@media\(max-width:1480px\)/)
  assert.match(view, /grid-template-areas:"identity scope capabilities" "meta meta actions"/)
  assert.match(view, /@media\(max-width:980px\)/)
  assert.match(view, /@media\(max-width:760px\)/)
})
