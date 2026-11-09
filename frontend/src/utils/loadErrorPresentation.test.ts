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

test('does not mislabel document verification as an insufficient workspace role', () => {
  const message = 'Document access requires verification'
  assert.equal(classifyLoadError({ status: 403, message }), 'verification')
  assert.equal(classifyLoadError({ response: { status: 403, data: { error: { message } } } }), 'verification')
  assert.equal(classifyLoadError({ status: 403, error: { message }, message: 'Forbidden' }), 'verification')
  assert.equal(classifyLoadError({ status: 403, message: 'Workspace membership required' }), 'forbidden')
  assert.equal(classifyLoadError({ status: 500, message }), 'generic')
  assert.equal(classifyLoadError({ status: 404, message }), 'notFound')
})
