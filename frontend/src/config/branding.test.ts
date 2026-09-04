import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { branding } from './branding'

test('KnowHub release branding is centralized', () => {
  assert.equal(branding.productName, 'KnowHub')
  assert.equal(branding.productNameZh, '知汇')
  assert.equal(branding.tagline, 'Unified Knowledge Hub for R&D')
  assert.equal(branding.taglineZh, '研发统一知识库')
  assert.equal(branding.capabilityLine, 'Unified Search · AI Q&A · Knowledge Governance')
  assert.equal(branding.capabilityLineZh, '统一检索 · 智能问答 · 知识治理')
})

test('upstream identity remains explicit for compatibility and attribution', () => {
  assert.equal(branding.upstreamName, 'WeKnora')
  assert.equal(branding.upstreamRepositoryUrl, 'https://github.com/Tencent/WeKnora')
})

test('primary web surfaces consume the KnowHub brand', () => {
  const indexHtml = readFileSync(new URL('../../index.html', import.meta.url), 'utf8')
  const embedHtml = readFileSync(new URL('../../embed.html', import.meta.url), 'utf8')
  const login = readFileSync(new URL('../views/auth/Login.vue', import.meta.url), 'utf8')
  const menu = readFileSync(new URL('../components/menu.vue', import.meta.url), 'utf8')
  const brandLogo = readFileSync(new URL('../components/BrandLogo.vue', import.meta.url), 'utf8')

  assert.match(indexHtml, /<title>KnowHub<\/title>/)
  assert.match(embedHtml, /<title>KnowHub<\/title>/)
  assert.doesNotMatch(login, /assets\/img\/weknora\.png/)
  assert.doesNotMatch(menu, /assets\/img\/weknora\.png/)
  assert.match(login, /<BrandLogo\s*\/>/)
  assert.match(menu, /<BrandLogo class="logo"\s*\/>/)
  assert.match(brandLogo, /branding\.productNameZh/)
  assert.match(brandLogo, /branding\.productName/)
  assert.match(brandLogo, /locale\.value === 'zh-CN'/)
  assert.doesNotMatch(brandLogo, />KnowHub</)
})
