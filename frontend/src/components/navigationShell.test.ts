import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('desktop, collapsed, and mobile drawer render the same navigation registry', () => {
  const menu = source('./menu.vue')
  const navigation = source('./SidebarNavigation.vue')
  assert.match(menu, /<SidebarNavigation :collapsed="sidebarCollapsed"/)
  assert.match(navigation, /visibleNavigationEntries/)
  assert.match(menu, /mobileOpen = true/)
  assert.match(menu, /mobile-nav-backdrop[^>]*@click="mobileOpen = false"/)
  assert.match(menu, /@media \(max-width: 768px\)/)
})

test('space context is first-class and searchable while platform shell has no fixed minimum width', () => {
  const menu = source('./menu.vue')
  const switcher = source('./SpaceSwitcher.vue')
  const shell = source('../views/platform/index.vue')
  assert.ok(menu.indexOf('<SpaceSwitcher') < menu.indexOf('<SidebarNavigation'))
  assert.match(switcher, /memberships\.length > 6/)
  assert.match(switcher, /switchWorkspaceAndNavigate/)
  assert.match(shell, /\.main\s*\{[\s\S]*?min-width:\s*0/)
})

test('Portal quick ask reuses the existing chat prefill and route', () => {
  const portal = source('../views/portal/PortalHome.vue')
  assert.match(portal, /menuStore\.setPrefillQuery\(question\)/)
  assert.match(portal, /router\.push\(['"]\/platform\/creatChat['"]\)/)
  assert.doesNotMatch(portal, /createSessions|\/home\/dashboard/)
})
