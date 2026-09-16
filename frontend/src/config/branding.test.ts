import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { branding } from './branding'

test('KnowHub release branding is centralized', () => {
  assert.equal(branding.productName, 'KnowHub')
  assert.equal(branding.productNameZh, 'CSBU研发知识库')
  assert.equal(branding.tagline, 'Unified Knowledge Hub for R&D')
  assert.equal(branding.taglineZh, '研发统一知识库')
  assert.equal(branding.capabilityLine, 'Unified Search · AI Q&A · Knowledge Governance')
  assert.equal(branding.capabilityLineZh, '统一检索 · 智能问答 · 知识治理')
  assert.equal(branding.logoMarkPath, '/brand/knowhub-mark.svg')
  assert.equal(branding.logoMarkDarkPath, '/brand/knowhub-mark-dark.svg')
  assert.equal(branding.logoMarkInversePath, '/brand/knowhub-mark-inverse.svg')
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
  const userMenu = readFileSync(new URL('../components/UserMenu.vue', import.meta.url), 'utf8')
  const brandLogo = readFileSync(new URL('../components/BrandLogo.vue', import.meta.url), 'utf8')
  const manifest = readFileSync(new URL('../../public/site.webmanifest', import.meta.url), 'utf8')

  assert.match(indexHtml, /<title>CSBU研发知识库<\/title>/)
  assert.match(embedHtml, /<title>CSBU研发知识库<\/title>/)
  assert.doesNotMatch(login, /assets\/img\/weknora\.png/)
  assert.doesNotMatch(menu, /assets\/img\/weknora\.png/)
  assert.match(login, /<BrandLogo inverse\s*\/>/)
  assert.match(menu, /<BrandLogo class="logo" portal-lockup inverse\s*\/>/)
  assert.doesNotMatch(userMenu, /general\.helpAndDocs/)
  assert.doesNotMatch(userMenu, /common\.github/)
  assert.match(brandLogo, /branding\.productNameZh/)
  assert.match(brandLogo, /branding\.productName/)
  assert.match(brandLogo, /branding\.logoMarkPath/)
  assert.match(brandLogo, /branding\.logoMarkDarkPath/)
  assert.match(brandLogo, /branding\.logoMarkInversePath/)
  assert.match(brandLogo, /locale\.value === 'zh-CN'/)
  assert.doesNotMatch(brandLogo, />KnowHub</)
  assert.deepEqual(JSON.parse(manifest), {
    name: 'KnowHub · CSBU研发知识库',
    short_name: 'CSBU研发知识库',
    description: '研发统一知识库：统一检索、智能问答与知识治理',
    start_url: '/',
    display: 'standalone',
    background_color: '#F3F3F3',
    theme_color: '#101F38',
    icons: [
      { src: '/brand/knowhub-app-icon-192.png', sizes: '192x192', type: 'image/png' },
      { src: '/brand/knowhub-app-icon-512.png', sizes: '512x512', type: 'image/png' },
    ],
  })
})

test('release surfaces keep product branding separate from compatibility identifiers', () => {
  const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')
  const readBinary = (path: string) => readFileSync(new URL(path, import.meta.url))
  const readme = read('../../../README.md')
  const docsConfig = read('../../../website-docs/.vitepress/config.mts')
  const docsLanding = read('../../../website-docs/.vitepress/theme/Landing.vue')
  const miniApp = JSON.parse(read('../../../miniprogram/app.json'))
  const miniProject = JSON.parse(read('../../../miniprogram/project.config.json'))
  const desktop = JSON.parse(read('../../../cmd/desktop/wails.json'))
  const desktopUpdater = read('../../../cmd/desktop/update.go')

  assert.match(readme, /docs\/brand\/knowhub-logo\.svg/)
  assert.match(docsConfig, /siteTitle: '知汇 KnowHub'/)
  assert.match(docsLanding, /知汇 KnowHub · 基于 WeKnora/)
  assert.match(docsLanding, /github\.com\/suoak\/WeKnora/)
  assert.equal(miniApp.window.navigationBarTitleText, '知汇')
  assert.equal(miniProject.projectname, '知汇 KnowHub')
  assert.equal(desktop.info.productName, '知汇 KnowHub')
  assert.equal(desktop.name, 'WeKnora Lite', 'desktop technical identity stays upgrade-compatible')
  assert.match(desktopUpdater, /repos\/suoak\/WeKnora\/releases\/latest/)
  assert.ok(readBinary('../../public/favicon.ico').byteLength > 1000)
  assert.ok(readBinary('../../../cmd/desktop/build/appicon.png').byteLength > 1000)
})
