import assert from 'node:assert/strict'
import test from 'node:test'
import { FEEDBACK_URL, USER_HELP_URL, isNavigationEntryActive, NAVIGATION_REGISTRY, visibleNavigationEntries } from './navigation.ts'

const supported = () => true

test('user help opens the approved document without requiring management permissions', () => {
  const entry = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: supported }).find(entry => entry.id === 'user-help')!
  assert.equal(entry.externalUrl, 'https://ruijie.feishu.cn/docx/E3fSdQCgyoy6bwxHTWVckgWYnLc')
  assert.equal(entry.externalUrl, USER_HELP_URL)
  assert.equal(entry.route, undefined)
  assert.equal(isNavigationEntryActive(entry, '/platform/mcp-access'), false)
})

test('viewer and contributor see global and accessible workspace navigation only', () => {
  for (const role of ['viewer', 'contributor']) {
    const entries = visibleNavigationEntries({ role, isSystemAdmin: false, supports: supported })
    assert.deepEqual(entries.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'mcp-access', 'members', 'feedback', 'user-help'])
    assert.ok(entries.every((entry) => entry.group !== 'management'))
  }
})

test('management center is visible to workspace admins and system admins', () => {
  const admin = visibleNavigationEntries({ role: 'admin', isSystemAdmin: false, supports: supported })
  assert.deepEqual(admin.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'mcp-access', 'members', 'feedback', 'user-help', 'management-center'])

  const systemAdmin = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: true, supports: supported })
  assert.deepEqual(systemAdmin.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'mcp-access', 'members', 'feedback', 'user-help', 'management-center'])
})

test('capabilities and active settings section are respected', () => {
  const entries = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: (key) => key !== 'agents' })
  assert.ok(!entries.some((entry) => entry.id === 'agents'))
  const management = NAVIGATION_REGISTRY.find((entry) => entry.id === 'management-center')!
  assert.equal(isNavigationEntryActive(management, '/platform/settings', 'usage-analytics'), true)
  assert.equal(isNavigationEntryActive(management, '/platform/knowledge-bases'), false)
  assert.deepEqual(NAVIGATION_REGISTRY.map((entry) => entry.id), ['portal', 'knowledge-bases', 'agents', 'mcp-access', 'members', 'feedback', 'user-help', 'management-center'])
})

test('feedback is a role-independent external destination and never becomes active', () => {
  const feedback = NAVIGATION_REGISTRY.find((entry) => entry.id === 'feedback')!
  assert.equal(feedback.externalUrl, FEEDBACK_URL)
  assert.equal(feedback.route, undefined)
  assert.equal(feedback.requiredCapabilities, undefined)
  assert.equal(feedback.minimumRole, undefined)
  assert.equal(feedback.visibility, undefined)
  assert.equal(isNavigationEntryActive(feedback, '/platform/knowledge-bases'), false)

  const beforeSpaceChange = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: supported })
  const afterSpaceChange = visibleNavigationEntries({ role: 'viewer', isSystemAdmin: false, supports: supported })
  assert.ok(beforeSpaceChange.some((entry) => entry.id === 'feedback'))
  assert.deepEqual(afterSpaceChange, beforeSpaceChange)
})
