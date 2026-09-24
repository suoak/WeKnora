import assert from 'node:assert/strict'
import test from 'node:test'

import { classifyAxiosTransportError } from './requestError'

test('classifies Axios timeout errors', () => {
  assert.equal(classifyAxiosTransportError({ code: 'ECONNABORTED' }), 'timeout')
  assert.equal(classifyAxiosTransportError({ code: 'ETIMEDOUT' }), 'timeout')
})

test('classifies Axios cancellation errors', () => {
  assert.equal(classifyAxiosTransportError({ code: 'ERR_CANCELED' }), 'canceled')
})

test('classifies genuine network errors', () => {
  assert.equal(classifyAxiosTransportError({ code: 'ERR_NETWORK' }), 'network')
  assert.equal(classifyAxiosTransportError({ request: {} }), 'network')
})
