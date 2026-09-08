import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('portal routes allow tenantless auth and admin route requires system admin', () => {
  const router = source('../../router/index.ts')
  assert.match(router, /path:\s*["']\/portal["'][\s\S]*?requiresAuth:\s*true[\s\S]*?requiresTenant:\s*false/)
  assert.match(router, /path:\s*["']\/portal\/admin["'][\s\S]*?requiresTenant:\s*false[\s\S]*?requiresSystemAdmin:\s*true/)
})

test('portal discovery does not import content APIs and tenant switch invalidates portal state', () => {
  const combined = [source('./PortalHome.vue'), source('../../stores/portal.ts'), source('../../components/portal/PortalSpaceCard.vue')].join('\n')
  for (const forbidden of ['knowledge-base', '@/api/agent', '@/stores/knowledge', '@/stores/chat', '@/api/datasource', '@/api/mcp']) {
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
  assert.match(panel, /固定授予 Viewer/)
  assert.doesNotMatch(panel, /roleOptions|t-select|requested_role/)
  assert.match(panel, /当前申请人账号不可用，无法授予空间权限/)
})

test('cards cover member, requestable, restricted, pending, suspended, and interaction states', () => {
  const card = source('../../components/portal/PortalSpaceCard.vue')
  for (const marker of ["access_state==='member'", "access_state==='pending'", "access_state==='suspended'", 'can_request_access', "interaction_action==='enter'"]) {
    assert.match(card, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  }
  assert.match(card, /受限空间/)
  assert.match(card, /current_role/)
})

test('my spaces are loaded independently of published portal results and requests refresh state', () => {
  const store = source('../../stores/portal.ts')
  assert.match(store, /listMyPortalSpaces\(\)/)
  assert.match(store, /Promise\.all\(\[loadStages\(\), loadMySpaces\(\), loadSpaces\(\)\]\)/)
  assert.match(store, /createPortalAccessRequest\(tenantId, reason\.trim\(\)\)[\s\S]*await loadSpaces\(\)/)
})

test('admin config uses system-only organization options and explicit transitions', () => {
  const admin = source('./SystemPortalSettings.vue')
  assert.match(admin, /listPortalOrganizationOptions\(\)/)
  assert.match(admin, /transitionAdminPortalStatus\(selectedId\.value,action\)/)
  assert.match(source('../../components/UserMenu.vue'), /平台管理 · 知识门户/)
})
