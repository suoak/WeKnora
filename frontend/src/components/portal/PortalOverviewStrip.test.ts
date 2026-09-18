import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./PortalOverviewStrip.vue', import.meta.url), 'utf8')

test('overview strip removes the redundant subtitle and renders one centralized asset summary', () => {
  assert.match(source, /class="overview-strip"/)
  assert.match(source, /formatAssetSummary\(metrics, t, numberFormatter\)/)
  assert.doesNotMatch(source, /portalExperience\.scaleLabel/)
  assert.doesNotMatch(source, /class="kpi-icon"|<dl|<dt|<dd/)
  assert.match(source, /class="asset-summary"/)
  assert.doesNotMatch(source, /searchPlaceholder|quickActions|heroTitle/)
})
