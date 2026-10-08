import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { parse as parseTemplate } from '@vue/compiler-dom'
import { LOCALE_BUNDLES, collectLocaleKeys } from '../../src/i18n/localeKeyAudit.ts'

const source = readFileSync(new URL('../../src/views/agent/AgentEditorModal.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const script = ts.createSourceFile('editor.ts', descriptor.scriptSetup.content, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const navIds = []
function collectKeys(node) {
  if (ts.isPropertyAssignment(node) && node.name.getText(script) === 'key' && ts.isStringLiteral(node.initializer)) navIds.push(node.initializer.text)
  ts.forEachChild(node, collectKeys)
}
function findNavigation(node) {
  if (ts.isVariableDeclaration(node) && node.name.getText(script) === 'promptNavItems') collectKeys(node.initializer)
  else ts.forEachChild(node, findNavigation)
}
findNavigation(script)
const panes = []
function walk(node, parent, ancestors = []) {
  if (node.type === 1) {
    const show = node.props.find(p => p.type === 7 && p.name === 'show')?.exp?.content
    const id = show?.match(/^activePromptAnchor === '([^']+)'$/)?.[1]
    if (id) panes.push({ id, node, parent, ancestors })
    for (const child of node.children || []) walk(child, node, [...ancestors, node])
  } else for (const child of node.children || []) walk(child, parent, ancestors)
}
walk(parseTemplate(descriptor.template.content))

test('all advertised prompt IDs have exactly one sibling content panel', () => {
  assert.equal(navIds.length, 6, 'navigation extraction must cover all six IDs')
  assert.deepEqual(panes.map(p => p.id).sort(), navIds.sort())
  assert.ok(panes.every(p => p.parent === panes[0].parent), 'prompt panes must share the same parent')
})

test('rewrite panels use the existing navigation condition and stored fields', () => {
  for (const [id, field] of [['rewrite-system', 'rewrite_prompt_system'], ['rewrite-user', 'rewrite_prompt_user']]) {
    const pane = panes.find(p => p.id === id)
    assert.ok(pane, id)
    assert.equal(pane.node.props.find(p => p.type === 7 && p.name === 'if')?.exp?.content, 'showRewritePrompts')
    const models = []
    function bindings(node) {
      if (node.type === 1) models.push(...node.props.filter(p => p.type === 7 && p.name === 'model').map(p => p.exp?.content))
      for (const child of node.children || []) bindings(child)
    }
    bindings(pane.node)
    assert.deepEqual(models, [`formData.config.${field}`])
  }
})

test('new rewrite UI only uses keys present in all five existing locales', () => {
  const block = source.slice(source.indexOf('<!-- 改写提示词复用'), source.indexOf('<!-- 检索兜底'))
  const keys = [...block.matchAll(/\$t\('([^']+)'\)/g)].map(match => match[1])
  assert.ok(keys.length)
  for (const [locale, bundle] of Object.entries(LOCALE_BUNDLES)) {
    const existing = collectLocaleKeys(bundle)
    assert.deepEqual(keys.filter(key => !existing.has(key)), [], locale)
  }
})
