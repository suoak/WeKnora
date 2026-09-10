import assert from 'node:assert/strict'
import test from 'node:test'
import { tenantSwitchTargetPath } from './tenantSwitchPolicy.ts'

test('tenant switch preserves safe tenant-independent landing pages', () => {
  assert.equal(tenantSwitchTargetPath('/portal'), '/portal')
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/agents'), '/platform/agents')
})

test('tenant switch removes stale resource context', () => {
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases/kb-1'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/knowledge-bases/kb-1/creatChat'), '/platform/knowledge-bases')
  assert.equal(tenantSwitchTargetPath('/platform/chat/session-1'), '/portal')
  assert.equal(tenantSwitchTargetPath('/platform/settings'), '/portal')
})
