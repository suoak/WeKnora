import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createSSRApp } from 'vue'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { isEncryptedParseFailure } from '../utils/knowledgeParseFailure.ts'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./knowledge-parse-failure-notice.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const script = compileScript(descriptor, { id: 'failure-notice', inlineTemplate: true })
const code = ts.transpileModule(script.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
new Function('require', 'module', 'exports', code)(
  (name) => name === '@/utils/knowledgeParseFailure' ? { isEncryptedParseFailure } : require(name),
  module, module.exports,
)

test('renders the encryption reason and corrective action without opening a tooltip or trace', async () => {
  const item = { parse_status: 'failed', metadata: { parse_failure_v1: {
    code: 'encrypted_file', state: 'needs_attention', action: 'provide_unencrypted_copy',
    auto_retry: false, attempts: 1, max_attempts: 4,
  } } }
  const app = createSSRApp(module.exports.default, { item })
  app.config.globalProperties.$t = (key) => ({
    'knowledgeBase.encryptedDocumentReason': '文档已加密，无法解析',
    'knowledgeBase.encryptedDocumentAction': '请先取消文件加密，再上传未加密版本。系统已停止自动重试。',
  })[key]
  const html = await renderToString(app)
  assert.ok(html.includes('文档已加密，无法解析'))
  assert.ok(html.includes('请先取消文件加密，再上传未加密版本'))
  assert.ok(html.includes('role="note"'))
  const normal = createSSRApp(module.exports.default, { item: { ...item, parse_status: 'completed' } })
  assert.equal((await renderToString(normal)).replace(/<!--.*?-->/g, ''), '')
})
