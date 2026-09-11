import assert from 'node:assert/strict'
import test from 'node:test'

import {
  conciseProcessingError,
  presentKnowledgeProcessingState,
  summarizeKnowledgeProcessing,
} from './knowledgeProcessingPresentation'

test('maps internal processing statuses to stable user-facing states', () => {
  assert.equal(presentKnowledgeProcessingState('pending'), 'waiting')
  assert.equal(presentKnowledgeProcessingState('processing'), 'processing')
  assert.equal(presentKnowledgeProcessingState('finalizing'), 'finalizing')
  assert.equal(presentKnowledgeProcessingState('completed'), 'completed')
  assert.equal(presentKnowledgeProcessingState('failed'), 'failed')
  assert.equal(presentKnowledgeProcessingState('cancelled'), 'cancelled')
  assert.equal(presentKnowledgeProcessingState('backend_new_state'), 'unknown')
})

test('summarizes a multi-file processing batch without counting terminal states as active', () => {
  assert.deepEqual(summarizeKnowledgeProcessing([
    { parse_status: 'completed' },
    { parse_status: 'completed' },
    { parse_status: 'pending' },
    { parse_status: 'processing' },
    { parse_status: 'finalizing' },
    { parse_status: 'failed' },
    { parse_status: 'cancelled' },
  ]), {
    total: 7,
    completed: 2,
    processing: 3,
    failed: 1,
    cancelled: 1,
  })
})

test('keeps only a concise first-line processing error for summary surfaces', () => {
  assert.equal(conciseProcessingError('error: parser timed out\nstack trace'), 'parser timed out')
  assert.equal(conciseProcessingError(''), '')
  assert.equal(conciseProcessingError('123456789', 6), '12345…')
})
