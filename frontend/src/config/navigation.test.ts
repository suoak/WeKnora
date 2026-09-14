import assert from 'node:assert/strict'
import test from 'node:test'
import { isNavigationEntryActive, NAVIGATION_REGISTRY, visibleNavigationEntries } from './navigation.ts'

const supported = () => true

test('viewer and contributor see global and accessible workspace navigation only', () => {
  for (const role of ['viewer', 'contributor']) {
    const entries = visibleNavigationEntries({ role, isSystemAdmin: false, supports: supported })
    assert.deepEqual(entries.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'members'])
    assert.ok(entries.every((entry) => entry.group !== 'management'))
  }
})

test('management center is visible to workspace admins and system admins', () => {
  const admin = visibleNavigationEntries({ role: 'admin', isSystemAdmin: false, supports: supported })
  assert.deepEqual(admin.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'members', 'management-center'])

  const systemAdmin = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: true, supports: supported })
  assert.deepEqual(systemAdmin.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'members', 'management-center'])
})

test('capabilities and active settings section are respected', () => {
  const entries = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: (key) => key !== 'agents' })
  assert.ok(!entries.some((entry) => entry.id === 'agents'))
  const management = NAVIGATION_REGISTRY.find((entry) => entry.id === 'management-center')!
  assert.equal(isNavigationEntryActive(management, '/platform/settings', 'usage-analytics'), true)
  assert.equal(isNavigationEntryActive(management, '/platform/knowledge-bases'), false)
  assert.deepEqual(NAVIGATION_REGISTRY.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'members', 'management-center'])
})
