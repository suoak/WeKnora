import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('knowledge map renders every configured stage with inline spaces and empty state', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /spacesFor\(stage\.key\)/)
  assert.match(stage, /v-for="space in spaces"/)
  assert.match(stage, /portalMap\.emptyStage/)
  assert.doesNotMatch(map, /selectedStage|@click=.*select/)
})

test('multi-stage association is preserved while header summary uses raw spaces', () => {
  const map = source('./IpdKnowledgeMap.vue')
  assert.match(map, /props\.spaces\.filter\(space=>space\.stages\.includes\(stage\)\)/)
  assert.match(map, /buildKnowledgeHierarchySummary\(props\.spaces/)
  assert.doesNotMatch(map, /summary\.phases.*reduce|Object\.values\(summary\.phases\)/)
})

test('space summary separates accessible entry from discoverable dialog behavior', () => {
  const summary = source('./SpaceSummary.vue')
  const home = source('../../views/portal/PortalHome.vue')
  assert.match(summary, /access_state === 'accessible' \? 'enter' : 'restricted'/)
  assert.match(summary, /space\.knowledge_base_count/)
  assert.match(summary, /space\.file_count/)
  assert.match(home, /if\(space\.access_state!=='accessible'\)\{openAccessDialog\(space\);return\}/)
  assert.match(home, /switchWorkspaceAndNavigate/)
  assert.match(home, /if\(space\.access_state==='discoverable'\)accessSpace\.value=space/)
})

test('search and ask are offered only for the already-active space', () => {
  const summary = source('./SpaceSummary.vue')
  assert.match(summary, /v-if="isActiveSpace"/)
  assert.match(summary, /@click="\$emit\('search', space\)"/)
  assert.match(summary, /@click="\$emit\('ask', space\)"/)
})

test('access dialog exposes summary, existing request state, and contact fallback only', () => {
  const dialog = source('./SpaceAccessDialog.vue')
  assert.match(dialog, /space\.display_name/)
  assert.match(dialog, /space\.knowledge_base_count/)
  assert.match(dialog, /space\.file_count/)
  assert.match(dialog, /space\.access_request_pending/)
  assert.match(dialog, /space\.can_request_access/)
  assert.match(dialog, /space\.responsible_team|space\.contact/)
  assert.doesNotMatch(dialog, /knowledge_bases|documents|approve|reject/i)
})
