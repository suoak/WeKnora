import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('Agent cards expose Run as the primary action and retain permission-aware secondary actions', () => {
  const source = read('./AgentList.vue')
  assert.equal((source.match(/class="agent-run-btn"/g) || []).length, 3)
  assert.match(source, /async function startAgentChat/)
  assert.match(source, /:disabled="agent\.disabled_by_me"/)
  assert.match(source, /v-if="canManageAgent\(agent\)"/)
  assert.match(source, /\.feature-badges\s*\{\s*display: none;/)
})

test('Agent list and editor include explicit error, read-only, and mobile contracts', () => {
  const list = read('./AgentList.vue')
  const editor = read('./AgentEditorModal.vue')
  assert.match(list, /classifyLoadError/)
  assert.match(list, /loadState\.\$\{loadError\}Title/)
  assert.match(list, /@media \(max-width: 420px\)/)
  assert.match(editor, /content-wrapper--readonly/)
  assert.match(editor, /@media \(max-width: 768px\)/)
  assert.ok(editor.indexOf("items: pickItems(['basic', 'prompts'") < editor.indexOf("items: pickItems(['knowledge', 'retrieval'"))
  assert.ok(editor.indexOf("items: pickItems(['tools', 'mcp'") < editor.indexOf("items: pickItems(['model'])"))
})

test('Knowledge views preserve context and offer a bounded mobile graph experience', () => {
  const knowledgeBase = read('../knowledge/KnowledgeBase.vue')
  const wiki = read('../knowledge/wiki/WikiBrowser.vue')
  assert.match(knowledgeBase, /class="kb-context-tabs" role="tablist"/)
  assert.match(knowledgeBase, /documentsContext/)
  assert.match(knowledgeBase, /KnowledgeProcessingSummary/)
  assert.match(wiki, /class="wiki-graph-mobile-note" role="note"/)
  assert.match(wiki, /pageLoadError/)
  assert.match(wiki, /graphLoadError/)
  assert.match(wiki, /@media \(max-width: 768px\)/)
})
