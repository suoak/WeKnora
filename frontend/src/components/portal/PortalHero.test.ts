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

test('legacy hero is no longer mounted on the Portal home', () => {
  const hero = source('./PortalHero.vue')
  const home = source('../../views/portal/PortalHome.vue')
  assert.match(hero, /portalExperience\.currentScope/)
  assert.match(hero, /portalExperience\.noActiveScope/)
  assert.doesNotMatch(home, /<PortalHero|openScopedSearch|requireActiveSpace/)
})

test('legacy hero quick actions are not duplicated on Portal home', () => {
  const hero = source('./PortalHero.vue')
  const home = source('../../views/portal/PortalHome.vue')
  for (const action of ['search', 'browse', 'agent', 'tools']) assert.match(hero, new RegExp(`quickActions\\.${action}`))
  assert.doesNotMatch(home, /openScopedRoute|quickActions\./)
  assert.doesNotMatch(home, /class="task-card"|loadResourceOverview|listKnowledgeBases|listAgents/)
})
