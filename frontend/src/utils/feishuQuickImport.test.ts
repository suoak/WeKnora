import test from 'node:test'
import assert from 'node:assert/strict'
import { normalizeFeishuLinks } from './feishuQuickImport'

test('ten mixed links normalize share parameters and ignore empty lines independently', () => {
  const links = ['docx','sheets','base','wiki'].flatMap(type => [
    `https://team.feishu.cn/${type}/token_${type}?from=share#fragment`,
    `https://team.feishu.cn/${type}/token_${type}`,
  ])
  const result = normalizeFeishuLinks([...links, 'https://team.feishu.cn/docx/extra', 'invalid', '', '  '].join('\r\n'))
  assert.equal(result.valid.length, 5)
  assert.deepEqual(result.invalid, ['invalid'])
  assert.ok(result.valid.every(url => !url.includes('?') && !url.includes('#')))
})

test('UI refuses untrusted origins and wider folder or space scope', () => {
  const rejected = ['http://team.feishu.cn/docx/id','https://feishu.cn.evil.test/docx/id',
    'https://evilfeishu.cn/docx/id','https://user:pass@team.feishu.cn/docx/id',
    'https://team.feishu.cn:8080/docx/id','https://team.feishu.cn/wiki/space/id',
    'https://team.feishu.cn/drive/folder/id','https://127.0.0.1/docx/id']
  const result = normalizeFeishuLinks(rejected.join('\n'))
  assert.equal(result.valid.length, 0)
  assert.equal(result.invalid.length, rejected.length)
})
