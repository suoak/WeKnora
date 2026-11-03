import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { isUnifiedPersonalKey, personalKnowledgeRangePayload, personalPrivateScopeUpdate } from './personalMcpPresentation.ts'
import type { MCPScopeOption, MCPTenantScope } from '../../api/mcpAccessKeys'

test('client name never upgrades a legacy or machine credential', () => {
  assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', capabilities: ['retrieve', 'enterprise_retrieve'] }), true)
  for (const capabilities of [['retrieve'], ['retrieve', 'chat'], ['retrieve', 'read_agents'], ['enterprise_retrieve'], ['retrieve', 'enterprise_retrieve', 'chat']] as any[]) {
    assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', capabilities }), false)
  }
  assert.equal(isUnifiedPersonalKey({ scope_type: 'tenant' as any, capabilities: ['retrieve', 'enterprise_retrieve'] }), false)
  assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', client_type: 'generic', capabilities: ['retrieve'] }), true)
  assert.equal(isUnifiedPersonalKey({ scope_type: 'user_mcp', client_type: 'workmate', capabilities: ['retrieve'] }), false)
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

test('one existing navigation route and clear default/selected range without public-scope copy', () => {
  const view = readFileSync(new URL('./MCPAccessKeys.vue', import.meta.url), 'utf8')
  const router = readFileSync(new URL('../../router/index.ts', import.meta.url), 'utf8')
  assert.match(router, /path: "mcp-access"/)
  assert.match(router, /name: "mcpAccessCenter"/)
  assert.match(view, /<h1>MCP 接入<\/h1>/)
  assert.match(view, /将 KnowHub 企业知识连接到常用 AI 工具/)
  assert.doesNotMatch(view, /@click="openCreate"|v-model="personalConfig"/)
  const create = view.slice(view.indexOf('v-model:visible="personalCreateVisible"'), view.indexOf('v-model:visible="privateScopeVisible"'))
  assert.match(create, /有效期/)
  assert.doesNotMatch(create, /<label>.*(?:能力|模型|Agent|客户端)/)
  assert.match(create, /默认范围/)
  assert.match(create, /指定范围/)
  assert.match(create, /当前有权读取的私人知识/)
  assert.doesNotMatch(view.slice(0, view.indexOf('<script')), /Portal|公共知识|八个只读工具|动态全部个人授权空间/)
  assert.match(view, /personalKnowledgeRangePayload\(personalRange.value/)
  assert.match(view, /历史接入/)
  assert.match(view, /watch\(configVisible, visible => \{ if \(!visible\) clearPlaintextToken\(\)/)
})

const rangeOptions: MCPScopeOption[] = [{ tenant_id: 1, tenant_name: 'One', role: 'viewer', owned_knowledge_bases: [{ id: 'private-one', name: 'Private' }], shared_knowledge_bases: [{ knowledge_base: { id: 'shared-one', name: 'Shared' }, share_id: 'share-one', organization_id: 'org', org_name: 'Org', permission: 'read' }] }, { tenant_id: 2, tenant_name: 'Two', role: 'viewer', owned_knowledge_bases: [{ id: 'private-two', name: 'Other' }], shared_knowledge_bases: [] }]

test('default range preserves dynamic authorization and selected range never gains enterprise access', () => {
  const snapshot = structuredClone(rangeOptions)
  const defaults = personalKnowledgeRangePayload('default', {}, rangeOptions)
  assert.equal(defaults.personal_scope_mode, 'dynamic_all')
  assert.deepEqual(defaults.capabilities, ['retrieve', 'enterprise_retrieve'])
  assert.equal(defaults.tenant_scopes.length, 2)
  const selected = personalKnowledgeRangePayload('selected', { 1: ['owned:private-one', 'shared:shared-one:share-one', 'owned:private-one'] }, rangeOptions)
  assert.equal(selected.personal_scope_mode, 'fixed')
  assert.deepEqual(selected.capabilities, ['retrieve'])
  assert.deepEqual(selected.tenant_scopes, [{ tenant_id: 1, kb_scope_mode: 'selected', knowledge_bases: [{ knowledge_base_id: 'private-one', source_type: 'owned' }, { knowledge_base_id: 'shared-one', source_type: 'shared', kb_share_id: 'share-one' }] }])
  assert.deepEqual(rangeOptions, snapshot)
})

test('empty, public-only, foreign and stale selections cannot become all knowledge', () => {
  const invalidSelections: Array<Record<number, string[]>> = [{}, { 1: [] }, { 1: ['owned:public-only'] }, { 99: ['owned:private-two'] }, { 1: ['shared:shared-one:revoked-share'] }]
  for (const selections of invalidSelections) {
    assert.throws(() => personalKnowledgeRangePayload('selected', selections, rangeOptions))
  }
  assert.throws(() => personalKnowledgeRangePayload('default', {}, []))
})
