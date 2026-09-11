import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./KnowledgeBaseEditorModal.vue', import.meta.url), 'utf8')

test('editing a knowledge base closes the editor after a successful save', () => {
  assert.match(source, /emit\('success', kbId\)\s*handleClose\(\)/)
})

test('successful create closes the modal and marks the event as a creation', () => {
  const createBranch = source.match(
    /if \(editorMode\.value === 'create'\) \{([\s\S]*?)^\s{4}\} else \{/m
  )?.[1]

  assert.ok(createBranch, 'expected to find the create branch')
  assert.match(createBranch, /emit\('success', createdKbId, true\)\s*handleClose\(\)/)
  assert.doesNotMatch(createBranch, /savedKbId\.value = createdKbId/)
})

test('save button labels distinguish create from save-and-close', () => {
  assert.match(
    source,
    /const saveButtonLabel = computed\(\(\) =>\s*editorMode\.value === 'create'\s*\? t\('knowledgeEditor\.buttons\.create'\)\s*: t\('knowledgeEditor\.buttons\.saveAndClose'\)\s*\)/
  )
})

test('basic create omits technical defaults while advanced remains available', () => {
  assert.match(source, /editorMode\.value === 'create' && !advancedCreateOpen\.value/)
  assert.match(source, /name: formData\.value\.name\.trim\(\)/)
  assert.match(source, /description: formData\.value\.description\?\.trim\(\) \|\| ''/)
  assert.match(source, /knowledgeEditor\.createFlow\.showAdvanced/)
  assert.match(source, /knowledgeEditor\.createFlow\.missingEmbedding/)
})
