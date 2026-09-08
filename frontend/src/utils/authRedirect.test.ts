import assert from 'node:assert/strict'
import test from 'node:test'
import {
  AUTH_RETURN_TARGET_KEY,
  DEFAULT_AUTHENTICATED_LANDING,
  consumeAuthReturnTarget,
  normalizeAuthReturnTarget,
  rememberAuthReturnTarget,
  resolvePostAuthLanding,
  resolveRootLanding,
  type AuthRedirectStorage,
} from './authRedirect'

function memoryStorage(): AuthRedirectStorage & { values: Map<string, string> } {
  const values = new Map<string, string>()
  return {
    values,
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => { values.delete(key) },
  }
}

test('password and OIDC login without a target land on Portal for every user kind', () => {
  assert.equal(resolvePostAuthLanding(), DEFAULT_AUTHENTICATED_LANDING)
  assert.equal(resolvePostAuthLanding({ storedTarget: null }), '/portal')
})

test('explicit and captured business targets take precedence over Portal', () => {
  assert.equal(resolvePostAuthLanding({ explicitTarget: '/platform/agents?view=mine' }), '/platform/agents?view=mine')
  assert.equal(resolvePostAuthLanding({ storedTarget: '/platform/settings?section=account' }), '/platform/settings?section=account')
  assert.equal(resolvePostAuthLanding({ storedTarget: '/platform/knowledge-bases/kb-1#files' }), '/platform/knowledge-bases/kb-1#files')
})

test('organization join targets remain restorable while unsafe targets are rejected', () => {
  const join = '/platform/organizations?invite_code=abc123'
  assert.equal(normalizeAuthReturnTarget(join), join)
  for (const unsafe of ['https://evil.example/x', '//evil.example/x', '/\\evil.example/x', '/login', '/login/', '/register']) {
    assert.equal(normalizeAuthReturnTarget(unsafe), null)
  }
})

test('captured targets are consumed once to avoid stale redirects and loops', () => {
  const storage = memoryStorage()
  rememberAuthReturnTarget('/platform/knowledge-bases/42', storage)
  assert.equal(storage.values.get(AUTH_RETURN_TARGET_KEY), '/platform/knowledge-bases/42')
  assert.equal(consumeAuthReturnTarget(storage), '/platform/knowledge-bases/42')
  assert.equal(consumeAuthReturnTarget(storage), null)
})

test('root uses Portal except when Lite recent-page recovery applies', () => {
  assert.equal(resolveRootLanding(false, '/platform/agents'), '/portal')
  assert.equal(resolveRootLanding(true, '/platform/agents?tab=mine'), '/platform/agents?tab=mine')
  assert.equal(resolveRootLanding(true, '/platform/organizations?invite_code=x'), '/portal')
})

test('explicit onboarding remains available but root and login cannot form a loop', () => {
  assert.equal(resolvePostAuthLanding({ explicitTarget: '/onboarding/workspace' }), '/onboarding/workspace')
  assert.equal(resolvePostAuthLanding({ explicitTarget: '/' }), '/portal')
  assert.equal(resolvePostAuthLanding({ explicitTarget: '/login' }), '/portal')
})
