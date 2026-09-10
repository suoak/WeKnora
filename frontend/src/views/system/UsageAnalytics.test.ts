import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(join(here, 'UsageAnalytics.vue'), 'utf8')
const apiSource = readFileSync(join(here, '../../api/usage-analytics.ts'), 'utf8')
const enLocaleSource = readFileSync(join(here, '../../i18n/locales/en-US.ts'), 'utf8')

test('passes token class and MCP direction filters to analytics queries', () => {
  assert.match(viewSource, /usage_class: activeTab\.value === 'token'/)
  assert.match(viewSource, /direction: activeTab\.value === 'mcp'/)
  assert.match(apiSource, /usage_class\?: 'foreground' \| 'background'/)
  assert.match(apiSource, /direction\?: 'inbound' \| 'outbound'/)
})

test('loads the backend-classified operation distribution with overview data', () => {
  assert.match(viewSource, /getUsageOperations\(query\.value\)/)
  assert.match(viewSource, /operations\.value = distribution\.data \|\| \[\]/)
  assert.match(apiSource, /\/operations\$\{params\(query\)\}/)
})

test('shows foreground and background totals returned by the backend', () => {
  assert.match(viewSource, /overview\?\.foreground_tokens/)
  assert.match(viewSource, /overview\?\.background_tokens/)
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
