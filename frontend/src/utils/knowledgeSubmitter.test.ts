import assert from 'node:assert/strict'
import test from 'node:test'
import { knowledgeSubmitterName } from './knowledgeSubmitter'

test('uses only validated server attribution for current and historical uploads', () => {
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'user', name: 'Alice' } }, 'System sync'), 'Alice')
  assert.equal(knowledgeSubmitterName({ metadata: { submitter: { name: 'Bob' } } }, 'System sync'), '')
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'system_sync' } }, 'System sync'), 'System sync')
  assert.equal(knowledgeSubmitterName({}, 'System sync'), '')
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'unknown', name: 'invented' } }, 'System sync'), '')
  assert.equal(knowledgeSubmitterName({
    submitter: { kind: 'system_sync' }, metadata: { submitter: { name: 'wrong owner' } },
  }, 'System sync'), 'System sync')
  assert.equal(knowledgeSubmitterName({
    submitter: { kind: 'unknown' }, metadata: { submitter: { name: 'wrong owner' } },
  }, 'System sync'), '')
})
