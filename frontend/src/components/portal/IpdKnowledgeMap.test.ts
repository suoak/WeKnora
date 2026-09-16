import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('knowledge map renders every API stage in the lifecycle rail and stage groups', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(map, /<nav v-else class="lifecycle-rail"/)
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /:space-count="spacesFor\(stage\.key\)\.length"/)
  assert.match(map, /class="stage-groups"/)
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /v-for="space in spacesFor\(stage\.key\)"/)
  assert.doesNotMatch(map, /activeStageSpaces|role="tabpanel"|class="stage-panel"/)
  assert.doesNotMatch(stage, /role="tab"|aria-selected/)
  assert.match(map, /class="ipd-map"/)
  assert.match(map, /border-radius:16px/)
})

test('insight remains visible with zero spaces and the rail is dynamically sized', () => {
  const map = source('./IpdKnowledgeMap.vue')
  assert.match(map, /v-for="\(stage,index\) in stages"/)
  assert.match(map, /v-if="spacesFor\(stage\.key\)\.length"/)
  assert.match(map, /v-else class="stage-empty"/)
  assert.match(map, /repeat\(\$\{Math\.max\(stages\.length, 1\)\}/)
  assert.doesNotMatch(map, /repeat\(7|item in 7|stages\.length === 7/)
})

test('rail activation scrolls to a stage group and highlights it without filtering', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const stage = source('./IpdStageCard.vue')
  assert.match(stage, /@click="\$emit\('navigate', stage\.key\)"/)
  assert.match(stage, /<button type="button"/)
  assert.match(map, /function navigateToStage\(stageKey:string\)/)
  assert.match(map, /querySelector<HTMLElement>\(`\[data-stage-group=/)
  assert.match(map, /scrollIntoView\(\{behavior:'smooth',block:'center'\}\)/)
  assert.match(map, /highlightedStageKey\.value=stageKey/)
  assert.match(map, /highlightedStageKey === stage\.key/)
  assert.match(map, /setTimeout\(\(\)=>\{highlightedStageKey\.value='';focusedStageKey\.value=''\},800\)/)
  assert.doesNotMatch(map, /filter\([^\n]*focusedStageKey|filter\([^\n]*highlightedStageKey/)
})

test('matrix cards expose stage identity while keeping current-space treatment lightweight', () => {
  const map = source('./IpdKnowledgeMap.vue')
  const summary = source('./SpaceSummary.vue')
  assert.match(map, /:stage-number="String\(index \+ 1\)\.padStart\(2, '0'\)"/)
  assert.match(map, /:stage-name="stage\.name"/)
  assert.match(summary, /class="stage-identity"/)
  assert.match(summary, /v-show="isActiveSpace"/)
  assert.match(summary, /\.space-summary\.current\{border-color:/)
  assert.match(summary, /\.space-summary\.current\{[^}]*background:/)
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
  assert.match(summary, /class="space-icon"/)
  assert.match(summary, /accessible-actions\{[^}]*border-top:1px solid/)
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
