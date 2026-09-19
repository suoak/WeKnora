import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./KnowledgeBase.vue', import.meta.url), 'utf8')

test('referenced documents navigate to their containing folder before opening details', () => {
  const resolveFolder = source.indexOf('await getKnowledgeDetails(targetId)')
  const selectFolder = source.indexOf('selectedFolderPath.value = detail.folder_path || ROOT_FOLDER_PATH')
  const openDetails = source.indexOf('openCardDetails(target)', selectFolder)

  assert.ok(resolveFolder >= 0, 'document details should be fetched to resolve folder_path')
  assert.ok(selectFolder > resolveFolder, 'the containing folder should be selected after details load')
  assert.ok(openDetails > selectFolder, 'the detail drawer should open after folder navigation')
})

test('a newer referenced-document navigation supersedes an older request', () => {
  assert.match(source, /const request = \+\+autoOpenRequest/)
  assert.match(source, /if \(request !== autoOpenRequest\) return;/)
})

test('knowledge base header exposes the active space with a safe empty fallback', () => {
  assert.match(source, /const knowledgeSpaceName = computed\(\(\) => authStore\.currentTenantName\?\.trim\(\) \|\| ''\)/)
  assert.match(source, /<template v-if="knowledgeSpaceName">[\s\S]*class="breadcrumb-space"[\s\S]*\{\{ knowledgeSpaceName \}\}/)
  assert.match(source, /<button type="button" class="breadcrumb-link" @click="handleNavigateToKbList">[\s\S]*navigation\.knowledgeBases/)
  assert.doesNotMatch(source, /class="document-subtitle"|kbContextDescription/)
  assert.match(source, /class="breadcrumb-link dropdown kb-name"/)
})

test('knowledge base breadcrumb uses one visual level while separators stay neutral', () => {
  assert.match(source, /\.document-breadcrumb\s*\{[\s\S]*?font-size: 16px;[\s\S]*?font-weight: 600;/)
  assert.match(source, /\.breadcrumb-space\s*\{[\s\S]*?font-size: 16px;[\s\S]*?font-weight: 600;/)
  assert.match(source, /\.breadcrumb-link\s*\{[\s\S]*?font-size: 16px;[\s\S]*?font-weight: 600;/)
  assert.match(source, /\.breadcrumb-current\s*\{[\s\S]*?font-size: 16px;[\s\S]*?font-weight: 600;/)
  assert.match(source, /\.breadcrumb-tab\s*\{[\s\S]*?font-size: 16px;[\s\S]*?font-weight: 600;/)
  assert.match(source, /\.breadcrumb-separator\s*\{[\s\S]*?color: var\(--td-text-color-placeholder\)/)
})

test('document date range shares the compact toolbar control contract', () => {
  assert.match(source, /<t-date-range-picker v-model="updatedTimeRange"/)
  assert.match(source, /class="doc-date-range doc-filter-field__control"/)
  assert.match(source, /\.doc-date-range\s*\{[\s\S]*?:deep\(\.t-range-input\)\s*\{[\s\S]*?height: 34px;/)
  assert.match(source, /:deep\(\.t-range-input__inner-separator\)/)
  assert.match(source, /:deep\(\.t-range-input__inner \.t-input__inner::placeholder\)/)
  assert.match(source, /watch\(\[selectedParseStatus, selectedSource, updatedTimeRange\]/)
})
