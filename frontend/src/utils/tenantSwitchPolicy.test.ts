import assert from 'node:assert/strict'
import test from 'node:test'
import { tenantSwitchTargetPath } from './tenantSwitchPolicy.ts'

test('active tenant switches always land in the target workspace knowledge bases', () => {
  assert.equal(tenantSwitchTargetPath('/portal'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/agents'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases/kb-1'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases/kb-1/creatChat'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/chat/session-1'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/settings'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/'), '/platform/knowledge-bases')
})
