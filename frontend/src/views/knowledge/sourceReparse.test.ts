import assert from 'node:assert/strict'
import test from 'node:test'
import { feishuSourceId, sourceIdFromReparseConflict, sourceReparseFailureAction } from './sourceReparse'
import { knowledgeNeedsStatusPolling, shouldRefreshWikiStatusAfterKnowledgePoll } from './wikiStatusRefresh'

const source = '8a5e2f6f-b646-4f4e-8b8e-3eb021730182'

test('explains diagnosed limits without exposing raw diagnostics as translation keys', () => {
  assert.equal(sourceReparseFailureAction('safe_staging_parser_semantic_row_too_large'),
    'knowledgeBase.parseRecoveryActions.adjust_table_chunking')
  assert.equal(sourceReparseFailureAction('encrypted_file'),
    'knowledgeBase.parseRecoveryActions.provide_unencrypted_copy')
  for (const code of [undefined, 'private diagnostic', '__proto__', 'constructor']) {
    assert.equal(sourceReparseFailureAction(code), 'knowledgeBase.sourceDocumentReparseReason')
  }
})

test('routes only persisted source bindings and never treats a personal exact import as a datasource', () => {
  const item = { id: 'document', channel: 'feishu_drive', metadata: { datasource_id: source, external_id: 'cloud' } }
  assert.equal(feishuSourceId(item), source)
  assert.equal(feishuSourceId({ ...item, channel: 'feishu_personal' }), null)
  assert.equal(feishuSourceId({ ...item, metadata: { datasource_id: 'private source string', external_id: 'cloud' } }), null)
  assert.equal(feishuSourceId({ ...item, metadata: { datasource_id: source } }), null)
  assert.equal(sourceIdFromReparseConflict({ error: { details: {
    reason: 'feishu_source_sync_required', data_source_id: source,
  } } }), source)
  assert.equal(sourceIdFromReparseConflict({ error: { details: {
    reason: 'personal_exact_recovery_required', data_source_id: source,
  } } }), null)
  assert.equal(sourceIdFromReparseConflict({ message: 'stale or unmanaged Feishu source write' }), null)
})

test('polls a controlled replacement while the old version remains completed and refreshes Wiki on completion', () => {
  for (const state of ['queued', 'processing']) {
    assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', source_document_reparse: { state } }), true)
  }
  for (const state of ['completed', 'failed']) {
    assert.equal(knowledgeNeedsStatusPolling({ parse_status: 'completed', source_document_reparse: { state } }), false)
  }
  assert.equal(shouldRefreshWikiStatusAfterKnowledgePoll(
    { parse_status: 'completed', source_document_reparse: { state: 'processing' } },
    { parse_status: 'completed', source_document_reparse: { state: 'completed' } },
  ), true)
})
