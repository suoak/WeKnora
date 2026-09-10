import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(join(here, 'UsageAnalytics.vue'), 'utf8')
const overviewSource = readFileSync(join(here, 'usage/UsageOverview.vue'), 'utf8')
const spacesSource = readFileSync(join(here, 'usage/UsageSpaces.vue'), 'utf8')
const knowledgeBasesSource = readFileSync(join(here, 'usage/UsageKnowledgeBases.vue'), 'utf8')
const apiSource = readFileSync(join(here, '../../api/usage-analytics.ts'), 'utf8')
const enLocaleSource = readFileSync(join(here, '../../i18n/locales/en-US.ts'), 'utf8')

test('passes token class and MCP direction filters to analytics queries', () => {
  assert.match(viewSource, /usage_class:\s*activeTab\.value === ["']token["']/)
  assert.match(viewSource, /direction:\s*activeTab\.value === ["']mcp["']/)
  assert.match(apiSource, /usage_class\?: ["']foreground["'] \| ["']background["']/)
  assert.match(apiSource, /direction\?: ["']inbound["'] \| ["']outbound["']/)
})

test('loads the backend-classified operation distribution with overview data', () => {
  assert.match(viewSource, /getUsageOperations\(query\.value\)/)
  assert.match(viewSource, /operations\.value\s*=\s*distribution\.data\s*\|\|\s*\[\]/)
  assert.match(apiSource, /\/operations\$\{params\(query\)\}/)
})

test('shows foreground and background totals returned by the backend', () => {
  assert.match(apiSource, /foreground_tokens/)
  assert.match(apiSource, /background_tokens/)
})

test('renders governance KPIs, statuses, filters, and zero-usage capable queries', () => {
  for (const field of [
    'active_knowledge_bases',
    'cross_tenant_usage',
    'inactive_knowledge_bases',
    'mcp_platform_penetration',
    'attention_needed',
  ]) {
    assert.match(overviewSource, new RegExp(field))
  }
  assert.match(viewSource, /include_inactive:\s*activeTab\.value === ["']spaces["']\s*\?\s*true/)
  assert.match(viewSource, /group_by:\s*activeTab\.value === ["']knowledgeBases["']\s*\?\s*["']knowledge_base["']/)
  assert.match(spacesSource, /insufficient_data/)
  assert.match(knowledgeBasesSource, /callerDistribution/)
  assert.match(knowledgeBasesSource, /metric:\s*["']accesses["']/)
})

test('keeps governance locale keys complete', () => {
  for (const key of [
    'active',
    'low_activity',
    'inactive',
    'never_used',
    'insufficient_data',
    'usedBySpaces',
    'attentionNeeded',
  ]) {
    assert.match(enLocaleSource, new RegExp(`${key}:`))
  }
})

test('labels Memory and Wiki operations and keeps unsupported meters explicit', () => {
  for (const operation of [
    'memory_extraction',
    'memory_consolidation',
    'memory_topic_resolution',
    'wiki_ingestion',
    'wiki_generation',
    'wiki_modification',
  ]) {
    assert.match(enLocaleSource, new RegExp(`${operation}:`))
  }
  assert.match(enLocaleSource, /Not yet included: Embedding, Rerank, VLM, and ASR/)
})
