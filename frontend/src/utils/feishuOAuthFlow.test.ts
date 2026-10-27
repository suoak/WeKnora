import test from 'node:test'
import assert from 'node:assert/strict'
import { safeImportPath, callbackFailure } from './feishuOAuthFlow'

test('OAuth return path is restricted to import pages on the same origin', () => {
  assert.equal(safeImportPath('/platform/knowledge-bases/library?knowledge_id=old'), '/platform/knowledge-bases/library')
  assert.equal(safeImportPath('/platform/feishu/imports?kb=library&batch=job'), '/platform/feishu/imports?kb=library&batch=job')
  for (const path of ['//evil.test/platform/knowledge-bases','https://evil.test/platform/knowledge-bases','/platform/../login','/platform/knowledge-bases/%2f%2fevil.test','/platform/knowledge-bases\\evil','javascript:alert(1)','/platform/settings','/feishu/callback?code=secret']) assert.equal(safeImportPath(path), '/platform/knowledge-bases')
})
test('Callback failures are actionable without exposing provider parameters', () => {
  assert.match(callbackFailure({status:401}), /登录已失效/)
  assert.match(callbackFailure({category:'cancelled'}), /取消/)
  assert.match(callbackFailure({error:'invalid_or_expired_oauth_state'}), /过期或已使用/)
  assert.match(callbackFailure({category:'timeout'}), /无法确认/)
  assert.match(callbackFailure({error:'feishu_oauth_application_configuration'}), /联系管理员/)
})