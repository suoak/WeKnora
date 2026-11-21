import assert from 'node:assert/strict'
import test from 'node:test'
import { notificationTarget, isResolvedAccessRequest } from './notificationTarget'

test('notification navigation accepts only known types and workspace IDs', () => {
  const row = { reference_type: 'tenant_access_request', notification_type: 'access_request_submitted', tenant_id: 7 }
  assert.equal(notificationTarget(row), '/platform/settings?section=members&access_requests=1')
  assert.equal(notificationTarget({ ...row, notification_type: 'access_request_approved' }), '/platform/knowledge-bases')
  assert.equal(notificationTarget({ ...row, notification_type: 'access_request_rejected' }), null)
  assert.equal(notificationTarget({ ...row, reference_type: 'https://evil.example' }), null)
  for (const tenant_id of [0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) assert.equal(notificationTarget({ ...row, tenant_id }), null)
})

test('resolved Owner reminders cannot reopen a completed approval task', () => {
  const row = { reference_type: 'tenant_access_request', notification_type: 'access_request_submitted', tenant_id: 7 }
  for (const request_status of ['approved', 'rejected', 'cancelled'] as const) {
    assert.equal(isResolvedAccessRequest({ ...row, request_status }), true)
    assert.equal(notificationTarget({ ...row, request_status }), null)
  }
  assert.equal(notificationTarget({ ...row, request_status: 'pending' }), '/platform/settings?section=members&access_requests=1')
  // The applicant's approval notice still opens the newly accessible workspace.
  assert.equal(notificationTarget({ ...row, notification_type: 'access_request_approved', request_status: 'approved' }), '/platform/knowledge-bases')
})
