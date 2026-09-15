import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('knowledge map renders the complete seven-stage rail and every IPD space in one matrix', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(map, /<nav v-else class="lifecycle-rail">/)
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /:space-count="spacesFor\(stage\.key\)\.length"/)
  assert.match(map, /class="knowledge-matrix"/)
  assert.match(map, /v-for="space in sortedSpaces"/)
  assert.match(map, /space\.stages\.map\(stage=>stageOrder\.value\.get\(stage\)/)
  assert.doesNotMatch(map, /activeStageSpaces|role="tabpanel"|class="stage-panel"/)
  assert.doesNotMatch(stage, /role="tab"|aria-selected/)
})

test('rail activation scrolls to a stage and highlights it without filtering the matrix', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(stage, /@click="\$emit\('navigate', stage\.key\)"/)
  assert.match(map, /function navigateToStage\(stageKey:string\)/)
  assert.match(map, /querySelectorAll<HTMLElement>\('\.matrix-item'\)/)
  assert.match(map, /scrollIntoView\(\{behavior:'smooth',block:'center'\}\)/)
  assert.match(map, /highlightedStageKey\.value=stageKey/)
  assert.match(map, /space\.stages\.includes\(highlightedStageKey\)/)
  assert.match(map, /setTimeout\(\(\)=>\{highlightedStageKey\.value='';focusedStageKey\.value=''\},1400\)/)
  assert.doesNotMatch(map, /filter\([^\n]*focusedStageKey|filter\([^\n]*highlightedStageKey/)
})

test('matrix cards expose stage identity while keeping current-space treatment lightweight', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const summary = source('./SpaceSummary.vue')
  assert.match(map, /:stage-number="stageIdentity\(space\)\.number"/)
  assert.match(map, /:stage-name="stageIdentity\(space\)\.name"/)
  assert.match(summary, /class="stage-identity"/)
  assert.match(summary, /v-show="isActiveSpace"/)
  assert.match(summary, /\.space-summary\.current\{border-color:/)
  assert.doesNotMatch(summary, /\.space-summary\.current\{[^}]*background:/)
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
