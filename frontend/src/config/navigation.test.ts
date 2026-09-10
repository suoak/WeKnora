import assert from 'node:assert/strict'
import test from 'node:test'
import { isNavigationEntryActive, NAVIGATION_REGISTRY, visibleNavigationEntries } from './navigation.ts'

const supported = () => true

test('viewer and contributor navigation excludes management groups', () => {
  for (const role of ['viewer', 'contributor']) {
    const entries = visibleNavigationEntries({ role, isSystemAdmin: false, supports: supported })
    assert.deepEqual(entries.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents'])
  }
})

test('workspace and system administration entries follow their distinct authority', () => {
  const admin = visibleNavigationEntries({ role: 'admin', isSystemAdmin: false, supports: supported })
  assert.ok(admin.some((entry) => entry.group === 'workspace'))
  assert.ok(admin.every((entry) => entry.group !== 'system'))

  const systemAdmin = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: true, supports: supported })
  assert.ok(systemAdmin.some((entry) => entry.group === 'system'))
  assert.ok(systemAdmin.every((entry) => entry.group !== 'workspace'))
})

test('capabilities and active settings section are respected', () => {
  const entries = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: (key) => key !== 'agents' })
  assert.ok(!entries.some((entry) => entry.id === 'agents'))
  const usage = NAVIGATION_REGISTRY.find((entry) => entry.id === 'usage-analytics')!
  assert.equal(isNavigationEntryActive(usage, '/platform/settings', 'usage-analytics'), true)
  assert.equal(isNavigationEntryActive(usage, '/platform/settings', 'models'), false)
})
