import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const settings = readFileSync(new URL('../settings/Settings.vue', import.meta.url), 'utf8')
const home = readFileSync(new URL('./SystemAdminHome.vue', import.meta.url), 'utf8')
const models = readFileSync(new URL('../settings/ModelSettings.vue', import.meta.url), 'utf8')
const overview = readFileSync(new URL('./usage/UsageOverview.vue', import.meta.url), 'utf8')
const spaces = readFileSync(new URL('./usage/UsageSpaces.vue', import.meta.url), 'utf8')
const knowledgeBases = readFileSync(new URL('./usage/UsageKnowledgeBases.vue', import.meta.url), 'utf8')

test('system administration has a lightweight task landing and preserves section deep links', () => {
  for (const section of [
    'usage-analytics',
    'models',
    'runtime-queues',
    'system-audit-log',
    'platform-api-keys',
    'system-global',
  ]) {
    assert.match(home, new RegExp(`section: '${section}'`))
    assert.match(settings, new RegExp(`['"]${section}['"]`))
  }
  assert.match(settings, /currentSection === 'system-admin'/)
  assert.match(settings, /syncSettingsRoute\(section\)/)
})

test('model inventory separates system built-ins from workspace models', () => {
  assert.match(models, /key: 'builtin' as const/)
  assert.match(models, /models: filteredModels\.value\.filter\(model => model\.isBuiltin\)/)
  assert.match(models, /key: 'workspace' as const/)
  assert.match(models, /models: filteredModels\.value\.filter\(model => !model\.isBuiltin\)/)
  assert.match(models, /group\.key === 'workspace' && authStore\.hasRole\('admin'\)/)
  assert.match(models, /class="default-policy-card"/)
})

test('analytics prioritizes adoption and governance over technical volume', () => {
  const primary = overview.match(/kpi-grid--primary[\s\S]*?<\/div>/)?.[0] || ''
  assert.match(primary, /active_tenants/)
  assert.match(primary, /active_knowledge_bases/)
  assert.match(primary, /cross_tenant_usage/)
  assert.match(primary, /mcp_platform_penetration/)
  assert.doesNotMatch(primary, /total_tokens/)

  assert.match(spaces, /row\.mcp_adopted/)
  assert.match(spaces, /row\.kb_used_count/)
  assert.match(knowledgeBases, /usedBySpaces/)
  assert.match(knowledgeBases, /externalSpaces/)
  assert.doesNotMatch(knowledgeBases, /cross_tenant_share\) \}\}/)
})

test('system administration landing collapses to one column on narrow screens', () => {
  assert.match(home, /@media \(max-width: 760px\)[\s\S]*?grid-template-columns: 1fr/)
})
