import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import ts from 'typescript'
import * as vue from 'vue'
import { classifyLoadError } from '../utils/loadErrorPresentation.ts'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./useKnowledgeBase.ts', import.meta.url), 'utf8')
const code = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function fixture() {
  const requests = []
  const deferred = (kind, id, page) => new Promise((resolve, reject) => {
    requests.push({ kind, id, page, resolve, reject })
  })
  const store = { cardList: vue.ref([]), total: vue.ref(0) }
  const modules = {
    vue,
    pinia: { storeToRefs: (value) => value },
    '@/utils/loadErrorPresentation': { classifyLoadError },
    '../utils/index': { formatStringDate: () => 'date' },
    '@/api/knowledge-base/index': {
      listKnowledgeFiles: (id, query) => deferred('list', id, query.page),
      getKnowledgeDetails: (id) => deferred('detail', id),
      getKnowledgeDetailsCon: (id, page) => deferred('chunk', id, page),
    },
    '@/stores/knowledge': { knowledgeStore: () => store },
    'vue-router': { useRoute: () => ({ params: {} }) },
    'vue-i18n': { useI18n: () => ({ t: (key) => key }) },
  }
  const module = { exports: {} }
  new Function('require', 'exports', 'module', code)(
    (name) => modules[name] ?? require(name), module.exports, module,
  )
  return { hook: module.exports.default(), requests }
}
const settle = () => new Promise((resolve) => setImmediate(resolve))

test('an unfinished import is shown as processing, without an authorization warning', async () => {
  const { hook, requests } = fixture()
  hook.getCardDetails({ id: 'unfinished', file_name: 'import.docx' })
  const error = { status: 403, error: { details: { reason: 'personal_published_snapshot_unverified' } } }
  requests[0].reject(error)
  requests[1].reject(error)
  await settle()
  assert.equal(hook.details.detailLoadError, 'loadState.notReadyDescription')
  assert.equal(hook.details.chunkLoadError, 'loadState.notReadyDescription')
  assert.equal(hook.details.chunkLoaded, false)
})

test('HTTP errors stay errors, initial load is loading, successful empty data is distinct', async () => {
  const { hook, requests } = fixture()
  hook.getCardDetails({ id: 'a', file_name: 'original.docx' })
  assert.equal(hook.details.title, 'original.docx')
  assert.equal(hook.details.chunkLoading, true)
  assert.equal(hook.details.chunkLoaded, false)
  requests[0].reject({ status: 500, message: 'sensitive provider error' })
  requests[1].reject({ status: 403 })
  await settle()
  assert.equal(hook.details.detailLoadError, 'knowledgeBase.chunkLoadFailed')
  assert.equal(hook.details.chunkLoadError, 'knowledgeBase.documentReadForbidden')
  assert.equal(hook.details.chunkLoading, false)
  assert.equal(hook.details.chunkLoaded, false)
  hook.getCardDetails({ id: 'b' })
  requests[2].resolve({ success: true, data: { id: 'b', title: 'B' } })
  requests[3].resolve({ success: true, data: [], total: 0 })
  await settle()
  assert.equal(hook.details.chunkLoaded, true)
  assert.equal(hook.details.chunkLoadError, '')
  assert.equal(hook.details.detailLoadError, '')
  assert.equal(hook.details.total, 0)
})

test('late detail and chunk responses cannot replace another document or page', async () => {
  const { hook, requests } = fixture()
  hook.getCardDetails({ id: 'a' })
  hook.getCardDetails({ id: 'b' })
  requests[2].resolve({ success: true, data: { id: 'b', title: 'B' } })
  requests[3].resolve({ success: true, data: [{ id: 'b1', content: 'body B' }], total: 62 })
  await settle()
  requests[0].resolve({ success: true, data: { id: 'a', title: 'A' } })
  requests[1].reject({ status: 500 })
  await settle()
  assert.equal(hook.details.id, 'b')
  assert.equal(hook.details.md[0].id, 'b1')
  assert.equal(hook.details.chunkLoadError, '')
  hook.getfDetails('b', 2)
  hook.getfDetails('b', 3)
  requests[5].resolve({ success: true, data: [{ id: 'b3' }], total: 62 })
  requests[4].resolve({ success: true, data: [{ id: 'b2' }], total: 62 })
  await settle()
  assert.equal(hook.details.md[0].id, 'b3')
  assert.equal(hook.details.total, 62)
})


test('catalog failures stay distinct from empty success and obsolete failures cannot replace a new folder', async () => {
  const { hook, requests } = fixture()
  const load = () => hook.getKnowled({ page: 1, page_size: 35, folder_path: '拜访纪要' }, 'kb')
  let pending = load()
  requests[0].reject({ status: 403 })
  await pending
  assert.equal(hook.knowledgeListError.value, 'forbidden')
  pending = load()
  requests[1].resolve({ success: true, data: [], total: 0 })
  await pending
  assert.equal(hook.knowledgeListError.value, null)
  assert.equal(hook.total.value, 0)
  const obsolete = load()
  const current = load()
  requests[3].resolve({ success: true, data: Array.from({ length: 24 }, (_, id) => ({ id, file_name: '纪要.docx', updated_at: '2026-11-09' })), total: 24 })
  await current
  requests[2].reject({ status: 500 })
  await obsolete
  assert.equal(hook.knowledgeListError.value, null)
  assert.equal(hook.cardList.value.length, 24)
  pending = load()
  requests[4].resolve({ success: false })
  await pending
  assert.equal(hook.knowledgeListError.value, 'generic')
  assert.equal(hook.cardList.value.length, 24)
})
