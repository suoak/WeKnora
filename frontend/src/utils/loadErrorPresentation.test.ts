import assert from 'node:assert/strict'
import test from 'node:test'

import { classifyLoadError } from './loadErrorPresentation'

test('keeps permission and missing-resource failures distinct', () => {
  assert.equal(classifyLoadError({ response: { status: 403 } }), 'forbidden')
  assert.equal(classifyLoadError({ response: { status: 404 } }), 'notFound')
})

test('distinguishes network failures from generic server failures', () => {
  assert.equal(classifyLoadError({ code: 'ERR_NETWORK' }), 'network')
  assert.equal(classifyLoadError({ response: { status: 500 } }), 'generic')
})
