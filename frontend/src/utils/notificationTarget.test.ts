import assert from 'node:assert/strict'
import test from 'node:test'
import { notificationTarget } from './notificationTarget'

test('notification navigation accepts only known types and workspace IDs', () => {
  const row = { reference_type: 'tenant_access_request', notification_type: 'access_request_submitted', tenant_id: 7 }
  assert.equal(notificationTarget(row), '/platform/settings?section=members&access_requests=1')
  assert.equal(notificationTarget({ ...row, notification_type: 'access_request_approved' }), '/platform/knowledge-bases')
  assert.equal(notificationTarget({ ...row, notification_type: 'access_request_rejected' }), null)
  assert.equal(notificationTarget({ ...row, reference_type: 'https://evil.example' }), null)
  for (const tenant_id of [0, -1, 1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) assert.equal(notificationTarget({ ...row, tenant_id }), null)
})
