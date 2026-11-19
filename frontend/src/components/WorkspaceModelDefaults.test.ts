import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createRenderer, h, nextTick, reactive } from 'vue'
import { selectInitialModelId } from '../utils/modelDefaults'

const require = createRequire(import.meta.url)
const filename = fileURLToPath(new URL('./WorkspaceModelDefaults.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const script = compileScript(descriptor, { id: 'workspace-defaults-test' }).content
  .replace('__expose();', '').replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const empty = () => ({ chat_model_id: '', utility_model_id: '', embedding_model_id: '', rerank_model_id: '', vlm_model_id: '', asr_model_id: '' })
const settle = async () => { await nextTick(); await nextTick(); await nextTick() }

function fixture(load: () => Promise<any>, admin = true) {
  const exports: any = {}, saves: any[] = []
  const auth = reactive({ currentTenantId: 7, isSystemAdmin: false, hasRole: () => admin })
  runInNewContext(compiled, {
    exports, console,
    require(name: string) {
      if (name === 'vue') return require('vue')
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      if (name === '@/stores/auth') return { useAuthStore: () => auth }
      if (name === 'tdesign-vue-next') return { MessagePlugin: { success() {}, error() {} } }
      if (name === '@/api/model/workspaceDefaults') return {
        emptyWorkspaceDefaults: empty, getWorkspaceModelDefaults: load,
        saveWorkspaceModelDefaults: async (settings: any, previous: any) => { saves.push({ settings, previous }); return { data: settings } },
      }
      throw new Error(`unexpected import: ${name}`)
    },
  })
  exports.default.render = () => null
  const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {}, parentNode: () => null, nextSibling: () => null })
  let vm: any
  const app = renderer.createApp({ setup: () => () => h(exports.default, { models: [], ref: (v: any) => { vm = v } }) })
  app.mount({})
  return { vm, auth, saves, close: () => app.unmount() }
}

test('pending and failed loads cannot overwrite saved workspace defaults', async () => {
  let reject!: (e: Error) => void
  const f = fixture(() => new Promise((_, no) => { reject = no }))
  try {
    await f.vm.save()
    assert.equal(f.saves.length, 0)
    reject(new Error('offline')); await settle()
    assert.equal(f.vm.failed, true)
    await f.vm.save()
    assert.equal(f.saves.length, 0)
  } finally { f.close() }
})

test('read-only members cannot save, and admins send the loaded previous snapshot', async () => {
  for (const admin of [false, true]) {
    const saved = { ...empty(), chat_model_id: 'old-explicit-model' }
    const f = fixture(async () => ({ data: saved }), admin)
    try {
      await settle()
      f.vm.draft.chat_model_id = 'new-model'
      await f.vm.save()
      assert.equal(f.saves.length, admin ? 1 : 0)
      if (admin) {
        assert.equal(f.saves[0].previous.chat_model_id, 'old-explicit-model')
        assert.equal(f.saves[0].settings.chat_model_id, 'new-model')
        assert.equal(saved.chat_model_id, 'old-explicit-model')
      }
    } finally { f.close() }
  }
})

test('a late response from another workspace never replaces current defaults', async () => {
  let resolveOld!: (v: any) => void
  let calls = 0
  const f = fixture(() => ++calls === 1 ? new Promise(yes => { resolveOld = yes }) : Promise.resolve({ data: { ...empty(), chat_model_id: 'workspace-eight' } }))
  try {
    f.auth.currentTenantId = 8
    await settle()
    resolveOld({ data: { ...empty(), chat_model_id: 'workspace-seven' } })
    await settle()
    assert.equal(f.vm.draft.chat_model_id, 'workspace-eight')
  } finally { f.close() }
})

test('purpose defaults keep chat and vision independent and skip inactive models', () => {
  const models = [
    { id: 'chat', type: 'KnowledgeQA', status: 'active', is_default: true },
    { id: 'vision', type: 'KnowledgeQA', status: 'active', parameters: { supports_vision: true }, default_for: ['VLLM'] },
    { id: 'inactive', type: 'ASR', status: 'downloading', is_default: true },
    { id: 'speech', type: 'ASR', status: 'active' },
  ]
  assert.equal(selectInitialModelId(models, 'KnowledgeQA'), 'chat')
  assert.equal(selectInitialModelId(models, 'VLLM'), 'vision')
  assert.equal(selectInitialModelId(models, 'ASR'), 'speech')
})

test('localized default settings remain readable after file generation', async () => {
  for (const locale of ['zh-CN', 'en-US', 'ja-JP', 'ko-KR', 'ru-RU']) {
    const { default: messages } = await import(`../i18n/locales/${locale}.ts`)
    for (const text of Object.values(messages.workspaceDefaults)) {
      assert.equal(typeof text, 'string')
      assert.ok(!/\?{2,}/.test(text as string), `${locale}: localization was corrupted`)
    }
  }
})
