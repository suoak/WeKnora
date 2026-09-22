import assert from 'node:assert/strict'
import test from 'node:test'

import type { MCPAccessKey } from '../../api/mcpAccessKeys.ts'
import {
  credentialLifecycleActions,
  credentialMatchesStatusFilter,
  credentialToReconnectPayload,
  getCredentialLifecycleState,
} from './mcpAccessLifecycle.ts'

const NOW = Date.parse('2026-09-22T00:00:00Z')

function credential(overrides: Partial<MCPAccessKey> = {}): MCPAccessKey {
  return {
    id: 7,
    name: 'Jerry - WorkBuddy',
    scope_type: 'user_mcp',
    client_type: 'workbuddy',
    status: 'revoked',
    token_hint: 'aB3x',
    capabilities: ['retrieve', 'chat'],
    tenant_scopes: [{
      tenant_id: 11,
      kb_scope_mode: 'selected',
      knowledge_bases: [
        { knowledge_base_id: 'owned-1', source_type: 'owned' },
        { knowledge_base_id: 'shared-1', source_type: 'shared', kb_share_id: 'share-1' },
      ],
    }],
    revoked_at: '2026-09-20T00:00:00Z',
    last_used_at: '2026-09-19T00:00:00Z',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-09-20T00:00:00Z',
    ...overrides,
  }
}

test('one lifecycle helper drives revoked, expired and active states and actions', () => {
  assert.equal(getCredentialLifecycleState(credential(), NOW), 'revoked')
  assert.equal(getCredentialLifecycleState(credential({ status: 'active', revoked_at: undefined, expires_at: '2026-09-21T00:00:00Z' }), NOW), 'expired')
  assert.equal(getCredentialLifecycleState(credential({ status: 'active', revoked_at: undefined, expires_at: '2026-10-01T00:00:00Z' }), NOW), 'active')
  assert.deepEqual(credentialLifecycleActions(credential()), ['reconnect'])
  assert.deepEqual(credentialLifecycleActions(credential({ status: 'expired', revoked_at: undefined })), ['edit', 'rotate', 'revoke'])
})

test('status filter does not misclassify expired credentials as disabled', () => {
  const expired = credential({ status: 'expired', revoked_at: undefined })
  assert.equal(credentialMatchesStatusFilter(expired, 'all'), true)
  assert.equal(credentialMatchesStatusFilter(expired, 'active'), false)
  assert.equal(credentialMatchesStatusFilter(expired, 'revoked'), false)
})

test('reconnect copies exact non-sensitive scope without credential identity or history', () => {
  const original = credential({ expires_at: '2026-10-01T00:00:00Z' })
  const payload = credentialToReconnectPayload(original, NOW)
  assert.deepEqual(payload, {
    name: 'Jerry - WorkBuddy',
    client_type: 'workbuddy',
    capabilities: ['retrieve', 'chat'],
    tenant_scopes: original.tenant_scopes,
    expires_at_unix: Date.parse('2026-10-01T00:00:00Z') / 1000,
  })
  assert.notEqual(payload.tenant_scopes, original.tenant_scopes)
  assert.notEqual(payload.tenant_scopes[0].knowledge_bases, original.tenant_scopes[0].knowledge_bases)
  assert.equal('id' in payload, false)
  assert.equal('token_hint' in payload, false)
  assert.equal('revoked_at' in payload, false)
  assert.equal('last_used_at' in payload, false)
})

test('reconnect expiry follows create API semantics for never, future, expired and malformed/null', () => {
  const never = credential()
  const future = credential({ expires_at: '2026-10-01T00:00:00Z' })
  const expired = credential({ expires_at: '2026-09-01T00:00:00Z' })
  const malformed = credential({ expires_at: 'not-a-date' })
  const nullable = { ...credential(), expires_at: null }

  assert.deepEqual(credentialToReconnectPayload(never, NOW), {
    name: never.name,
    client_type: never.client_type,
    capabilities: never.capabilities,
    tenant_scopes: never.tenant_scopes,
    never_expires: true,
  })
  assert.equal(credentialToReconnectPayload(future, NOW).expires_at_unix, Date.parse(future.expires_at!) / 1000)
  assert.equal('expires_at_unix' in credentialToReconnectPayload(expired, NOW), false)
  assert.equal('never_expires' in credentialToReconnectPayload(expired, NOW), false)
  assert.equal('expires_at_unix' in credentialToReconnectPayload(malformed, NOW), false)
  assert.equal('never_expires' in credentialToReconnectPayload(nullable, NOW), false)
})

test('nullable scopes stay invalid instead of being silently reduced to currently visible grants', () => {
  const payload = credentialToReconnectPayload({
    ...credential(),
    capabilities: null,
    tenant_scopes: null,
  }, NOW)
  assert.deepEqual(payload.capabilities, [])
  assert.deepEqual(payload.tenant_scopes, [])
})
