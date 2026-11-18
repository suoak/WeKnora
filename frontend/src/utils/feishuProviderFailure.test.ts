import test from 'node:test'
import assert from 'node:assert/strict'
import {feishuProviderFailure} from './feishuProviderFailure'

test('show the actual provider message and code with the failed stage', () => {
  assert.equal(feishuProviderFailure({provider_code: 1069902, provider_message: 'no permission', operation: 'create_export_task'}),
    '导出失败。飞书返回：“no permission”（错误码：1069902）。')
})

test('historical code-only evidence does not invent an original message or cause', () => {
  assert.equal(feishuProviderFailure({provider_code: 1069902, operation: 'create_export_task'}),
    '导出失败。飞书返回错误码：1069902。')
  assert.equal(feishuProviderFailure({provider_code: 1069902}), '飞书请求失败。飞书返回错误码：1069902。')
})

test('without a valid provider code, keep the local failure fallback', () => {
  for (const provider_code of [undefined, 0, -1, 1.2, NaN, Infinity, 1000000000]) {
    assert.equal(feishuProviderFailure({provider_code, provider_message: 'no permission'}), '')
  }
})
