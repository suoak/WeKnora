import test from 'node:test'
import assert from 'node:assert/strict'
import {feishuImportNeedsReconnect,feishuImportRecoveryHint} from './feishuImportRecovery'
import type {FeishuBatchItem} from '@/api/feishuPersonal'

const item = (extra: Partial<FeishuBatchItem> = {}): FeishuBatchItem => ({
  id: 'item', ordinal: 0, url: 'https://tenant.feishu.cn/docx/example', status: 'failed', retry_count: 0, ...extra,
})

test('expired or rejected refresh authorization has an executable reconnect action', () => {
  for (const error_category of ['authorization_expired', 'reconnect_required', 'authorization_invalid', 'refresh_token_expired', 'refresh_rejected', 'user_scope_missing']) {
    assert.equal(feishuImportNeedsReconnect(item({error_category})), true)
  }
  assert.equal(feishuImportNeedsReconnect(item({user_action: 'connect_feishu'})), true)
  assert.equal(feishuImportNeedsReconnect(item({error_category: 'source_read_denied'})), false)
})

test('only server-approved retained executions offer continuation', () => {
  const error_category = 'exact_document_execution_requires_review'
  assert.match(feishuImportRecoveryHint(item({error_category, can_retry: true})), /\u70b9\u51fb\u91cd\u65b0\u5c1d\u8bd5/)
  assert.match(feishuImportRecoveryHint(item({error_category, can_retry: false})), /\u7ba1\u7406\u5458/)
  assert.doesNotMatch(feishuImportRecoveryHint(item({error_category})), /\u70b9\u51fb\u91cd\u65b0\u5c1d\u8bd5/)
})

test('terminal parser and index errors never promise an unavailable retry', () => {
  for (const error_category of ['parse_failed', 'index_failed', 'retry_exhausted']) {
    assert.match(feishuImportRecoveryHint(item({error_category, can_retry: true})), /\u70b9\u51fb\u91cd\u65b0\u5c1d\u8bd5/)
    assert.doesNotMatch(feishuImportRecoveryHint(item({error_category, can_retry: false})), /\u70b9\u51fb\u91cd\u65b0\u5c1d\u8bd5/)
  }
  assert.equal(feishuImportRecoveryHint(item({error_category: 'source_copy_denied'})), '')
})
