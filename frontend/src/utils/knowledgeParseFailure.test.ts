import assert from 'node:assert/strict'
import test from 'node:test'
import { isEncryptedParseFailure, parseFailureProgress } from './knowledgeParseFailure'

test('shows retry progress only for an admitted current processing task', () => {
  const failure = {
    code: 'DOCREADER_TIMEOUT', state: 'retry_scheduled', action: 'wait_for_automatic_retry',
    attempts: 1, max_attempts: 4, auto_retry: true,
  }
  assert.equal(parseFailureProgress({ parse_status: 'processing', metadata: { parse_failure_v1: failure } })?.attempts, 1)
  for (const state of ['failed', 'completed', 'cancelled', 'pending']) {
    assert.equal(parseFailureProgress({ parse_status: state, metadata: { parse_failure_v1: failure } }), null)
  }
  assert.equal(parseFailureProgress({ parse_status: 'processing', metadata: {
    parse_failure_v1: { ...failure, attempts: 4 },
  } }), null)
})

test('preserves manual diagnosis and rejects untrusted action keys', () => {
  const failure = {
    code: 'encrypted_file', state: 'needs_attention', action: 'provide_unencrypted_copy',
    attempts: 1, max_attempts: 4, auto_retry: false,
  }
  assert.equal(parseFailureProgress({ parse_status: 'failed', metadata: { parse_failure_v1: failure } })?.action,
    'provide_unencrypted_copy')
  for (const change of [{ action: 'private raw error' }, { attempts: -1 }, { auto_retry: true }, { max_attempts: 100000 }]) {
    assert.equal(parseFailureProgress({ parse_status: 'failed', metadata: { parse_failure_v1: { ...failure, ...change } } }), null)
  }
})

test('encrypted notice requires current structured diagnosis and a stopped retry policy', () => {
  const failure = {
    code: 'encrypted_file', state: 'needs_attention', action: 'provide_unencrypted_copy',
    attempts: 1, max_attempts: 4, auto_retry: false,
  }
  const item = { parse_status: 'failed', metadata: { parse_failure_v1: failure } }
  assert.equal(isEncryptedParseFailure(item), true)
  for (const parse_status of ['pending', 'processing', 'completed', 'cancelled']) {
    assert.equal(isEncryptedParseFailure({ ...item, parse_status }), false)
  }
  for (const change of [{ code: 'unknown' }, { auto_retry: true }, { action: 'review_processing_trace' }]) {
    assert.equal(isEncryptedParseFailure({ ...item, metadata: { parse_failure_v1: { ...failure, ...change } } }), false)
  }
  assert.equal(isEncryptedParseFailure({ parse_status: 'failed' }), false)
})
