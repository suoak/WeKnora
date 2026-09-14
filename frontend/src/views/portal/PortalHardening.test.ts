import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('desktop panorama uses seven natural-height columns and narrow layouts expose continuation', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const stage = source('../../components/portal/IpdStageCard.vue')
  assert.match(map, /grid-template-columns:repeat\(7,minmax\(0,1fr\)\)/)
  assert.match(map, /align-items:start/)
  assert.match(map, /@media\(max-width:1319px\)/)
  assert.match(map, /grid-auto-columns:minmax\(172px,1fr\)/)
  assert.match(map, /mask-image:linear-gradient/)
  assert.match(map, /scrollbar-width:thin/)
  assert.match(stage, /min-width:0/)
  assert.match(stage, /portalMap\.stageInventoryCompact/)
})

test('hero stays compact while preserving scoped search and visible focus', () => {
  const hero = source('../../components/portal/PortalHero.vue')
  assert.match(hero, /class="hero-overview"/)
  assert.match(hero, /\.portal-hero\{padding:20px 0 18px/)
  assert.match(hero, /\.hero-search\{[^}]*height:42px/)
  assert.match(hero, /hero-search:focus-visible/)
  assert.match(hero, /quick-actions button:focus-visible/)
  assert.match(hero, /portalExperience\.currentScope/)
})

test('public cards use three, two, and one column tiers without changing card vocabulary', () => {
  const section = source('../../components/portal/PublicKnowledgeSection.vue')
  assert.match(section, /grid-template-columns:repeat\(3,minmax\(0,1fr\)\)/)
  assert.match(section, /@media\(max-width:1599px\).*repeat\(2,minmax\(0,1fr\)\)/)
  assert.match(section, /@media\(max-width:900px\).*grid-template-columns:1fr/)
  assert.match(section, /<SpaceSummary/)
})

test('loading, empty, and error states are distinct and retry only their failed source', () => {
  const store = source('../../stores/portal.ts')
  const home = source('./PortalHome.vue')
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const publicSection = source('../../components/portal/PublicKnowledgeSection.vue')
  assert.match(store, /stagesLoading = ref\(false\)/)
  assert.match(store, /overviewLoading = ref\(false\)/)
  assert.match(store, /stagesError = ref\(false\)/)
  assert.match(store, /overviewError = ref\(false\)/)
  assert.match(store, /Promise\.allSettled\(\[loadStages\(\), loadOverviewSpaces\(\)\]\)/)
  assert.match(home, /function retryIpd\(\)/)
  assert.match(map, /PortalSectionState v-if="error"/)
  assert.match(map, /v-if="loading"/)
  assert.match(publicSection, /v-else class="public-empty"/)
})

test('Portal initialization stays batch-based and does not restore legacy requests', () => {
  const store = source('../../stores/portal.ts')
  const home = source('./PortalHome.vue')
  assert.match(store, /Promise\.allSettled\(\[loadStages\(\), loadOverviewSpaces\(\)\]\)/)
  assert.doesNotMatch(store, /for\s*\([^)]*\)[\s\S]{0,200}(listPortalSpaces|listKnowledgeBases)/)
  assert.doesNotMatch(home, /loadMySpaces|listKnowledgeBases|listAgents|recommend/i)
})

test('access controls expose labels, focus, dialog input naming, and duplicate-submit protection', () => {
  const summary = source('../../components/portal/SpaceSummary.vue')
  const dialog = source('../../components/portal/SpaceAccessDialog.vue')
  const store = source('../../stores/portal.ts')
  assert.match(summary, /@keydown\.space\.prevent="activate"/)
  assert.match(summary, /:aria-label="actionLabel\('search'\)"/)
  assert.match(summary, /button:focus-visible/)
  assert.match(dialog, /:aria-label="t\('portal\.reasonPlaceholder'\)"/)
  assert.match(dialog, /:disabled="submitting \|\| !valid"/)
  assert.match(store, /access_request_pending = true/)
  assert.match(store, /can_request_access = false/)
})

test('new-user guidance remains an overlay shown once rather than layout content', () => {
  const guide = source('../../components/NewUserGuide.vue')
  assert.match(guide, /<SpotlightGuide/)
  assert.match(guide, /localStorage\.getItem\(GLOBAL_USER_GUIDE_KEY\) !== '1'/)
  assert.match(guide, /localStorage\.setItem\(GLOBAL_USER_GUIDE_KEY, '1'\)/)
})
