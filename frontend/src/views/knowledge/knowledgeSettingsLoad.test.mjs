import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'

// Exercise the actual editor loader without mounting unrelated settings tabs.
const source = readFileSync(new URL('./KnowledgeBaseEditorModal.vue', import.meta.url), 'utf8')
const loader = source.slice(source.indexOf('const loadKBData = async ('), source.indexOf('// 处理配置更新'))
const executable = ts.transpileModule(loader + '\nglobalThis.load = loadKBData', {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText

for (const [count, locked] of [[1, true], [0, false], [undefined, true]]) {
  test(`settings open without reading failed documents; count=${count}`, async () => {
    const ref = (value) => ({ value })
    const errors = []
    const calls = []
    const context = {
      activeKbId: ref('kb'), editorMode: ref('edit'), kbEditorLoadGeneration: 1,
      loading: ref(false), hasFiles: ref(false), generatedProfile: ref(null),
      kbCreatorId: ref(''), kbTenantId: ref(0), formData: ref(null),
      initialStorageProvider: ref(''), initialIndexingStrategy: ref(null),
      isCurrentKBLoad: () => true,
      getKnowledgeBaseById: async (id) => {
        calls.push(id)
        return { data: { id, name: 'Knowledge archive', tenant_id: 1, knowledge_count: count } }
      },
      listKnowledgeFiles: async () => { throw new Error('document publication verification failed') },
      mergeImageActions: () => ({}), loadImageAttrSchema: () => {},
      t: (key) => key, MessagePlugin: { error: (message) => errors.push(message) },
      handleClose: () => errors.push('closed'), modalShell: { markClean: () => {} },
      console: { error: (...args) => errors.push(args) },
    }
    vm.runInNewContext(executable, context)
    await context.load()
    assert.deepEqual(errors, [])
    assert.deepEqual(calls, ['kb'])
    assert.equal(context.formData.value.name, 'Knowledge archive')
    assert.equal(context.hasFiles.value, locked)
    assert.equal(context.loading.value, false)
  })
}
