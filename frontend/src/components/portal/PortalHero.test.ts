import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('hero presents published Portal scale with a permission tooltip', () => {
  const hero = source('./PortalHero.vue')
  assert.match(hero, /props\.stats\.spaces/)
  assert.match(hero, /props\.stats\.knowledgeBases/)
  assert.match(hero, /props\.stats\.files/)
  assert.match(hero, /portalExperience\.scaleTooltip/)
  assert.doesNotMatch(hero, /my spaces|accessible count|global search all/i)
})

test('hero search names the active-space scope and delegates to the command palette', () => {
  const hero = source('./PortalHero.vue')
  const home = source('../../views/portal/PortalHome.vue')
  assert.match(hero, /portalExperience\.currentScope/)
  assert.match(hero, /portalExperience\.noActiveScope/)
  assert.match(home, /function openScopedSearch\(\)\{if\(requireActiveSpace\(\)\)commandPalette\.openPalette\(''\)\}/)
  assert.match(home, /MessagePlugin\.warning\(t\('portalExperience\.selectSpaceFirst'\)\)/)
})

test('quick actions remain lightweight and route through existing capabilities', () => {
  const hero = source('./PortalHero.vue')
  const home = source('../../views/portal/PortalHome.vue')
  for (const action of ['search', 'browse', 'agent', 'tools']) assert.match(hero, new RegExp(`quickActions\\.${action}`))
  assert.match(home, /openScopedRoute\('\/platform\/knowledge-bases'\)/)
  assert.match(home, /openScopedRoute\('\/platform\/agents'\)/)
  assert.match(home, /section=mcp-access-keys/)
  assert.doesNotMatch(home, /class="task-card"|loadResourceOverview|listKnowledgeBases|listAgents/)
})
