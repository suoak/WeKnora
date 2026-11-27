import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { compileScript, parse } from '@vue/compiler-sfc'
import * as vue from 'vue'

const source = readFileSync(new URL('./KnowledgeTagPicker.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const compiled = ts.transpileModule(compileScript(descriptor, { id: 'picker-test', inlineTemplate: true }).content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const translate = (key: string) => key
type Host = { type: string; props: Record<string, any>; children: Host[]; text: string; parent?: Host }
const node = (type: string): Host => ({ type, props: {}, children: [], text: '' })
const renderer = vue.createRenderer<Host, Host>({
  createElement: node,
  createText: text => ({ ...node('#text'), text }),
  createComment: text => ({ ...node('#comment'), text }),
  patchProp: (el, key, _old, value) => { el.props[key] = value },
  setText: (el, text) => { el.text = text },
  setElementText: (el, text) => { el.children = []; el.text = text },
  parentNode: el => el.parent || null,
  nextSibling: el => el.parent?.children[el.parent.children.indexOf(el) + 1] || null,
  insert(el, parent, anchor) {
    if (el.parent) el.parent.children.splice(el.parent.children.indexOf(el), 1)
    el.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, el)
  },
  remove(el) {
    el.parent?.children.splice(el.parent.children.indexOf(el), 1)
    el.parent = undefined
  },
})
const textOf = (el: Host): string => el.type === '#comment' ? '' : el.text + el.children.map(textOf).join('')
const all = (el: Host, predicate: (node: Host) => boolean): Host[] => [
  ...(predicate(el) ? [el] : []), ...el.children.flatMap(child => all(child, predicate)),
]
const hasClass = (el: Host, name: string) => String(el.props.class || '').split(' ').includes(name)


async function fixture(options: { selectCreateOnly?: boolean; allowCreate?: boolean; empty?: boolean; conflict?: boolean; pagedConflict?: boolean; create?: (...args: any[]) => Promise<any>; list?: () => void } = {}) {
  const calls: { create: unknown[][]; rename: unknown[][]; remove: unknown[][] } = { create: [], rename: [], remove: [] }
  let rows = [{ id: 'used', seq_id: 1, name: 'Used', knowledge_count: 2 }, { id: 'free', seq_id: 2, name: 'Free', knowledge_count: 0 }]
  if (options.empty) rows = []
  const exports: { default?: vue.Component } = {}
  runInNewContext(compiled, { exports, setTimeout, clearTimeout, require(name: string) {
    if (name === 'vue') return vue
    if (name === 'vue-i18n') return { useI18n: () => ({ t: translate }) }
    if (name === 'tdesign-vue-next') return { MessagePlugin: { success() {}, error() {}, warning() {} } }
    if (name === '@/api/knowledge-base') return {
      listKnowledgeTags: async (_kb: string, params: { keyword?: string; page?: number }) => {
        options.list?.()
        const keyword = params.keyword
        if (options.pagedConflict && keyword && calls.create.length) {
          const matches = params.page === 1 ? Array.from({ length: 50 }, (_, index) => ({ id: 'similar-' + index, name: keyword + index })) : rows.filter(row => row.name === keyword)
          return { data: { data: matches, total: 51 } }
        }
        const filtered = rows.filter(r => !params.keyword || r.name.includes(params.keyword))
        return { data: { data: filtered.map(r => ({ ...r })), total: filtered.length } }
      },
      createKnowledgeBaseTag: async (...args: any[]) => {
        calls.create.push(args)
        if (options.create) return options.create(...args)
        const tag = { id: 'new', seq_id: 3, name: args[1].name, knowledge_count: 0 }
        rows.push(tag)
        if (options.conflict) throw { status: 409 }
        return { data: tag }
      },
      updateKnowledgeBaseTag: async (...args: any[]) => { calls.rename.push(args); rows.find(r => r.id === args[1])!.name = args[2].name },
      deleteKnowledgeBaseTag: async (...args: any[]) => { calls.remove.push(args); rows = rows.filter(r => r.seq_id !== args[1]) },
    }
    throw new Error('Unexpected import: ' + name)
  } })
  const selectedIds = vue.ref<string[]>([])
  const changes: unknown[] = []
  const root = node('root')
  const app = renderer.createApp({ setup: () => () => vue.h(exports.default!, {
    kbId: 'kb', selectedIds: selectedIds.value, selectCreateOnly: options.selectCreateOnly, allowCreate: options.allowCreate,
    'onUpdate:selectedIds': (ids: string[]) => { selectedIds.value = ids },
    onChanged: (payload: unknown) => changes.push(payload),
  }) })
  app.config.globalProperties.$t = translate as typeof app.config.globalProperties.$t
  for (const [name, type] of [['t-input', 'input'], ['t-button', 'button'], ['t-checkbox', 'checkbox'], ['t-popconfirm', 'popconfirm'], ['t-tooltip', 'tooltip'], ['t-icon', 'icon'], ['t-loading', 'loading']]) {
    app.component(name!, vue.defineComponent({ setup: (_props, { slots }) => () => vue.h(type!, {}, slots.default?.()) }))
  }
  app.component('t-popup', vue.defineComponent({
    props: ['visible'], emits: ['visible-change'],
    setup: (props, { slots, emit }) => () => vue.h('popup', { onOpen: () => emit('visible-change', true) }, [slots.default?.(), props.visible ? slots.content?.() : null]),
  }))
  app.mount(root)
  const settle = async () => { for (let i = 0; i < 6; i++) await vue.nextTick() }
  await settle()
  const find = (predicate: (el: Host) => boolean) => { const el = all(root, predicate)[0]; assert.ok(el); return el }
  const fire = async (el: Host, event = 'onClick', value?: unknown) => { await el.props[event](value); await settle() }
  const search = async (value: string) => {
    await fire(find(el => el.type === 'input'), 'onUpdate:modelValue', value)
    await new Promise(resolve => setTimeout(resolve, 270))
    await settle()
  }
  return { root, calls, selectedIds, changes, find, fire, search, close: () => app.unmount() }
}

test('search, create and select happen in one panel without losing previous selection', async t => {
  const f = await fixture(); t.after(f.close)
  await f.fire(f.find(el => el.type === 'checkbox'), 'onChange', true)
  assert.deepEqual([...f.selectedIds.value], ['used'])
  await f.search('New')
  await f.fire(f.find(el => hasClass(el, 'tag-picker-create')))
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls.create)), [['kb', { name: 'New' }]])
  assert.deepEqual([...f.selectedIds.value], ['used', 'new'])
  assert.equal(f.changes.length, 1)
})

test('rename saves in place without changing selection', async t => {
  const f = await fixture(); t.after(f.close)
  f.selectedIds.value = ['used']; await vue.nextTick()
  await f.fire(f.find(el => el.type === 'popup'), 'onOpen')
  await f.fire(f.find(el => el.type === 'button' && textOf(el) === 'knowledgeBase.tagEditAction'))
  const input = f.find(el => el.type === 'input' && el.props['aria-label'] === 'knowledgeBase.tagEditAction')
  await f.fire(input, 'onUpdate:modelValue', 'Renamed')
  await f.fire(f.find(el => el.type === 'button' && el.props['aria-label'] === 'common.save'))
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls.rename)), [['kb', 'used', { name: 'Renamed' }]])
  assert.deepEqual([...f.selectedIds.value], ['used'])
  assert.ok(textOf(f.root).includes('Renamed'))
})

test('selected tags move above unselected tags and search preserves selection', async t => {
  const f = await fixture(); t.after(f.close)
  const headings = () => all(f.root, el => hasClass(el, 'tag-picker-group-title')).map(textOf)
  const labels = () => all(f.root, el => el.type === 'checkbox').map(textOf)
  assert.deepEqual(headings(), ['knowledgeBase.tagPickerUnselected2'])
  await f.fire(f.find(el => el.type === 'checkbox' && textOf(el) === 'Free'), 'onChange', true)
  assert.deepEqual(headings(), ['knowledgeBase.tagPickerSelected1', 'knowledgeBase.tagPickerUnselected1'])
  assert.deepEqual(labels(), ['Free', 'Used'])
  await f.search('Used')
  assert.deepEqual(headings(), ['knowledgeBase.tagPickerUnselected1'])
  assert.deepEqual([...f.selectedIds.value], ['free'])
  await f.search('')
  assert.deepEqual(labels(), ['Free', 'Used'])
  await f.fire(f.find(el => el.type === 'checkbox' && textOf(el) === 'Used'), 'onChange', true)
  assert.deepEqual(headings(), ['knowledgeBase.tagPickerSelected2'])
  await f.fire(f.find(el => el.type === 'checkbox' && textOf(el) === 'Free'), 'onChange', false)
  assert.deepEqual(headings(), ['knowledgeBase.tagPickerSelected1', 'knowledgeBase.tagPickerUnselected1'])
  assert.deepEqual(labels(), ['Used', 'Free'])
})

test('used tags cannot be deleted; unused deletion never requests cascading content removal', async t => {
  const f = await fixture(); t.after(f.close)
  await f.fire(f.find(el => el.type === 'popup'), 'onOpen')
  const deleteButtons = all(f.root, el => el.type === 'button' && textOf(el) === 'knowledgeBase.tagDeleteAction')
  assert.ok('disabled' in deleteButtons[0]!.props && deleteButtons[0]!.props.disabled !== false)
  assert.equal(all(f.root, el => el.type === 'popconfirm').length, 0)
  await f.fire(all(f.root, el => el.type === 'popup')[1]!, 'onOpen')
  assert.equal(all(f.root, el => el.type === 'popconfirm').length, 1)
  f.selectedIds.value = ['used', 'free']; await vue.nextTick()
  await f.fire(f.find(el => el.type === 'popconfirm'), 'onConfirm')
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls.remove)), [['kb', 2]])
  assert.deepEqual([...f.selectedIds.value], ['used'])
  assert.equal(all(f.root, el => hasClass(el, 'tag-picker-row')).length, 1)
})

test('upload mode exposes create and multi-selection but no management actions', async t => {
  const f = await fixture({ selectCreateOnly: true }); t.after(f.close)
  assert.equal(all(f.root, el => el.type === 'popup').length, 0)
  assert.equal(all(f.root, el => el.type === 'button' && textOf(el) === 'knowledgeBase.tagCreateAction').length, 0)
  await f.search(' First ')
  assert.equal(all(f.root, el => hasClass(el, 'tag-picker-create')).length, 1)
  await f.fire(f.find(el => el.type === 'input'), 'onEnter')
  assert.deepEqual(JSON.parse(JSON.stringify(f.calls.create)), [['kb', { name: 'First' }]])
  assert.deepEqual([...f.selectedIds.value], ['new'])
  assert.ok(textOf(f.root).includes('First'))
})

test('empty, failed loading, and denied creation have distinct states', async t => {
  const empty = await fixture({ selectCreateOnly: true, empty: true }); t.after(empty.close)
  assert.ok(textOf(empty.root).includes('uploadConfirm.tagsCreateHint'))
  await empty.search('First')
  assert.ok(textOf(empty.root).includes('knowledgeBase.tagEmptyResult'))
  const failed = await fixture({ list: () => { throw new Error('offline') } }); t.after(failed.close)
  assert.ok(textOf(failed.root).includes('uploadConfirm.tagsLoadFailed'))
  assert.ok(!textOf(failed.root).includes('knowledgeBase.noTags'))
  const denied = await fixture({ selectCreateOnly: true, allowCreate: false }); t.after(denied.close)
  await denied.search('New')
  assert.ok(textOf(denied.root).includes('uploadConfirm.tagsCreateForbidden'))
  assert.equal(all(denied.root, el => hasClass(el, 'tag-picker-create')).length, 0)
})

test('conflicting name is queried and reused without dropping prior selection', async t => {
  const f = await fixture({ conflict: true }); t.after(f.close)
  f.selectedIds.value = ['free']; await vue.nextTick()
  await f.search('Raced')
  await f.fire(f.find(el => hasClass(el, 'tag-picker-create')))
  assert.deepEqual([...f.selectedIds.value], ['free', 'new'])
  assert.equal(f.calls.create.length, 1)
})

test('conflict recovery finds an exact name beyond the first search page', async t => {
  const f = await fixture({ conflict: true, pagedConflict: true }); t.after(f.close)
  await f.search('Common')
  await f.fire(f.find(el => hasClass(el, 'tag-picker-create')))
  assert.deepEqual([...f.selectedIds.value], ['new'])
  assert.equal(f.calls.create.length, 1)
})

test('failed and duplicate in-flight creates preserve the query and selection', async t => {
  let reject!: (value: unknown) => void
  const f = await fixture({ create: () => new Promise((_resolve, rejectCreate) => { reject = rejectCreate }) }); t.after(f.close)
  f.selectedIds.value = ['used']; await vue.nextTick()
  await f.search('Retry me')
  const button = f.find(el => hasClass(el, 'tag-picker-create'))
  const first = button.props.onClick()
  await button.props.onClick()
  assert.equal(f.calls.create.length, 1)
  reject(new Error('timeout')); await first; await vue.nextTick()
  assert.equal(f.find(el => el.type === 'input').props.modelValue, 'Retry me')
  assert.deepEqual([...f.selectedIds.value], ['used'])
})

test('a late creation result after closing does not select into the next upload draft', async () => {
  let resolve!: (value: unknown) => void
  const f = await fixture({ create: () => new Promise(done => { resolve = done }) })
  await f.search('Late tag')
  const pending = f.find(el => hasClass(el, 'tag-picker-create')).props.onClick()
  f.close()
  resolve({ data: { id: 'late', name: 'Late tag' } }); await pending
  assert.deepEqual([...f.selectedIds.value], [])
})

test('HTTP 403 retains the name and exposes denied creation instead of an empty catalog', async t => {
  const f = await fixture({ selectCreateOnly: true, create: async () => { throw { status: 403 } } }); t.after(f.close)
  await f.search('Denied tag')
  await f.fire(f.find(el => hasClass(el, 'tag-picker-create')))
  assert.equal(f.find(el => el.type === 'input').props.modelValue, 'Denied tag')
  assert.ok(textOf(f.root).includes('uploadConfirm.tagsCreateForbidden'))
  assert.equal(all(f.root, el => hasClass(el, 'tag-picker-create')).length, 0)
})
