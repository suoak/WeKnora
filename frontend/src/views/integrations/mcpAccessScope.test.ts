import test from 'node:test'
import assert from 'node:assert/strict'
import { invalidCredentialScopes } from './mcpAccessScope.ts'
import type { MCPAccessKey, MCPScopeOption } from '../../api/mcpAccessKeys.ts'

const options: MCPScopeOption[] = [{
  tenant_id: 2, tenant_name: 'Space B', role: 'viewer',
  owned_knowledge_bases: [{ id: 'kb-1', name: 'KB 1' }],
  shared_knowledge_bases: [{ knowledge_base: { id: 'kb-shared', name: 'Shared' }, share_id: 'share-live', organization_id: 'org', org_name: 'Org', permission: 'read' }],
}]

test('identifies lost membership and revoked shared grants without silently deleting scope', () => {
  const credential = { tenant_scopes: [
    { tenant_id: 1, kb_scope_mode: 'all', knowledge_bases: [] },
    { tenant_id: 2, kb_scope_mode: 'selected', knowledge_bases: [
      { knowledge_base_id: 'kb-1', source_type: 'owned' },
      { knowledge_base_id: 'kb-shared', source_type: 'shared', kb_share_id: 'share-revoked' },
    ] },
  ] } as Pick<MCPAccessKey, 'tenant_scopes'>
  const result = invalidCredentialScopes(credential, options)
  assert.deepEqual(result.invalidTenantIds, [1])
  assert.equal(result.invalidKBRefs[2][0].kb_share_id, 'share-revoked')
  assert.equal(credential.tenant_scopes.length, 2)
})
