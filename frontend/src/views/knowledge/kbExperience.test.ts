import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  getKnowledgeBaseExperienceStatus,
  isKnowledgeBaseEditable,
  matchesKnowledgeBaseFilters,
} from './kbExperience'

const ready = {
  name: 'IPD Playbook',
  description: 'Product development lifecycle',
  isMine: true,
  embedding_model_id: 'emb-1',
  summary_model_id: 'llm-1',
  type: 'document',
}

test('searches name and description without case sensitivity', () => {
  assert.equal(matchesKnowledgeBaseFilters(ready, 'ipd', 'all', 'all'), true)
  assert.equal(matchesKnowledgeBaseFilters(ready, 'LIFECYCLE', 'all', 'all'), true)
  assert.equal(matchesKnowledgeBaseFilters(ready, 'finance', 'all', 'all'), false)
})

test('uses real share permissions for card action filtering', () => {
  assert.equal(isKnowledgeBaseEditable({ permission: 'editor' }), true)
  assert.equal(isKnowledgeBaseEditable({ permission: 'viewer' }), false)
  assert.equal(isKnowledgeBaseEditable({ isMine: true, editable: false }), false)
  assert.equal(matchesKnowledgeBaseFilters({ permission: 'viewer' }, '', 'readonly', 'all'), true)
  assert.equal(matchesKnowledgeBaseFilters({ isMine: false, permission: 'viewer' }, '', 'shared', 'all'), true)
  assert.equal(matchesKnowledgeBaseFilters({ isMine: true }, '', 'shared', 'all'), false)
})

test('presents processing and setup states before ready', () => {
  assert.equal(getKnowledgeBaseExperienceStatus({ is_processing: true }), 'processing')
  assert.equal(getKnowledgeBaseExperienceStatus({ type: 'document' }), 'setup')
  assert.equal(getKnowledgeBaseExperienceStatus(ready), 'ready')
})

test('list actions remain role-gated and cards open the detail route', () => {
  const source = readFileSync(new URL('./KnowledgeBaseList.vue', import.meta.url), 'utf8')
  const creationNavigation = readFileSync(
    new URL('../../hooks/useKnowledgeBaseCreationNavigation.ts', import.meta.url),
    'utf8',
  )
  assert.match(source, /v-if="authStore\.hasRole\('contributor'\)"/)
  assert.match(source, /<template v-if="canManageKBCard\(kb\)">/)
  assert.match(source, /const handleCardClick = \(kb: KB\) => \{[\s\S]*?goDetail\(kb\.id\)/)
  assert.match(creationNavigation, /router\.push\(`\/platform\/knowledge-bases\/\$\{kbId\}`\)/)
})

test('empty knowledge promotes upload and keeps secondary sources permission-gated', () => {
  const source = readFileSync(new URL('../../components/empty-knowledge.vue', import.meta.url), 'utf8')
  assert.match(source, /v-if="canEdit" class="empty-actions"/)
  assert.match(source, /theme="primary" @click="\$emit\('upload'\)"/)
  assert.match(source, /\$emit\('url'\)/)
  assert.match(source, /\$emit\('manual'\)/)
})

test('knowledge base list and create dialog have mobile breakpoints', () => {
  const list = readFileSync(new URL('./KnowledgeBaseList.vue', import.meta.url), 'utf8')
  const editor = readFileSync(new URL('./KnowledgeBaseEditorModal.vue', import.meta.url), 'utf8')
  assert.match(list, /@media \(max-width: 700px\)[\s\S]*?grid-template-columns: 1fr/)
  assert.match(editor, /@media \(max-width: 720px\)[\s\S]*?width: calc\(100vw - 24px\)/)
})

test('knowledge base cards expose clamped descriptions and the header follows current space context', () => {
  const source = readFileSync(new URL('./KnowledgeBaseList.vue', import.meta.url), 'utf8')
  assert.match(source, /\.card-description\s*\{[\s\S]*?-webkit-line-clamp: 2/)
  assert.match(source, /class="card-description" :title="kb\.description \|\| \$t\('knowledgeBase\.noDescription'\)"/)
  assert.match(source, /:title="shared\.knowledge_base\.description \|\| \$t\('knowledgeBase\.noDescription'\)"/)
  assert.match(source, /const knowledgeBasePageTitle = computed/)
  assert.match(source, /authStore\.currentTenantName\?\.trim\(\)/)
  assert.match(source, /knowledgeList\.workspaceTitle/)
  assert.match(source, /: t\('knowledgeBase\.title'\)/)
})

test('knowledge base polish keeps real permissions, filters, metadata, and restrained card sizing', () => {
  const source = readFileSync(new URL('./KnowledgeBaseList.vue', import.meta.url), 'utf8')
  assert.match(source, /v-if="authStore\.hasRole\('contributor'\)" theme="primary" class="kb-create-btn header-create-btn"/)
  assert.match(source, /v-model="kbSearchQuery"/)
  assert.match(source, /v-model="kbAccessFilter"/)
  assert.match(source, /v-model="kbStatusFilter"/)
  assert.match(source, /grid-template-columns: repeat\(auto-fill, minmax\(320px, 360px\)\)/)
  assert.match(source, /height: 168px;[\s\S]*?min-height: 168px/)
  assert.match(source, /min-height: 36px/)
  assert.match(source, /<t-icon name="home" \/>/)
  assert.match(source, /<t-icon name="secured" \/>/)
  assert.match(source, /<t-icon name="time" \/>/)
  assert.match(source, /background: var\(--td-bg-color-container\) !important/)
  assert.match(source, /color-mix\(in srgb, var\(--td-brand-color\) 32%, var\(--td-component-stroke\)\)/)
})
