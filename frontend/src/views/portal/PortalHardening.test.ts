import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('Portal defines a system-font typography hierarchy without content scaling', () => {
  const home = source('./PortalHome.vue')
  const files = ['PortalHero.vue','IpdKnowledgeMap.vue','IpdStageCard.vue','SpaceSummary.vue','PublicKnowledgeSection.vue','SpaceAccessDialog.vue']
    .map((name)=>source(`../../components/portal/${name}`)).join('\n')
  assert.match(home, /--portal-font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI","Microsoft YaHei","PingFang SC","Noto Sans CJK SC",sans-serif/)
  assert.match(home, /--portal-page-title:32px/)
  assert.match(home, /--portal-section-title:21px/)
  assert.match(home, /max-width:1500px/)
  assert.match(home, /padding:24px clamp\(28px,2vw,40px\) 56px/)
  assert.doesNotMatch(files, /font-size:(?:9|10|11)px/)
  assert.doesNotMatch(files, /transform:|translate3d|scale\(|zoom\s*:/)
})

test('hero is a compact branded portal surface with legible KPI and search sizing', () => {
  const hero = source('../../components/portal/PortalHero.vue')
  assert.match(hero, /class="hero-surface"/)
  assert.match(hero, /min-height:218px/)
  assert.match(hero, /radial-gradient/)
  assert.match(hero, /background-size:28px 28px/)
  assert.match(hero, /opacity:\.03/)
  assert.match(hero, /font-size:clamp\(32px,2vw,36px\)/)
  assert.match(hero, /hero-stats strong\{[^}]*font-size:35px/)
  assert.match(hero, /hero-search\{[^}]*height:50px/)
  assert.match(hero, /hero-search:focus-visible/)
  assert.match(hero, /portalExperience\.currentScope/)
})

test('lifecycle rail dynamically keeps eight readable steps and removes nested stage cards', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const stage = source('../../components/portal/IpdStageCard.vue')
  assert.match(map, /gridTemplateColumns: `repeat\(\$\{Math\.max\(stages\.length, 1\)\}/)
  assert.match(map, /min-width:1040px/)
  assert.match(map, /overflow-x:auto/)
  assert.match(stage, /font-size:15px/)
  assert.match(stage, /font-size:13px/)
  assert.match(map, /border-radius:14px/)
  assert.match(stage, /width:34px;height:34px/)
  assert.match(stage, /height:2px/)
  assert.doesNotMatch(stage, /content:'→'/)
  assert.doesNotMatch(stage, /border:1px[^}]+stage-step/)
  assert.doesNotMatch(stage, /SpaceSummary/)
})

test('stage grid has four, three, two and one column tiers with dynamic development span', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  assert.match(map, /stage-card-grid\{[^}]*repeat\(4,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:1599px\).*repeat\(3,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:1199px\).*repeat\(2,minmax\(0,1fr\)\)/)
  assert.match(map, /@media\(max-width:900px\)[\s\S]*stage-card-grid\{grid-template-columns:minmax\(0,1fr\)\}/)
  assert.match(map, /stage-card--wide\{grid-column:span 2\}/)
  assert.match(map, /stage-card--wide \.stage-spaces\{grid-template-columns:repeat\(2/)
  assert.doesNotMatch(map, /activeStageSpaces|stage-panel|role="tabpanel"/)
})

test('public knowledge uses the shared direct grid without explanatory filler', () => {
  const section = source('../../components/portal/PublicKnowledgeSection.vue')
  assert.match(section, /repeat\(4,minmax\(240px,1fr\)\)/)
  assert.match(section, /@media\(max-width:1499px\).*auto-fit,minmax\(240px,1fr\)/)
  assert.match(section, /<SpaceSummary v-for="space in publicSpaces"/)
  assert.doesNotMatch(section, /<aside>|publicZone\.description|moreSpaces|fake|placeholderSpace/)
})

test('empty descriptions use presentation-only stage fallback and never render generic filler', () => {
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const summary = source('../../components/portal/SpaceSummary.vue')
  assert.match(map, /:fallback-description="stageFallbackDescription\(stage\.key\)"/)
  assert.match(map, /stageLocaleSuffix:\s*Record<string,\s*string>\s*=\s*\{insight:\s*'insight',concept_market:\s*'concept1'/)
  assert.match(summary, /space\?\.description\?\.trim\(\)\|\|props\.fallbackDescription\.trim\(\)/)
  assert.match(summary, /<p v-if="displayDescription">/)
  assert.doesNotMatch(summary, /portal\.noDescription/)
  assert.match(summary, /'is-redundant': repeatsSpaceName/)
})

test('space cards use an icon-led hierarchy and divided real actions without fake status', () => {
  const summary = source('../../components/portal/SpaceSummary.vue')
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  assert.match(summary, /class="space-icon"/)
  assert.match(summary, /space\.knowledge_base_count/)
  assert.match(summary, /space\.file_count/)
  assert.match(summary, /accessible-actions\{[^}]*border-top:1px solid/)
  assert.match(summary, /restricted-action\{[^}]*border-top:1px solid/)
  assert.doesNotMatch(map + summary, /研究中|进行中|规划中|未开始|已归档/)
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
  assert.match(publicSection, /v-else-if="loading" class="public-grid"/)
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
