import assert from 'node:assert/strict'
import test from 'node:test'
import { isSourceIDProvider } from './oidcLoginPresentation'

test('only the explicit SourceID API marker selects the OA presentation', () => {
  for (const encoding of ['oauth_form', 'raw']) {
    assert.equal(isSourceIDProvider({ sourceid_basic_encoding: encoding }), true)
  }
  for (const config of [{}, { sourceid_basic_encoding: '' }, { sourceid_basic_encoding: 'unknown' }]) {
    assert.equal(isSourceIDProvider(config), false)
  }
})
