import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('portal routes allow tenantless auth and admin route requires system admin', () => {
  const router = source('../../router/index.ts')
  assert.match(router, /path:\s*["']\/portal["'][\s\S]*?requiresAuth:\s*true[\s\S]*?requiresTenant:\s*false/)
  assert.match(router, /path:\s*["']\/portal\/admin["'][\s\S]*?requiresTenant:\s*false[\s\S]*?requiresSystemAdmin:\s*true/)
})

test('portal homepage reuses only the approved lightweight content APIs and tenant switch invalidates portal state', () => {
  const combined = [source('./PortalHome.vue'), source('../../stores/portal.ts'), source('../../components/portal/PortalSpaceCard.vue')].join('\n')
  assert.match(combined, /listKnowledgeBases/)
  assert.match(combined, /listAgents/)
  for (const forbidden of ['@/stores/knowledge', '@/stores/chat', '@/api/datasource', '@/api/mcp']) {
    assert.doesNotMatch(combined, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))
  }
  assert.match(source('../../utils/tenantSwitch.ts'), /usePortalStore\(\)\.invalidate\(\)/)
})

test('interaction and status APIs use fixed routes and safe bodies', () => {
  const api = source('../../api/portal.ts')
  assert.match(api, /resolvePortalInteraction[\s\S]*post<[\s\S]*interaction\/resolve`\)/)
  assert.doesNotMatch(api, /interaction\/resolve`,\s*\{[^}]*organization_id/)
  assert.match(api, /spaces\/\$\{tenantId\}\/\$\{action\}/)
  const editor = source('../../components/portal/PortalConfigEditor.vue')
  assert.doesNotMatch(editor, /emit\('save',[\s\S]{0,300}status:/)
  assert.match(editor, /@click="\$emit\('status','publish'\)"/)
})

test('Owner approval surface is fixed Viewer and has no role selector', () => {
  const panel = source('../../components/portal/AccessRequestsPanel.vue')
  assert.match(panel, /portal\.requests\.description/)
  assert.doesNotMatch(panel, /roleOptions|t-select|requested_role/)
  assert.match(panel, /portal\.requests\.applicantUnavailable/)
})

test('cards show public asset scale while content actions remain accessible-only', () => {
  const card = source('../../components/portal/PortalSpaceCard.vue')
  for (const marker of ["access_state==='accessible'", 'access_request_pending', 'membership_suspended', 'can_request_access', "interaction_action==='enter'"]) {
    assert.match(card, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  }
  assert.match(card, /portal\.restricted/)
  assert.match(card, /current_role/)
  assert.match(card, /knowledge_base_count/)
  assert.match(card, /file_count/)
  assert.match(card, /v-if="space\.access_state==='accessible'"[^>]*@click="\$emit\('enter'/)
  assert.doesNotMatch(card, /v-if="space\.access_state==='accessible'" class="resource-counts"/)
})

test('sidebar selector is sourced from authorized spaces and not Portal discovery results', () => {
  const sidebar = source('../../components/SidebarNavigation.vue')
  assert.match(sidebar, /<SpaceSwitcher/)
  assert.match(sidebar, /<TenantSelector/)
  assert.doesNotMatch(sidebar, /portal\.spaces|overviewSpaces|listPortalSpaces/)
})

test('my spaces are loaded independently of published portal results and requests refresh state', () => {
  const store = source('../../stores/portal.ts')
  assert.match(store, /listMyPortalSpaces\(\)/)
  assert.match(store, /Promise\.all\(\[loadStages\(\), loadMySpaces\(\), loadOverviewSpaces\(\)\]\)/)
  assert.match(store, /overviewSpaces\.value = uniquePortalSpaces/)
  assert.match(store, /createPortalAccessRequest\(tenantId, reason\.trim\(\)\)[\s\S]*await loadSpaces\(\)/)
})

test('admin config uses system-only organization options and explicit transitions', () => {
  const admin = source('./SystemPortalSettings.vue')
  assert.match(admin, /listPortalOrganizationOptions\(\)/)
  assert.match(admin, /transitionAdminPortalStatus\(selectedId\.value,action\)/)
  assert.match(admin, /portalStore\.invalidate\(\)/)
  assert.match(source('../../components/UserMenu.vue'), /portal\.admin\.menuEntry/)
})

test('portal is a primary sidebar item instead of a personal-menu shortcut', () => {
  const registry = source('../../config/navigation.ts')
  const userMenu = source('../../components/UserMenu.vue')
  assert.match(registry, /id:\s*['"]portal['"][^\n]*route:\s*['"]\/portal['"]/)
  assert.doesNotMatch(userMenu, /handlePortal\s*=|@click="handlePortal"/)
})

test('portal always uses the primary workspace sidebar, including for tenantless users', () => {
  const home = source('./PortalHome.vue')
  assert.match(home, /<Menu\s*\/>/)
  assert.match(home, /import Menu from ['"]@\/components\/menu\.vue['"]/)
  assert.doesNotMatch(home, /portal-header|brand-mark|<UserMenu/)
})

test('portal provides separate IPD and public-knowledge discovery views', () => {
  const home = source('./PortalHome.vue')
  const publicCard = source('../../components/portal/PublicKnowledgeSpaceCard.vue')
  const publicPolicy = source('../../config/publicKnowledgeSpaces.ts')
  const stageDetail = source('../../components/portal/IpdStageDetail.vue')
  const flow = source('../../components/portal/IpdFlowOverview.vue')
  const scale = source('../../components/portal/KnowledgeScaleSummary.vue')
  const hierarchy = source('../../config/portalKnowledgeSummary.ts')
  assert.match(home, /viewMode==='ipd'/)
  assert.match(home, /viewMode==='public'/)
  assert.match(home, /PUBLIC_CATEGORY=PUBLIC_KNOWLEDGE_CATEGORY/)
  assert.match(home, /<IpdStageDetail/)
  assert.match(home, /<IpdFlowOverview[^>]*portal\.overviewSpaces/)
  assert.match(home, /portal\.publicZone\.title/)
  assert.match(home, /knowledgeSummary\.overall/)
  assert.match(home, /knowledgeSummary\.publicArea/)
  assert.match(stageDetail, /portal\.stageDetail\.resultCount/)
  assert.doesNotMatch(stageDetail, /activities|deliverables|recommended skills/i)
  assert.match(flow, /space\.stages\.includes\(stage\)/)
  assert.match(flow, /space\.knowledge_base_count/)
  assert.match(flow, /space\.file_count/)
  assert.match(flow, /slice\(0,4\)/)
  assert.match(flow, /summary\.ipd/)
  assert.match(flow, /summary\.phases\[stage\.key\]/)
  assert.doesNotMatch(flow, /knowledge-base|document|chunk|agent|datasource|mcp/i)
  assert.match(publicPolicy, /PUBLIC_KNOWLEDGE_CATEGORY = ['"]public_knowledge['"]/)
  assert.match(publicPolicy, /PUBLIC_KNOWLEDGE_PREVIEW_LIMIT = 6/)
  assert.match(publicPolicy, /space\.category === PUBLIC_KNOWLEDGE_CATEGORY/)
  assert.doesNotMatch(publicPolicy, /项目库空间|公共库空间|名词库空间/)
  assert.match(home, /visiblePublicKnowledgeSpaces\(portal\.spaces/)
  assert.match(home, /PublicKnowledgeSpaceCard v-for="space in visibleSpaces"/)
  assert.match(publicCard, /space\.display_name/)
  assert.match(publicCard, /space\.description/)
  assert.match(publicCard, /space\.knowledge_base_count/)
  assert.match(publicCard, /space\.file_count/)
  assert.match(scale, /Intl\.NumberFormat/)
  assert.match(hierarchy, /uniqueSpaces\(\[\.\.\.ipdSpaces, \.\.\.publicSpaces\]\)/)
  assert.doesNotMatch(home, /publicTypes|portal\.publicZone\.types/)
  assert.match(home, /portal\.spaces\.slice\(0,visibleLimit\.value\)/)
  assert.match(home, /portal\.loadMoreSpaces/)
})

test('admin settings group fields and use a creatable category selector', () => {
  const editor = source('../../components/portal/PortalConfigEditor.vue')
  for (const group of ['basicTitle', 'positioningTitle', 'displayTitle', 'accessTitle']) {
    assert.match(editor, new RegExp(`portal\\.admin\\.groups\\.${group}`))
  }
  assert.match(editor, /<t-select v-model="form\.category"[^>]*creatable/)
  assert.match(editor, /'public_knowledge'/)
  assert.match(editor, /portal\.categories\./)
})

test('default landing remains portal after higher-priority redirect cases', () => {
  const redirect = source('../../utils/authRedirect.ts')
  assert.match(redirect, /DEFAULT_AUTHENTICATED_LANDING = ['"]\/portal['"]/)
  const explicit = redirect.indexOf('if (explicitTarget) return explicitTarget')
  const captured = redirect.indexOf('if (storedTarget) return storedTarget')
  const lite = redirect.indexOf('if (options.liteMode')
  const fallback = redirect.lastIndexOf('return DEFAULT_AUTHENTICATED_LANDING')
  assert.ok(explicit >= 0 && explicit < captured && captured < lite && lite < fallback)
})
