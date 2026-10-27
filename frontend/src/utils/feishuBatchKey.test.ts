import test from 'node:test'
import assert from 'node:assert/strict'
import { webcrypto } from 'node:crypto'
import { feishuBatchKey } from './feishuBatchKey'

test('Feishu batch admission works when HTTP exposes only getRandomValues', () => {
  const httpCrypto = { getRandomValues: webcrypto.getRandomValues.bind(webcrypto) } as Pick<Crypto, 'getRandomValues'>
  assert.equal('randomUUID' in httpCrypto, false)
  const keys = new Set(Array.from({ length: 100 }, () => feishuBatchKey(httpCrypto)))
  assert.equal(keys.size, 100)
  for (const key of keys) assert.match(key, /^[a-f0-9]{32}$/)
})

test('missing cryptographic randomness fails before admission', () => {
  assert.throws(() => feishuBatchKey({} as Pick<Crypto, 'getRandomValues'>))
})
