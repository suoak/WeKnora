import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { isUnifiedPersonalKey, personalPrivateScopeUpdate } from './personalMcpPresentation.ts'
import type { MCPScopeOption, MCPTenantScope } from '../../api/mcpAccessKeys'

test('client name never upgrades a legacy or machine credential', () => {
  assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', capabilities: ['retrieve', 'enterprise_retrieve'] }), true)
  for (const capabilities of [['retrieve'], ['retrieve', 'chat'], ['retrieve', 'read_agents'], ['enterprise_retrieve'], ['retrieve', 'enterprise_retrieve', 'chat']] as any[]) {
    assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', capabilities }), false)
  }
  assert.equal(isUnifiedPersonalKey({ scope_type: 'tenant' as any, capabilities: ['retrieve', 'enterprise_retrieve'] }), false)
})

test('explicit new private space preserves every old selected restriction', () => {
  const original: MCPTenantScope[] = [{ tenant_id: 1, kb_scope_mode: 'selected', knowledge_bases: [{ knowledge_base_id: 'one', source_type: 'owned' }] }]
  const snapshot = structuredClone(original)
  const options = [{ tenant_id: 7 }] as MCPScopeOption[]
  const scopes = personalPrivateScopeUpdate(original, [7, 7], options)
  assert.deepEqual(scopes, [...original, { tenant_id: 7, kb_scope_mode: 'all', knowledge_bases: [] }])
  assert.deepEqual(original, snapshot)
  assert.throws(() => personalPrivateScopeUpdate(original, [99], options))
  assert.deepEqual(personalPrivateScopeUpdate(original, [], []), original)
})

test('one existing navigation route, single personal create entry, fixed read-only payload', () => {
  const view = readFileSync(new URL('./MCPAccessKeys.vue', import.meta.url), 'utf8')
  const router = readFileSync(new URL('../../router/index.ts', import.meta.url), 'utf8')
  assert.match(router, /path: "mcp-access"/)
  assert.match(router, /name: "mcpAccessCenter"/)
  assert.match(view, /<h1>MCP 接入<\/h1>/)
  assert.match(view, /将 KnowHub 企业知识连接到常用 AI 工具/)
  assert.doesNotMatch(view, /@click="openCreate"|v-model="personalConfig"/)
  const create = view.slice(view.indexOf('<t-dialog v-model:visible="personalCreateVisible"'), view.indexOf('<t-dialog v-model:visible="privateScopeVisible"'))
  assert.match(create, /有效期/)
  assert.doesNotMatch(create, /tenant_id|kb_id|客户端|能力|模型|Agent/)
  assert.match(view, /capabilities: \['retrieve', 'enterprise_retrieve'\]/)
  assert.match(view, /历史接入/)
  assert.match(view, /watch\(configVisible, visible => \{ if \(!visible\) clearPlaintextToken\(\)/)
})
