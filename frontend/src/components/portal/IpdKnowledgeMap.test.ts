import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('knowledge map renders one seven-stage lifecycle rail and one active-stage panel', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(map, /class="lifecycle-rail" role="tablist"/)
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /:space-count="spacesFor\(stage\.key\)\.length"/)
  assert.match(map, /v-else-if="activeStage" class="stage-panel" role="tabpanel"/)
  assert.match(map, /v-for="space in activeStageSpaces"/)
  assert.match(stage, /role="tab"/)
  assert.doesNotMatch(stage, /<SpaceSummary|stage-body|stage-spaces/)
})

test('active stage selection uses loaded data and preserves multi-stage overview counts', () => {
  const map = source('./IpdKnowledgeMap.vue')
  assert.match(map, /defaultPortalStageKey\(props\.stages,props\.spaces,props\.activeTenantId\)/)
  assert.match(map, /function selectStage\(stageKey:string\)/)
  assert.match(map, /selectionTouched\.value=true/)
  assert.match(map, /props\.spaces\.filter\(space=>space\.stages\.includes\(stage\)\)/)
  assert.match(map, /buildKnowledgeHierarchySummary\(props\.spaces/)
})

test('space summary separates accessible entry from discoverable dialog behavior', () => {
  const summary = source('./SpaceSummary.vue')
  const home = source('../../views/portal/PortalHome.vue')
  assert.match(summary, /if\(props\.space\.access_state === 'accessible'\)emit\('enter',props\.space\)/)
  assert.match(summary, /else emit\('restricted',props\.space\)/)
  assert.match(summary, /space\.knowledge_base_count/)
  assert.match(summary, /space\.file_count/)
  assert.match(home, /if\(space\.access_state!=='accessible'\)\{openAccessDialog\(space\);return\}/)
  assert.match(home, /switchWorkspaceAndNavigate/)
})

test('accessible cards expose lightweight ask/search icons and one primary entry', () => {
  const summary = source('./SpaceSummary.vue')
  assert.match(summary, /class="icon-actions"/)
  assert.match(summary, /class="enter-action"/)
  assert.match(summary, /@click="\$emit\('search', space\)"/)
  assert.match(summary, /@click="\$emit\('ask', space\)"/)
  assert.match(summary, /<t-tooltip :content="actionLabel\('search'\)"/)
  assert.match(summary, /:aria-label="actionLabel\('ask'\)"/)
  assert.doesNotMatch(summary, /v-if="isActiveSpace"[^>]+accessible-actions/)
})

test('restricted cards show one state-aware CTA and always activate the gate', () => {
  const summary = source('./SpaceSummary.vue')
  assert.match(summary, /v-else class="restricted-action"/)
  assert.match(summary, /access_request_pending\?t\('portal\.pending'\)/)
  assert.match(summary, /can_request_access\?t\('portal\.requestAccess'\):t\('portalMap\.getAccess'\)/)
  assert.match(summary, /@keydown\.enter\.prevent="activate"/)
  assert.match(summary, /@keydown\.space\.prevent="activate"/)
  assert.doesNotMatch(summary, /portalMap\.permissionRequired|portalMap\.learnAccess/)
})

test('access dialog retains real request states and readable space context', () => {
  const dialog = source('./SpaceAccessDialog.vue')
  assert.match(dialog, /space\.access_request_pending/)
  assert.match(dialog, /space\.can_request_access/)
  assert.match(dialog, /portalMap\.accessDialog\.titleWithSpace/)
  assert.match(dialog, /stageLabel/)
  assert.match(dialog, /:disabled="submitting \|\| !valid"/)
  assert.doesNotMatch(dialog, /knowledge_bases|documents|approve|reject/i)
})

test('hidden spaces are absent from the Portal response contract', () => {
  const api = source('../../api/portal.ts')
  assert.match(api, /PortalAccessState = 'accessible' \| 'discoverable'/)
  assert.doesNotMatch(api, /PortalAccessState[^\n]*hidden/)
})
