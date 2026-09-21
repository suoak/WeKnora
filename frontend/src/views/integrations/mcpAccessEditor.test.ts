import assert from 'node:assert/strict'
import test from 'node:test'
import type { MCPKBRef } from '../../api/mcpAccessKeys.ts'
import { credentialToEditDraft, type EditableMCPAccessKey } from './mcpAccessEditor.ts'

const credential = (knowledgeBases: readonly MCPKBRef[] | null | undefined): EditableMCPAccessKey => ({
  id: 7,
  name: 'Workspace assistant',
  scope_type: 'user_mcp',
  client_type: 'cursor',
  status: 'active',
  token_hint: '••••ABCD',
  capabilities: ['retrieve', 'chat'],
  tenant_scopes: [{
    tenant_id: 11,
    kb_scope_mode: 'all',
    ...(knowledgeBases === undefined ? {} : { knowledge_bases: knowledgeBases === null ? null : [...knowledgeBases] }),
  }],
  expires_at: '2030-06-01T08:30:00Z',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
})

for (const [label, knowledgeBases] of [
  ['missing', undefined],
  ['null', null],
  ['empty', []],
] as const) {
  test(`normalizes ${label} knowledge_bases when editing an all-KB scope`, () => {
    const draft = credentialToEditDraft(credential(knowledgeBases))
    assert.deepEqual(draft.selectedTenantIds, [11])
    assert.deepEqual(draft.scopes[11], { mode: 'all', selected: [] })
    assert.deepEqual(draft.capabilities, ['retrieve', 'chat'])
    assert.equal(draft.expiryMode, 'custom')
    assert.match(draft.customExpiry, /^2030-06-01T/)
  })
}

test('normalizes nullable top-level edit fields to legal defaults', () => {
  const draft = credentialToEditDraft({
    ...credential([]),
    capabilities: null,
    tenant_scopes: null,
    expires_at: 'not-a-date',
  })
  assert.deepEqual(draft.capabilities, [])
  assert.deepEqual(draft.selectedTenantIds, [])
  assert.deepEqual(draft.scopes, {})
  assert.equal(draft.expiryMode, 'never')
  assert.equal(draft.customExpiry, '')
})

test('preserves selected owned and shared KB references', () => {
  const draft = credentialToEditDraft({
    ...credential([]),
    tenant_scopes: [{
      tenant_id: 11,
      kb_scope_mode: 'selected',
      knowledge_bases: [
        { knowledge_base_id: 'owned-kb', source_type: 'owned' },
        { knowledge_base_id: 'shared-kb', source_type: 'shared', kb_share_id: 'share-1' },
      ],
    }],
  })
  assert.deepEqual(draft.scopes[11].selected, ['owned:owned-kb', 'shared:shared-kb:share-1'])
})
