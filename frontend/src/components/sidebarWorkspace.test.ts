import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (relative: string) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('workspace sidebar keeps fixed top and footer around one scroll area', () => {
  const menu = source('./menu.vue')
  assert.ok(menu.indexOf('class="logo_row"') < menu.indexOf('class="menu_top"'))
  assert.ok(menu.indexOf('class="menu_top"') < menu.indexOf('class="menu_bottom"'))
  assert.match(menu, /min-width: 250px;\s*width: 250px/)
  assert.match(menu, /&--collapsed \{[\s\S]*?min-width: 60px;\s*width: 60px/)
})

test('agents are expanded by default and shortcuts come from the existing agent store', () => {
  const navigation = source('./SidebarNavigation.vue')
  assert.match(navigation, /knowhub\.sidebar\.agents\.expanded/)
  assert.match(navigation, /localStorage\.getItem\(AGENTS_EXPANDED_KEY\)!=='false'/)
  assert.match(navigation, /chatResources\.ensureAgents\(\)/)
  assert.match(navigation, /\[\.\.\.chatResources\.agents\][\s\S]*\.slice\(0,4\)/)
  assert.match(navigation, /v-for="agent in shortcutAgents"/)
  assert.doesNotMatch(navigation, /快速问答|智能推理|维基问答|数据分析师/)
})

test('agent shortcut reuses Agent Center run flow and cannot bypass disabled state', () => {
  const navigation = source('./SidebarNavigation.vue')
  const agentList = source('../views/agent/AgentList.vue')
  assert.match(navigation, /:disabled="isAgentDisabled\(agent\.id\)"/)
  assert.match(navigation, /if\(!isAgentDisabled\(agentId\)\)void router\.push\(\{path:'\/platform\/agents',query:\{runAgent:agentId\}\}\)/)
  assert.match(agentList, /const requestedId = typeof route\.query\.runAgent/)
  assert.match(agentList, /if \(agent\.disabled_by_me\)/)
  assert.match(agentList, /await startAgentChat\(agent\.id\)/)
  assert.match(navigation, /openAllAgents[\s\S]*router\.push\('\/platform\/agents'\)/)
})

test('recent conversations default open, preserve API order, and show five until expanded', () => {
  const menu = source('./menu.vue')
  assert.match(menu, /const recentChatsExpanded = ref\(true\)/)
  assert.match(menu, /filteredGroupedSessions\.value\.flatMap\(\(group\) => group\.items\)/)
  assert.match(menu, /sessions\.slice\(0, 5\)/)
  assert.match(menu, /v-for="subitem in visibleRecentSessions"/)
  assert.match(menu, /subitem\.path === currentSecondpath/)
  assert.match(menu, /@navigate="gotopage\(subitem\.path\)"/)
  assert.match(menu, /if \(!showAllRecent\.value\) return/)
})

test('collapsed sidebar hides agent children and expands from the agent icon', () => {
  const navigation = source('./SidebarNavigation.vue')
  assert.match(navigation, /entry\.id === 'agents' && !collapsed/)
  assert.match(navigation, /entry\.id==='agents'&&props\.collapsed\)\{uiStore\.expandSidebar\(\);return\}/)
})

test('user footer displays account identity instead of repeating current space', () => {
  const userMenu = source('./UserMenu.vue')
  const info = userMenu.match(/<div class="user-info">([\s\S]*?)<\/div>\s*<t-icon/)?.[1] || ''
  assert.match(info, /\{\{ userName \}\}/)
  assert.match(info, /currentRoleLabel \|\| userEmail/)
  assert.doesNotMatch(info, /activeTenantName|user-tenant-name/)
})
