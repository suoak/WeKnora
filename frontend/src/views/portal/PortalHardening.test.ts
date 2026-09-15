import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('Portal defines a system-font typography hierarchy without content scaling', () => {
  const home = source('./PortalHome.vue')
  const files = ['PortalHero.vue','IpdKnowledgeMap.vue','IpdStageCard.vue','SpaceSummary.vue','PublicKnowledgeSection.vue','SpaceAccessDialog.vue']
    .map((name)=>source(`../../components/portal/${name}`)).join('\n')
  assert.match(home, /--portal-font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI","Microsoft YaHei","PingFang SC","Noto Sans CJK SC",sans-serif/)
  assert.match(home, /--portal-page-title:28px/)
  assert.match(home, /--portal-section-title:20px/)
  assert.doesNotMatch(files, /font-size:(?:9|10|11)px/)
  assert.doesNotMatch(files, /transform:|translate3d|scale\(|zoom\s*:/)
})

test('hero is a compact branded portal surface with legible KPI and search sizing', () => {
  const hero = source('../../components/portal/PortalHero.vue')
  assert.match(hero, /class="hero-surface"/)
  assert.match(hero, /background:var\(--portal-brand-surface\)/)
  assert.match(hero, /font-size:var\(--portal-page-title\)/)
  assert.match(hero, /hero-stats strong\{[^}]*font-size:28px/)
  assert.match(hero, /hero-search\{[^}]*height:46px/)
  assert.match(hero, /hero-search:focus-visible/)
  assert.match(hero, /portalExperience\.currentScope/)
})

test('lifecycle rail keeps seven readable steps and removes nested stage cards', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const stage = source('../../components/portal/IpdStageCard.vue')
  assert.match(map, /grid-template-columns:repeat\(7,minmax\(132px,1fr\)\)/)
  assert.match(map, /@media\(max-width:1399px\).*repeat\(7,150px\)/)
  assert.match(map, /overflow-x:auto/)
  assert.match(stage, /font-size:15px/)
  assert.match(stage, /font-size:12px/)
  assert.doesNotMatch(stage, /border:1px[^}]+stage-step/)
  assert.doesNotMatch(stage, /SpaceSummary/)
})

test('active stage knowledge panel has four, three, two and one column tiers', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  assert.match(map, /stage-space-grid\{[^}]*repeat\(4,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:1599px\).*repeat\(3,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:1100px\).*repeat\(2,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:900px\)[\s\S]*stage-space-grid\{grid-template-columns:1fr\}/)
})

test('public knowledge balances real cards with explanatory space and responsive layout', () => {
  const section = source('../../components/portal/PublicKnowledgeSection.vue')
  assert.match(section, /class="public-surface"/)
  assert.match(section, /repeat\(auto-fit,minmax\(280px,360px\)\)/)
  assert.match(section, /<aside>/)
  assert.match(section, /portal\.publicZone\.description/)
  assert.match(section, /<SpaceSummary v-for="space in publicSpaces"/)
  assert.doesNotMatch(section, /moreSpaces|fake|placeholderSpace/)
})

test('loading, empty, and error states remain distinct and retry only failed sources', () => {
  const store = source('../../stores/portal.ts')
  const home = source('./PortalHome.vue')
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const publicSection = source('../../components/portal/PublicKnowledgeSection.vue')
  assert.match(store, /Promise\.allSettled\(\[loadStages\(\), loadOverviewSpaces\(\)\]\)/)
  assert.match(home, /function retryIpd\(\)/)
  assert.match(map, /PortalSectionState v-if="error"/)
  assert.match(map, /v-if="loading" class="lifecycle-rail is-loading"/)
  assert.match(publicSection, /v-else class="public-surface"/)
  assert.match(publicSection, /v-else class="public-empty"/)
})

test('Portal initialization stays batch-based and does not add resource requests', () => {
  const store = source('../../stores/portal.ts')
  const home = source('./PortalHome.vue')
  assert.match(store, /Promise\.allSettled\(\[loadStages\(\), loadOverviewSpaces\(\)\]\)/)
  assert.doesNotMatch(store, /for\s*\([^)]*\)[\s\S]{0,200}(listPortalSpaces|listKnowledgeBases)/)
  assert.doesNotMatch(home, /loadMySpaces|listKnowledgeBases|listAgents|recommend/i)
})

test('access controls retain keyboard gate and real-request duplicate protection', () => {
  const summary = source('../../components/portal/SpaceSummary.vue')
  const dialog = source('../../components/portal/SpaceAccessDialog.vue')
  const store = source('../../stores/portal.ts')
  const home = source('./PortalHome.vue')
  const api = source('../../api/portal.ts')
  assert.match(summary, /@keydown\.space\.prevent="activate"/)
  assert.match(summary, /:aria-label="actionLabel\('search'\)"/)
  assert.match(summary, /button:focus-visible/)
  assert.match(dialog, /:disabled="submitting \|\| !valid"/)
  assert.match(store, /access_request_pending = true/)
  assert.match(store, /can_request_access = false/)
  assert.match(home, /if\(space\.access_state!=='accessible'\)\{openAccessDialog\(space\);return\}/)
  assert.match(home, /await portal\.requestAccess\(accessSpace\.value\.tenant_id,reason\)/)
  assert.match(api, /\/api\/v1\/portal\/spaces\/\$\{tenantId\}\/access-requests/)
})

test('new-user guidance remains an overlay shown once rather than layout content', () => {
  const guide = source('../../components/NewUserGuide.vue')
  assert.match(guide, /<SpotlightGuide/)
  assert.match(guide, /localStorage\.getItem\(GLOBAL_USER_GUIDE_KEY\) !== '1'/)
  assert.match(guide, /localStorage\.setItem\(GLOBAL_USER_GUIDE_KEY, '1'\)/)
})
