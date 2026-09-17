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
  assert.match(source, /class="kpi-icon"><t-icon name="folder"/)
  assert.match(source, /class="kpi-icon"><t-icon name="book-open"/)
  assert.match(source, /class="kpi-icon"><t-icon name="file"/)
  assert.match(source, /font-size:21px;font-weight:740/)
  assert.match(source, /border-left:1px solid var\(--portal-line\)/)
  assert.match(source, /min-height:74px/)
  assert.doesNotMatch(source, /searchPlaceholder|quickActions|heroTitle/)
})
