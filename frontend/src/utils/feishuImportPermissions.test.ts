import test from 'node:test'
import assert from 'node:assert/strict'
import {originalDocumentURL} from './feishuImportPermissions'

test('failed import source links keep the document and discard unrelated parameters', () => {
  assert.equal(originalDocumentURL('https://ruijie.feishu.cn/docx/Test123?from=share#block'),
    'https://ruijie.feishu.cn/docx/Test123')
  assert.equal(originalDocumentURL('https://ruijie.feishu.cn/wiki/Node123'),
    'https://ruijie.feishu.cn/wiki/Node123')
})

test('source links reject executable, misleading and unrelated destinations', () => {
  for (const value of ['javascript:alert(1)', '//ruijie.feishu.cn/docx/Test123',
    'http://ruijie.feishu.cn/docx/Test123', 'https://feishu.cn.evil.test/docx/Test123',
    'https://ruijie.feishu.cn@evil.test/docx/Test123', 'https://user:secret@ruijie.feishu.cn/docx/Test123',
    'https://ruijie.feishu.cn:8443/docx/Test123', 'https://ruijie.feishu.cn/accounts/login', '']) {
    assert.equal(originalDocumentURL(value), '', value)
  }
})
