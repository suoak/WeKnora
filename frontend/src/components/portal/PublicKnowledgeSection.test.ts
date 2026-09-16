import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8')

test('public knowledge is driven only by the configured Portal category', () => {
  const section = source('./PublicKnowledgeSection.vue')
  assert.match(section, /space\.category===PUBLIC_KNOWLEDGE_CATEGORY/)
  assert.doesNotMatch(section, /includes\(.*name|公共库|项目库|standards|templates/i)
})

test('public section reuses normal SpaceSummary for accessible and discoverable spaces', () => {
  const section = source('./PublicKnowledgeSection.vue')
  assert.match(section, /<SpaceSummary v-for="space in publicSpaces"/)
  assert.match(section, /variant="normal"/)
  assert.match(section, /@enter="\$emit\('enter',\$event\)"/)
  assert.match(section, /@restricted="\$emit\('restricted',\$event\)"/)
  assert.doesNotMatch(section, /PortalSpaceCard|PublicKnowledgeSpaceCard/)
})

test('public section has independent loading and empty states', () => {
  const section = source('./PublicKnowledgeSection.vue')
  assert.match(section, /PortalSectionState v-if="error"/)
  assert.match(section, /v-else-if="loading" class="public-grid"/)
  assert.match(section, /v-else-if="publicSpaces\.length"/)
  assert.match(section, /portal\.publicZone\.empty/)
})

test('public section is a direct four-space grid without explanatory filler', () => {
  const section = source('./PublicKnowledgeSection.vue')
  assert.match(section, /portal\.spaceCount/)
  assert.match(section, /grid-template-columns:repeat\(4,minmax\(240px,1fr\)\)/)
  assert.doesNotMatch(section, /<aside>|publicZone\.description|section-kicker|public-surface/)
})
