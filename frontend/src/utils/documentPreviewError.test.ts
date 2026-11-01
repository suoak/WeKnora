import assert from 'node:assert/strict';
import test from 'node:test';
import { documentPreviewErrorKey } from './documentPreviewError.ts';

test('preview distinguishes transport timeout, denied access, missing original and generic failure', () => {
  assert.equal(documentPreviewErrorKey({ kind: 'timeout' }), 'error.requestTimeout');
  assert.equal(documentPreviewErrorKey({ code: 'ECONNABORTED' }), 'error.requestTimeout');
  assert.equal(documentPreviewErrorKey({ status: 403 }), 'knowledgeBase.documentReadForbidden');
  assert.equal(documentPreviewErrorKey({ response: { status: 401 } }), 'knowledgeBase.documentReadForbidden');
  assert.equal(documentPreviewErrorKey({ status: 404 }), 'loadState.notFoundDescription');
  assert.equal(documentPreviewErrorKey({ status: 410 }), 'loadState.notFoundDescription');
  assert.equal(documentPreviewErrorKey({ status: 500, message: 'private provider details' }), 'preview.loadFailed');
  assert.equal(documentPreviewErrorKey(null), 'preview.loadFailed');
});
