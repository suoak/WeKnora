import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('portal routes allow tenantless auth and admin route requires system admin', () => {
  const router = source('../../router/index.ts')
  assert.match(router, /path:\s*["']\/portal["'][\s\S]*?requiresAuth:\s*true[\s\S]*?requiresTenant:\s*false/)
  assert.match(router, /path:\s*["']\/portal\/admin["'][\s\S]*?requiresTenant:\s*false[\s\S]*?requiresSystemAdmin:\s*true/)
})

test('portal homepage uses only Portal summary data and tenant switch invalidates Portal state', () => {
  const combined = [source('./PortalHome.vue'), source('../../stores/portal.ts')].join('\n')
  assert.match(combined, /listPortalSpaces/)
  for (const forbidden of ['listKnowledgeBases', 'listAgents', '@/stores/knowledge', '@/stores/chat', '@/api/datasource', '@/api/mcp']) {
    assert.doesNotMatch(combined, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))
  }
  assert.match(source('../../utils/tenantSwitch.ts'), /usePortalStore\(\)\.invalidate\(\)/)
})

test('stage and overview loading are isolated and access requests refresh overview state', () => {
  const store = source('../../stores/portal.ts')
  assert.match(store, /Promise\.allSettled\(\[loadStages\(\), loadOverviewSpaces\(\)\]\)/)
  assert.match(store, /overviewSpaces\.value = uniquePortalSpaces/)
  assert.match(store, /createPortalAccessRequest\(tenantId, reason\.trim\(\)\)[\s\S]*access_request_pending = true[\s\S]*Promise\.allSettled\(\[loadOverviewSpaces\(\), loadSpaces\(\)\]\)/)
})

test('portal renders one panorama tree and removes legacy duplicated discovery surfaces', () => {
  const home = source('./PortalHome.vue')
  const map = source('../../components/portal/IpdKnowledgeMap.vue')
  const publicSection = source('../../components/portal/PublicKnowledgeSection.vue')
  const summary = source('../../components/portal/SpaceSummary.vue')
  assert.match(home, /<PortalHero/)
  assert.match(home, /<IpdKnowledgeMap/)
  assert.match(home, /<PublicKnowledgeSection/)
  assert.match(home, /knowledgeSummary\.overall/)
  assert.doesNotMatch(home, /IpdStageDetail|IpdFlowOverview|PortalSpaceGrid|MySpacesSection|PortalFilters|PublicKnowledgeSpaceCard|viewMode|visibleLimit/)
  assert.match(map, /space\.stages\.includes\(stage\)/)
  assert.match(summary, /space\.knowledge_base_count/)
  assert.match(summary, /space\.file_count/)
  assert.match(publicSection, /space\.category===PUBLIC_KNOWLEDGE_CATEGORY/)
  assert.match(publicSection, /variant="normal"/)
})

test('discoverable space cannot switch tenant or expose content actions', () => {
  const home = source('./PortalHome.vue')
  const summary = source('../../components/portal/SpaceSummary.vue')
  assert.match(home, /if\(space\.access_state!=='accessible'\)\{openAccessDialog\(space\);return\}/)
  assert.match(home, /if\(space\.access_state==='discoverable'\)accessSpace\.value=space/)
  assert.match(summary, /v-if="space\.access_state === 'accessible'"/)
  assert.doesNotMatch(summary, /v-if="isActiveSpace"/)
  assert.match(summary, /@click="\$emit\('search', space\)"/)
  assert.match(summary, /@click="\$emit\('ask', space\)"/)
})

test('sidebar selector is sourced from authorized spaces and not Portal discovery results', () => {
  const sidebar = source('../../components/SidebarNavigation.vue')
  assert.match(sidebar, /<SpaceSwitcher/)
  assert.match(sidebar, /<TenantSelector/)
  assert.doesNotMatch(sidebar, /portal\.spaces|overviewSpaces|listPortalSpaces/)
})

test('admin interaction and configuration APIs remain constrained', () => {
  const api = source('../../api/portal.ts')
  assert.match(api, /resolvePortalInteraction[\s\S]*post<[\s\S]*interaction\/resolve`\)/)
  assert.doesNotMatch(api, /interaction\/resolve`,\s*\{[^}]*organization_id/)
  assert.match(api, /spaces\/\$\{tenantId\}\/\$\{action\}/)
  const editor = source('../../components/portal/PortalConfigEditor.vue')
  assert.doesNotMatch(editor, /emit\('save',[\s\S]{0,300}status:/)
  assert.match(editor, /@click="\$emit\('status','publish'\)"/)
  for (const group of ['basicTitle', 'positioningTitle', 'displayTitle', 'accessTitle']) {
    assert.match(editor, new RegExp(`portal\\.admin\\.groups\\.${group}`))
  }
})

test('portal remains primary navigation and default authenticated landing', () => {
  const registry = source('../../config/navigation.ts')
  const home = source('./PortalHome.vue')
  const redirect = source('../../utils/authRedirect.ts')
  assert.match(registry, /id:\s*['"]portal['"][^\n]*route:\s*['"]\/portal['"]/)
  assert.match(home, /<Menu\s*\/>/)
  assert.match(redirect, /DEFAULT_AUTHENTICATED_LANDING = ['"]\/portal['"]/)
})
