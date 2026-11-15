import assert from 'node:assert/strict'
import test from 'node:test'
import { parseFailureProgress } from './knowledgeParseFailure'

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
