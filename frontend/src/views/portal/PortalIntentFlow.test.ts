import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

test('current accessible spaces execute actions directly without switching', () => {
  const home = source('./PortalHome.vue')
  assert.match(home, /<GlobalCommandPalette \/>/)
  assert.match(home, /if\(isActiveSpace\(space\)\)/)
  assert.match(home, /if\(action==='search'\)commandPalette\.openPalette\(''\)/)
  assert.match(home, /menuStore\.setPrefillQuery\(''\);void router\.push\('\/platform\/creatChat'\)/)
  assert.match(home, /if\(isActiveSpace\(space\)\)\{void router\.push\('\/platform\/knowledge-bases'\);return\}/)
})

test('space action keys do not bubble into the card entry action', () => {
  const summary = source('../../components/portal/SpaceSummary.vue')
  assert.match(summary, /class="accessible-actions" @click\.stop @keydown\.stop/)
})

test('non-active accessible actions persist enum intent and use the shared explicit switch target', () => {
  const home = source('./PortalHome.vue')
  assert.match(home, /createPortalIntent\(action,space\.tenant_id\)/)
  assert.match(home, /targetPath:'\/platform\/knowledge-bases'/)
  assert.match(home, /onNavigationFailure:handlePortalSwitchFailure/)
  assert.match(home, /if\(space\.access_state!=='accessible'\)\{openAccessDialog\(space\);return\}/)
})

test('platform layout refreshes authorization and consumes without another tenant switch', () => {
  const platform = source('../platform/index.vue')
  assert.match(platform, /await authStore\.refreshFromAuthMe\(\)/)
  assert.match(platform, /consumePortalIntent\(/)
  assert.match(platform, /commandPaletteStore\.openPalette\(''\)/)
  assert.match(platform, /router\.push\('\/platform\/creatChat'\)/)
  assert.doesNotMatch(platform, /switchWorkspaceAndNavigate/)
})
