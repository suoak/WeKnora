import assert from 'node:assert/strict'
import test from 'node:test'
import { knowledgeSubmitterName } from './knowledgeSubmitter'

test('uses server attribution and retains stored names for earlier uploads', () => {
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'user', name: 'Alice' } }, 'System sync'), 'Alice')
  assert.equal(knowledgeSubmitterName({ metadata: { submitter: { name: 'Bob' } } }, 'System sync'), 'Bob')
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'system_sync' } }, 'System sync'), 'System sync')
  assert.equal(knowledgeSubmitterName({}, 'System sync'), '')
  assert.equal(knowledgeSubmitterName({ submitter: { kind: 'unknown', name: 'invented' } }, 'System sync'), '')
})
