import assert from 'node:assert/strict'
import test from 'node:test'
import {
  PORTAL_INTENT_STORAGE_KEY,
  PORTAL_INTENT_TTL_MS,
  consumePortalIntent,
  createPortalIntent,
  readPortalIntent,
  type PortalIntentStorage,
} from './portalIntent.ts'

class MemoryStorage implements PortalIntentStorage {
  readonly values = new Map<string, string>()
  getItem(key: string) { return this.values.get(key) ?? null }
  setItem(key: string, value: string) { this.values.set(key, value) }
  removeItem(key: string) { this.values.delete(key) }
}

test('creates only a structured, tab-local action without routes or query data', () => {
  const tabA = new MemoryStorage()
  const tabB = new MemoryStorage()
  assert.equal(createPortalIntent('search', 2, tabA, 100), true)
  assert.deepEqual(readPortalIntent(tabA, 100), { version: 1, action: 'search', targetTenantId: 2, createdAt: 100 })
  assert.equal(readPortalIntent(tabB, 100), null)
  assert.equal(tabA.getItem(PORTAL_INTENT_STORAGE_KEY)?.includes('/platform'), false)
})

test('expired, malformed, future, and unknown intents are removed without execution', () => {
  for (const raw of [
    '{bad json',
    JSON.stringify({ version: 1, action: 'delete', targetTenantId: 2, createdAt: 100 }),
    JSON.stringify({ version: 1, action: 'ask', targetTenantId: 2, createdAt: 100, route: '/unsafe' }),
    JSON.stringify({ version: 1, action: 'ask', targetTenantId: 2, createdAt: 101 }),
  ]) {
    const storage = new MemoryStorage()
    storage.setItem(PORTAL_INTENT_STORAGE_KEY, raw)
    assert.equal(readPortalIntent(storage, 100), null)
    assert.equal(storage.getItem(PORTAL_INTENT_STORAGE_KEY), null)
  }
  const expired = new MemoryStorage()
  createPortalIntent('search', 2, expired, 100)
  assert.equal(readPortalIntent(expired, 100 + PORTAL_INTENT_TTL_MS + 1), null)
  assert.equal(expired.getItem(PORTAL_INTENT_STORAGE_KEY), null)
})

test('tenant mismatch and revoked access clear intent before doing nothing', () => {
  for (const [activeTenantId, accessible, expected] of [[1, true, 'tenant-mismatch'], [2, false, 'inaccessible']] as const) {
    const storage = new MemoryStorage()
    let executions = 0
    createPortalIntent('search', 2, storage, 100)
    assert.equal(consumePortalIntent({ activeTenantId, isTargetAccessible: () => accessible, actions: { search: () => executions++ }, storage, now: 100 }), expected)
    assert.equal(executions, 0)
    assert.equal(storage.getItem(PORTAL_INTENT_STORAGE_KEY), null)
  }
})

test('search and ask execute once only, with removal before the callback', () => {
  for (const action of ['search', 'ask'] as const) {
    const storage = new MemoryStorage()
    let executions = 0
    createPortalIntent(action, 2, storage, 100)
    const actions = { [action]: () => {
      assert.equal(storage.getItem(PORTAL_INTENT_STORAGE_KEY), null)
      executions++
    } }
    assert.equal(consumePortalIntent({ activeTenantId: 2, isTargetAccessible: () => true, actions, storage, now: 100 }), 'executed')
    assert.equal(consumePortalIntent({ activeTenantId: 2, isTargetAccessible: () => true, actions, storage, now: 100 }), 'none')
    assert.equal(executions, 1)
  }
})

test('unavailable and throwing actions stay cleared and cannot replay on refresh or back', () => {
  for (const actions of [{}, { ask: () => { throw new Error('unavailable') } }]) {
    const storage = new MemoryStorage()
    createPortalIntent('ask', 2, storage, 100)
    const result = consumePortalIntent({ activeTenantId: 2, isTargetAccessible: () => true, actions, storage, now: 100 })
    assert.ok(result === 'action-unavailable' || result === 'execution-failed')
    assert.equal(storage.getItem(PORTAL_INTENT_STORAGE_KEY), null)
  }
})
