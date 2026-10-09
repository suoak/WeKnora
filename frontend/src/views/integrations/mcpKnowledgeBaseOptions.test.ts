import test from 'node:test'
import assert from 'node:assert/strict'
import { loadMcpKnowledgeBaseOptions } from './mcpKnowledgeBaseOptions'
const reply = (data: unknown) => async () => ({ success: true, data })
const share = (id: string, org = 'Team', tenant = 2) => ({ knowledge_base: { id, name: id }, org_name: org,
  source_tenant_id: tenant, permission: 'viewer' })
test('scope options merge owned and authorized multi-space shares once, retaining origins', async () => {
  const result = await loadMcpKnowledgeBaseOptions(reply([{ id: 'own', name: 'Own' }]),
    reply([share('own'), share('shared'), share('shared', 'Second'), share('other', 'Other', 3),
      { ...share('denied'), permission: 'none' }, { ...share('bad'), source_tenant_id: 0 }, {}]), 'Current')
  assert.equal(result.failed, false)
  assert.deepEqual(result.rows.map(row => row.id), ['own', 'shared', 'other'])
  assert.deepEqual(result.rows[0].sources, [])
  assert.deepEqual(result.rows[1].sources, ['Team', 'Second'])
  assert.equal(result.rows[2].space, '3')
})
test('scope refresh uses new grants; revocation is not restored from an old snapshot', async () => {
  const first = await loadMcpKnowledgeBaseOptions(reply([]), reply([share('revoked')]), 'Current')
  const next = await loadMcpKnowledgeBaseOptions(reply([]), reply([]), 'Current')
  assert.equal(first.rows.length, 1)
  assert.deepEqual(next, { rows: [], failed: false })
})
for (const status of [401, 403, 500]) test(`scope request ${status} fails closed without cached shared options`, async () => {
  const result = await loadMcpKnowledgeBaseOptions(reply([{ id: 'own' }]), async () => { throw new Error(String(status)) }, 'Current')
  assert.equal(result.failed, true)
  assert.deepEqual(result.rows.map(row => row.id), ['own'])
})
test('resolved API failure and malformed payload fail closed, valid empty responses do not', async () => {
  assert.equal((await loadMcpKnowledgeBaseOptions(reply([]), async () => ({ success: false }), 'Current')).failed, true)
  assert.equal((await loadMcpKnowledgeBaseOptions(reply([]), reply(null), 'Current')).failed, true)
  assert.deepEqual(await loadMcpKnowledgeBaseOptions(reply([]), reply([]), 'Current'), { rows: [], failed: false })
})
