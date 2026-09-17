import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./PortalOverviewStrip.vue', import.meta.url), 'utf8')

test('overview strip is compact and renders the three centralized asset totals', () => {
  assert.match(source, /class="overview-strip"/)
  assert.match(source, /metrics\.spaces/)
  assert.match(source, /metrics\.knowledgeBases/)
  assert.match(source, /metrics\.files/)
  assert.match(source, /portalExperience\.scaleLabel/)
  assert.match(source, /min-height:74px/)
  assert.doesNotMatch(source, /searchPlaceholder|quickActions|heroTitle/)
})
