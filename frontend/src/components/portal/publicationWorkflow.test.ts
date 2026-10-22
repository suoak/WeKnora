import { test } from 'node:test'
import assert from 'node:assert/strict'
import { publicationActionAllowed, publicationBase, type PublicationSnapshot } from './publicationWorkflow'
test('owner and reviewer share an exact tenant-bound workflow with default-disabled actions', () => {
  assert.equal(publicationBase(99, true), '/api/v1/tenants/99/knowledge-publication')
  assert.equal(publicationBase(99, false), '/api/v1/system/admin/portal/spaces/99/knowledge-publication')
  assert.throws(() => publicationBase(0, true)); assert.throws(() => publicationBase(NaN, true))
  const s: PublicationSnapshot = { tenant_id: 99, knowledge_base_id: 'kb', state: 'pending_review', proposed_by: 'owner', reviewed_by: null, reviewed_at: null, reason: '', inherit_new_ordinary: true, proposer_active_owner: true, permissions: { owner: true, reviewer: false, mutations_enabled: false }, resources: [{kind:'space',id:'99',revision:0,version:'a'},{kind:'kb',id:'kb',revision:0,version:'b'},{kind:'knowledge',id:'doc',revision:0,version:'c'}] }
  assert.equal(publicationActionAllowed(s,'propose','ordinary inspected',true),false)
  s.permissions.mutations_enabled = true
  assert.equal(publicationActionAllowed(s,'propose','ordinary inspected',true),true)
  assert.equal(publicationActionAllowed(s,'approve','reviewed',true),false)
  assert.equal(publicationActionAllowed(s,'propose','ordinary inspected',false),false)
  s.permissions = { owner: false, reviewer: true, mutations_enabled: true }
  assert.equal(publicationActionAllowed(s,'approve','reviewed',true),true)
  s.proposer_active_owner = false
  assert.equal(publicationActionAllowed(s,'approve','reviewed',true),false)
  assert.equal(publicationActionAllowed(s,'revoke','urgent withdrawal',false),true)
})
