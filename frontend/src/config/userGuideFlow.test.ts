import assert from 'node:assert/strict'
import test from 'node:test'
import { buildUserGuideFlow } from './userGuideFlow.ts'

test('ordinary guide stays task-oriented and excludes administration', () => {
  const keys = buildUserGuideFlow({ role: 'viewer', isSystemAdmin: false, agentsSupported: true, mcpSupported: true }).map((step) => step.key)
  assert.deepEqual(keys, ['welcome', 'portal', 'quickAsk', 'space', 'knowledge', 'agents', 'mcp', 'done'])
  assert.ok(!keys.includes('models'))
})

test('role-specific guide steps are additive', () => {
  const admin = buildUserGuideFlow({ role: 'admin', isSystemAdmin: false, agentsSupported: false, mcpSupported: false }).map((step) => step.key)
  assert.ok(admin.includes('members') && admin.includes('integrations'))
  const system = buildUserGuideFlow({ role: 'viewer', isSystemAdmin: true, agentsSupported: false, mcpSupported: false }).map((step) => step.key)
  assert.ok(system.includes('systemAdmin'))
})
