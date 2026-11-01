import assert from 'node:assert/strict'
import { test } from 'node:test'
import { personalDocumentAction, personalAuthorizationTarget, personalReauthorizationAllowed, personalFailureReason } from './personalDocumentActions.ts'

test('personal actions distinguish failed and published documents without changing ordinary uploads', () => {
  assert.equal(personalDocumentAction('web', 'failed'), undefined)
  assert.equal(personalDocumentAction(undefined, 'completed'), undefined)
  assert.equal(personalDocumentAction('feishu_personal', 'failed'), 'retryParse')
  assert.equal(personalDocumentAction('feishu_personal', 'completed'), 'syncSource')
  for (const state of ['pending', 'processing', 'finalizing']) {
    assert.equal(personalDocumentAction('feishu_personal', state), undefined)
  }
})

test('only explicit server source-owner permission on 403 offers reauthorization', () => {
  const details = { reason: 'personal_exact_source_owner_reauthorization_required', reauthorize_allowed: true }
  assert.equal(personalReauthorizationAllowed({ status: 403, error: { details } }), true)
  for (const status of [200, 401, 409, 500]) {
    assert.equal(personalReauthorizationAllowed({ status, error: { details } }), false)
  }
  for (const reauthorize_allowed of [false, 'true', undefined]) {
    assert.equal(personalReauthorizationAllowed({ status: 403, error: { details: { ...details, reauthorize_allowed } } }), false)
  }
  assert.equal(personalReauthorizationAllowed({ status: 403, error: { details: { ...details, reason: 'permission_denied' } } }), false)
})

test('reauthorization navigation accepts only the normal Feishu account origin', () => {
  assert.equal(personalAuthorizationTarget('https://accounts.feishu.cn/open-apis/authen/v1/authorize?state=normal'),
    'https://accounts.feishu.cn/open-apis/authen/v1/authorize?state=normal')
  for (const value of ['http://accounts.feishu.cn/', 'https://accounts.feishu.cn.evil.test/',
    'javascript:alert(1)', 'https://user:password@accounts.feishu.cn/', 'https://accounts.feishu.cn/#token']) {
    assert.throws(() => personalAuthorizationTarget(value))
  }
})

test('personal failures use fixed human-readable categories without reflecting source errors', () => {
  assert.equal(personalFailureReason('transport=security_rejected'), 'safetyFailure')
  assert.equal(personalFailureReason('permission denied'), 'permissionFailure')
  assert.equal(personalFailureReason('context deadline exceeded'), 'timeoutFailure')
  assert.equal(personalFailureReason('personal_exact_document_unverified generation=4'), 'verificationFailure')
  assert.equal(personalFailureReason('private provider payload'), 'processingFailure')
})
