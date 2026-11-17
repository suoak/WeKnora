import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createRenderer, h, nextTick, reactive, ref } from 'vue'

const require = createRequire(import.meta.url)
const filename = fileURLToPath(new URL('./DataSourceEditorDialog.vue', import.meta.url))
const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
const script = compileScript(descriptor, { id: 'datasource-editor-test' }).content
  .replace('__expose();', '')
  .replace('return __returned__', '__expose(__returned__); return __returned__')
const compiled = ts.transpileModule(script, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

async function fixture({
  configured = true,
  create = false,
  type = 'gitlab',
  settings = { projects: [{ project_id: '123', paths: [] }] },
  role = 'admin',
}: {
  configured?: boolean
  create?: boolean
  type?: string
  settings?: Record<string, unknown>
  role?: 'admin' | 'contributor' | 'viewer'
} = {}) {
  const calls: Array<{ method: string; args: any[] }> = []
  let storedToken = configured ? 'expired-token' : ''
  const api = {
    async createDataSource(data: any) {
      calls.push({ method: 'createDataSource', args: [JSON.parse(JSON.stringify(data))] })
      return { data: { id: 'new-source' } }
    },
    async getConnectorTypes() { return [] },
    async listResourcePage(id: string, options: any = {}): Promise<any> {
      calls.push({ method: 'listResourcePage', args: [id, options] })
      return { items: [{ external_id: options.cursor ? 'space-two' : 'space-one', name: options.q || 'Engineering', type: 'space', has_children: true, metadata: { space_key: 'ENG' } }], next_cursor: options.cursor ? '' : 'next' }
    },
    async listResources(id: string, parent?: string) {
      calls.push({ method: 'listResources', args: [id, parent] })
      return []
    },
    async validateCredentials(type: string, credentials: Record<string, string>) {
      calls.push({ method: 'validateCredentials', args: [type, { ...credentials }] })
      if (type === 'confluence' && credentials.credential_source === 'system') return
      if (credentials.access_token !== 'rotated-token') throw new Error('gitlab API /user: status 401')
    },
    async validateConnection(id: string) {
      calls.push({ method: 'validateConnection', args: [id] })
      if (storedToken !== 'rotated-token') throw new Error('gitlab API /user: status 401')
    },
    async updateDataSource(id: string, data: any) {
      calls.push({ method: 'updateDataSource', args: [id, JSON.parse(JSON.stringify(data))] })
      // The main update endpoint deliberately preserves stored credentials.
    },
    async putDataSourceCredentials(id: string, credentials: Record<string, string>) {
      calls.push({ method: 'putDataSourceCredentials', args: [id, { ...credentials }] })
      storedToken = credentials.access_token
    },
  }
  const props = reactive({
    visible: false, kbId: 'kb-one',
    dataSource: create ? null : {
      id: 'source-one', name: 'GitLab', type,
      credentials: { credentials: { configured } },
      config: { resource_ids: [], settings },
      sync_schedule: '0 0 */6 * * *', sync_mode: 'incremental',
      conflict_strategy: 'overwrite', sync_deletions: true,
    },
  })
  const exports: any = {}
  runInNewContext(compiled, {
    exports,
    require(name: string) {
      if (name === 'vue') return require('vue')
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      if (name === 'tdesign-vue-next') return { MessagePlugin: { warning() {}, success() {}, error() {} } }
      if (name === '@/api/datasource') return api
      if (name === '@/stores/auth') return {
        useAuthStore: () => ({ hasRole: (minimum: string) => role === 'admin' ||
          (role === 'contributor' && minimum === 'contributor') }),
      }
      return { default: {} }
    },
    URL, console,
  })
  const component = exports.default
  component.render = () => null
  const renderer = createRenderer<any, any>({
    createElement: () => ({}), createText: () => ({}), createComment: () => ({}),
    insert() {}, remove() {}, setElementText() {}, setText() {}, patchProp() {},
    parentNode: () => null, nextSibling: () => null,
  })
  const instance = ref<any>()
  const app = renderer.createApp({ render: () => h(component, { ...props, ref: instance }) })
  app.mount({})
  props.visible = true
  await nextTick()
  const vm = instance.value
  async function replace(token = 'rotated-token') {
    vm.enterReplaceCredentials()
    vm.form.config.credentials = { base_url: 'https://gitlab.example.com', access_token: token }
    await nextTick()
  }
  return { vm, api, props, calls, replace, storedToken: () => storedToken, close: () => app.unmount() }
}

test('space viewers can configure every connector without administrator privileges', async () => {
  const f = await fixture({ create: true, type: 'feishu', role: 'viewer' })
  try {
    const types = Array.from(f.vm.connectorDefs, (def: any) => def.type)
    for (const kind of ['feishu', 'feishu_drive', 'lark', 'lark_drive', 'gitlab']) assert.ok(types.includes(kind))
  } finally { f.close() }
})

test('new Feishu resource discovery keeps its unused draft paused until final save', async () => {
  const f = await fixture({ create: true, type: 'feishu' })
  try {
    f.vm.selectType(f.vm.connectorDefs.find((def: any) => def.type === 'feishu'))
    await f.vm.loadResources()
    await f.vm.loadResources()
    assert.equal(f.calls.find(call => call.method === 'createDataSource')?.args[0].status, 'paused')
    assert.equal(f.calls.find(call => call.method === 'updateDataSource')?.args[1].status, 'paused')
    assert.equal(f.calls.filter(call => call.method === 'createDataSource').length, 1)
  } finally { f.close() }
})

test('Confluence paging and search retain selections and loaded children', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    assert.equal(f.calls.filter(c => c.method === 'listResources').length, 0)
    assert.equal(f.vm.visibleTree.length, 1)
    f.vm.toggleResource('space-one')
    await f.vm.loadSpacePage(true)
    assert.equal(f.vm.visibleTree.length, 2)
    assert.equal(f.vm.spaceNextCursor, '')
    assert.deepEqual([...f.vm.selectedResourceIds], ['space-one'])
    f.vm.spaceSearch = 'Finance'
    await f.vm.loadSpacePage()
    assert.equal(f.vm.visibleTree.length, 1)
    assert.deepEqual([...f.vm.selectedResourceIds], ['space-one'])
    f.vm.removeSelectedResource('space-one')
    assert.equal(f.vm.selectedResourceIds.length, 0)
  } finally { f.close() }
})

test('new Confluence uses only a system marker when built-in credentials are available', async () => {
  const f = await fixture({ create: true, type: 'confluence', settings: {} })
  try {
    f.vm.builtinConfluence = { enabled: true, base_url: 'http://confluence.internal' }
    f.vm.selectType({ type: 'confluence', available: true })
    await nextTick()
    assert.equal(f.vm.usesSystemConfluence, true)
    assert.equal(f.vm.displayedCredentialFields.length, 0)
    assert.equal(f.vm.credentialsInputVisible, false)
    await f.vm.testConnection()
    const call = f.calls.find(c => c.method === 'validateCredentials')!
    assert.deepEqual(Object.keys(call.args[1]), ['credential_source'])
    assert.equal(call.args[1].credential_source, 'system')
    const payload = f.vm.buildConfigPayload()
    assert.equal(payload.settings.credential_source, 'system')
    assert.equal(JSON.stringify(payload).includes('username'), false)
    assert.equal(JSON.stringify(payload).includes('password'), false)
  } finally { f.close() }
})

test('switching an existing personal Confluence connection clears public credential remnants', async () => {
  const f = await fixture({ type: 'confluence', settings: { username: 'old-user', password: 'old-password', base_url: 'http://old.example' } })
  try {
    f.vm.useSystemConfluence()
    await nextTick()
    assert.equal(f.vm.form.config.settings.username, undefined)
    assert.equal(f.vm.form.config.settings.password, undefined)
    assert.equal(f.vm.replaceCredentialsMode, false)
    assert.equal(f.vm.displayedCredentialFields.length, 0)
    await f.vm.testConnection()
    assert.equal(f.calls.some(c => c.method === 'updateDataSource'), false)
    assert.equal(f.calls.some(c => c.method === 'putDataSourceCredentials'), false)
    assert.equal(f.calls.find(c => c.method === 'validateCredentials')!.args[1].credential_source, 'system')
  } finally { f.close() }
})

test('disabled system profile never reveals personal credential inputs for managed sources', async () => {
  const f = await fixture({ type: 'confluence', settings: { credential_source: 'system' } })
  try {
    assert.equal(f.vm.builtinConfluence.enabled, false)
    assert.equal(f.vm.usesSystemConfluence, true)
    assert.equal(f.vm.credentialsInputVisible, false)
    assert.equal(f.vm.displayedCredentialFields.length, 0)
    assert.equal(f.vm.form.config.credentials.password, undefined)
  } finally { f.close() }
})

test('Confluence ignores out-of-order search responses', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    const pending: Array<(value: any) => void> = []
    f.api.listResourcePage = async () => new Promise(resolve => pending.push(resolve))
    f.vm.spaceSearch = 'old'
    const old = f.vm.loadSpacePage()
    f.vm.spaceSearch = 'new'
    const latest = f.vm.loadSpacePage()
    pending[1]({ items: [{ external_id: 'new', name: 'New', type: 'space' }], next_cursor: '' })
    await latest
    pending[0]({ items: [{ external_id: 'old', name: 'Old', type: 'space' }], next_cursor: 'stale' })
    await old
    assert.equal(f.vm.visibleTree[0].resource.external_id, 'new')
    assert.equal(f.vm.spaceNextCursor, '')
  } finally { f.close() }
})

test('Confluence child listing sends a verified space lookup hint', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    await f.vm.ensureChildrenLoaded('space-one')
    const call = f.calls.filter(c => c.method === 'listResourcePage').at(-1)!
    assert.equal(call.args[1].parentId, 'space-one')
    assert.equal(call.args[1].spaceKey, 'ENG')
  } finally { f.close() }
})

test('Confluence child lists load one page and expose a continuation row', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    f.api.listResourcePage = async (_id, options) => ({
      items: [{ external_id: options.cursor ? 'page:space-one:2' : 'page:space-one:1', parent_id: 'space-one', name: 'Page', type: 'page', has_children: true, metadata: {} }],
      next_cursor: options.cursor ? '' : 'children-next',
    })
    f.vm.expandedResourceIds = new Set(['space-one'])
    await f.vm.ensureChildrenLoaded('space-one')
    assert.equal(f.vm.childrenMap.get('space-one').length, 1)
    assert.equal(f.vm.visibleTree.filter((row: any) => row.loadMoreAfter).length, 1)
    await f.vm.ensureChildrenLoaded('space-one', true)
    assert.equal(f.vm.childrenMap.get('space-one').length, 2)
    assert.equal(f.vm.visibleTree.filter((row: any) => row.loadMoreAfter).length, 0)
  } finally { f.close() }
})

test('unchecking an inherited child retains siblings on subsequent pages', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    f.api.listResourcePage = async (_id, options) => ({
      items: (options.cursor ? ['3'] : ['1', '2']).map(id => ({ external_id: `page:space-one:${id}`, parent_id: 'space-one', name: id, type: 'page', has_children: true, metadata: {} })),
      next_cursor: options.cursor ? '' : 'children-next',
    })
    await f.vm.ensureChildrenLoaded('space-one')
    f.vm.selectedResourceIds = ['space-one']
    await f.vm.toggleResource('page:space-one:1')
    assert.deepEqual([...f.vm.selectedResourceIds].sort(), ['page:space-one:2', 'page:space-one:3'])
    assert.equal(f.vm.changingSelection, false)
  } finally { f.close() }
})

test('failed sibling pagination leaves the entire inherited selection intact', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    f.api.listResourcePage = async (_id, options) => {
      if (options.cursor) throw new Error('upstream unavailable')
      return { items: [{ external_id: 'page:space-one:1', parent_id: 'space-one', name: 'Page', type: 'page', has_children: true, metadata: {} }], next_cursor: 'next' }
    }
    await f.vm.ensureChildrenLoaded('space-one')
    f.vm.selectedResourceIds = ['space-one']
    await f.vm.toggleResource('page:space-one:1')
    assert.deepEqual([...f.vm.selectedResourceIds], ['space-one'])
    assert.equal(f.vm.childNextCursors.get('space-one'), 'next')
    assert.equal(f.vm.changingSelection, false)
  } finally { f.close() }
})

test('locating a saved page reveals its path outside the current root page', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    const pageId = 'page:other-space:20'
    f.vm.selectedResourceIds = [pageId]
    f.api.listResourcePage = async (_id, options) => {
      assert.equal(options.revealId, pageId)
      return { items: [
        { external_id: 'other-space', name: 'Other', type: 'space', has_children: true, metadata: {} },
        { external_id: pageId, parent_id: 'other-space', name: 'Saved page', type: 'page', has_children: true, metadata: {} },
      ], next_cursor: '' }
    }
    await f.vm.locateSelectedResource(pageId)
    assert.equal(f.vm.visibleTree.some((row: any) => row.resource.external_id === pageId), true)
    assert.equal(f.vm.loadedChildrenIds.has('other-space'), false)
    assert.deepEqual([...f.vm.selectedResourceIds], [pageId])
  } finally { f.close() }
})

test('bulk expansion bounds concurrency and excludes spaces hidden by search', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    const roots = Array.from({ length: 9 }, (_, i) => ({ external_id: String(i + 1), name: `Space ${i}`, type: 'space', has_children: true, metadata: { space_key: String(i) } }))
    f.vm.resources = roots
    f.vm.spaceRootIds = new Set(roots.slice(0, 8).map(r => r.external_id))
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let active = 0, maxActive = 0, calls = 0
    f.api.listResourcePage = async () => {
      active++; calls++; maxActive = Math.max(maxActive, active)
      await gate
      active--
      return { items: [] }
    }
    const work = f.vm.expandAllNodes()
    await nextTick()
    assert.equal(active, 4)
    release()
    await work
    assert.equal(calls, 8)
    assert.equal(maxActive, 4)
  } finally { f.close() }
})

test('collapsing bulk expansion stops queued child requests', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    f.vm.resources = Array.from({ length: 10 }, (_, i) => ({ external_id: String(i), name: 'Space', type: 'space', has_children: true }))
    f.vm.spaceRootIds = new Set(f.vm.resources.map((r: any) => r.external_id))
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let calls = 0
    f.api.listResourcePage = async () => { calls++; await gate; return { items: [] } }
    const work = f.vm.expandAllNodes()
    await nextTick()
    f.vm.collapseAllNodes()
    release()
    await work
    assert.equal(calls, 4)
    assert.equal(f.vm.expandedResourceIds.size, 0)
  } finally { f.close() }
})

test('Confluence retains unloaded page selections until explicitly cleared', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    await f.vm.loadResources()
    f.vm.selectedResourceIds = ['page:space-one:unloaded']
    assert.equal(f.vm.checkStates.get('space-one'), 'indeterminate')
    f.vm.toggleResource('space-one')
    assert.equal(f.vm.selectedResourceIds.length, 0)
    f.vm.selectedResourceIds = ['page:space-one:unloaded']
    const cover = new Set(f.vm.selectedResourceIds)
    f.vm.checkResource('space-one', cover)
    assert.deepEqual([...cover], ['space-one'])
  } finally { f.close() }
})

test('Confluence drops responses from a closed picker session', async () => {
  const f = await fixture({ type: 'confluence', settings: {} })
  try {
    let finish!: (value: any) => void
    f.api.listResourcePage = async () => new Promise(resolve => { finish = resolve })
    const request = f.vm.loadSpacePage()
    f.props.visible = false
    await nextTick()
    f.props.visible = true
    await nextTick()
    finish({ items: [{ external_id: 'stale', name: 'Stale' }], next_cursor: 'stale' })
    await request
    assert.equal(f.vm.resources.length, 0)
    assert.equal(f.vm.loadingSpacePage, false)
  } finally { f.close() }
})

test('rotated GitLab credentials are tested without updating the saved data source', async () => {
  const f = await fixture()
  try {
    await f.replace()
    await f.vm.testConnection()
    assert.equal(f.vm.testResult, 'success')
    assert.deepEqual(f.calls, [{ method: 'validateCredentials', args: ['gitlab', {
      base_url: 'https://gitlab.example.com', access_token: 'rotated-token',
    }] }])
    assert.equal(f.storedToken(), 'expired-token')
  } finally { f.close() }
})

test('Next tests the replacement and final save commits credentials before settings', async () => {
  const f = await fixture()
  try {
    await f.replace()
    await f.vm.nextStep()
    assert.equal(f.vm.step, 2)
    await f.vm.nextStep()
    assert.equal(f.vm.step, 3)
    await f.vm.handleSubmit()
    assert.deepEqual(f.calls.map(call => call.method), [
      'validateCredentials', 'putDataSourceCredentials', 'updateDataSource',
    ])
    assert.equal(f.storedToken(), 'rotated-token')
    assert.deepEqual(f.calls[2].args[1].config.credentials, {})
  } finally { f.close() }
})

test('invalid replacement stays on the credentials step and can be corrected', async () => {
  const f = await fixture()
  try {
    await f.replace('invalid-token')
    await f.vm.nextStep()
    assert.equal(f.vm.step, 1)
    assert.equal(f.vm.testResult, 'error')
    assert.match(f.vm.testErrorMsg, /401/)
    assert.equal(f.storedToken(), 'expired-token')
    await f.replace()
    await f.vm.nextStep()
    assert.equal(f.vm.step, 2)
  } finally { f.close() }
})

test('testing unchanged credentials still validates the stored token', async () => {
  const f = await fixture()
  try {
    await f.vm.testConnection()
    assert.equal(f.vm.testResult, 'error')
    assert.deepEqual(f.calls.map(call => call.method), ['updateDataSource', 'validateConnection'])
  } finally { f.close() }
})

test('an existing data source with no saved credentials tests the entered token', async () => {
  const f = await fixture({ configured: false })
  try {
    await f.replace()
    await f.vm.testConnection()
    assert.equal(f.vm.testResult, 'success')
    assert.deepEqual(f.calls.map(call => call.method), ['validateCredentials'])
    assert.equal(f.storedToken(), '')
  } finally { f.close() }
})

test('new GitLab data sources continue to test credentials without persistence', async () => {
  const f = await fixture({ create: true })
  try {
    f.vm.selectType(f.vm.connectorDefs.find((def: any) => def.type === 'gitlab'))
    await f.replace()
    await f.vm.testConnection()
    assert.equal(f.vm.testResult, 'success')
    assert.deepEqual(f.calls.map(call => call.method), ['validateCredentials'])
  } finally { f.close() }
})

test('a new Yuque data source adopts the TOC folder layout, but not the filter', async () => {
  const f = await fixture({ create: true })
  try {
    assert.equal(f.vm.form.config.settings.folder_mode, undefined)
    f.vm.selectType(f.vm.connectorDefs.find((def: any) => def.type === 'yuque'))
    assert.equal(f.vm.form.config.settings.folder_mode, 'toc')
    // The layout is a presentation choice; toc_only decides what may enter the
    // knowledge base, so a new source is deliberately left without it.
    assert.equal(f.vm.form.config.settings.toc_only, undefined)
    assert.equal(f.vm.yuqueTOCOnly, false)
  } finally { f.close() }
})

test('an existing Yuque data source reports its admission filter', async () => {
  const f = await fixture({ type: 'yuque', settings: { folder_mode: 'toc', toc_only: true } })
  try {
    assert.equal(f.vm.yuqueTOCOnly, true)
  } finally { f.close() }
})

test('an existing Yuque data source is never switched to the TOC layout', async () => {
  const f = await fixture({ type: 'yuque', settings: {} })
  try {
    // Opening a source created before this control existed must not inject
    // folder_mode: the connector default (flat) has to stand, otherwise editing
    // an unrelated field would silently re-file the whole knowledge base.
    assert.equal(f.vm.form.config.settings.folder_mode, undefined)
    assert.equal(f.vm.yuqueFolderMode, 'none')
  } finally { f.close() }
})

test('an existing Yuque data source on the TOC layout reports it', async () => {
  const f = await fixture({ type: 'yuque', settings: { folder_mode: 'toc' } })
  try {
    assert.equal(f.vm.yuqueFolderMode, 'toc')
  } finally { f.close() }
})

test('Cloud hierarchy limitation stays visible after an empty space expansion', async () => {
  const f = await fixture()
  try {
    f.vm.resources = [{
      external_id: 'space-1',
      name: 'Cloud space',
      type: 'space',
      has_children: true,
      metadata: { hierarchy_limitation: 'cloud_top_level_containers' },
    }]
    f.vm.expandedResourceIds = new Set(['space-1'])
    await nextTick()
    assert.equal(f.vm.visibleTree.some((row: any) => row.noticeAfter), true)

    f.vm.expandedResourceIds = new Set()
    f.vm.loadedChildrenIds = new Set(['space-1'])
    f.vm.resources = [{ ...f.vm.resources[0], has_children: false }]
    await nextTick()
    assert.equal(f.vm.visibleTree.some((row: any) => row.noticeAfter), true)
  } finally { f.close() }
})
