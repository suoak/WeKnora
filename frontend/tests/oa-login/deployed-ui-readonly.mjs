// Real packaging-host screenshots. No SID navigation, login or API mutation.
import { chromium, expect } from '@playwright/test'
import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'

const phase = process.argv[2]
assert.ok(['before', 'after'].includes(phase))
const origin = 'http://172.30.232.42'
const output = new URL('../../../docs/qa/oa-login-ui-deployment-20261015/', import.meta.url)
await mkdir(output, { recursive: true })
const browser = await chromium.launch({
  executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe',
  headless: true, args: ['--no-proxy-server', '--disable-gpu'],
})
const rows = []
try {
  for (const [name, width, height, theme] of [['desktop', 1440, 1000, 'light'], ['mobile', 390, 844, 'light'], ['dark', 1440, 1000, 'dark']]) {
    const context = await browser.newContext({ viewport: { width, height }, reducedMotion: 'reduce', serviceWorkers: 'block' })
    await context.addInitScript(() => localStorage.setItem('locale', 'zh-CN'))
    let forbiddenNavigation = 0
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin) { forbiddenNavigation++; return route.abort() }
      if (url.pathname === '/api/v1/auth/oidc/url' || url.pathname === '/api/v1/auth/oidc/callback') {
        forbiddenNavigation++; return route.abort()
      }
      return route.continue()
    })
    const page = await context.newPage()
    const cdp = await context.newCDPSession(page)
    await cdp.send('Network.enable')
    await cdp.send('Network.setCacheDisabled', { cacheDisabled: true })
    const assets = []
    const pending = []
    let pageErrors = 0
    page.on('pageerror', () => { pageErrors++ })
    page.on('response', response => {
      const url = new URL(response.url())
      if (url.origin === origin && /\/assets\/(?:Login-|main-).*\.js$/.test(url.pathname)) {
        pending.push(response.body().then(body => assets.push({ path: url.pathname, status: response.status(), sha256: createHash('sha256').update(body).digest('hex') })))
      }
    })
    await page.goto(origin + '/login', { waitUntil: 'domcontentloaded', timeout: 60000 })
    const button = page.locator('.oidc-button')
    await expect(button).toBeVisible()
    await page.evaluate(theme => document.documentElement.setAttribute('theme-mode', theme), theme)
    await page.addStyleTag({ content: '*, *::before, *::after { animation:none!important; transition:none!important; }' })
    if (phase === 'after') {
      await expect(button).toHaveText('使用 OA 登录')
      await expect(button).toHaveClass(/oa-button/)
      await expect(page.locator('.oidc-divider')).toHaveText('或使用以下方式登录')
    }
    const layout = await page.evaluate(() => {
      const box = selector => {
        const { x, y, width, height } = document.querySelector(selector).getBoundingClientRect()
        return { x, y, width, height }
      }
      const style = getComputedStyle(document.querySelector('.oidc-button'))
      return { primary: box('.submit-button'), button: box('.oidc-button'), divider: box('.oidc-divider'),
        background: getComputedStyle(document.querySelector('.login-layout')).backgroundImage,
        primaryColor: getComputedStyle(document.querySelector('.submit-button')).backgroundColor,
        buttonBackground: style.backgroundColor, buttonBorder: style.borderColor, buttonColor: style.color,
        text: document.querySelector('.oidc-button').textContent.trim(),
        horizontalOverflow: document.documentElement.scrollWidth > innerWidth }
    })
    assert.equal(layout.horizontalOverflow, false)
    if (phase === 'after') {
      assert.ok(Math.abs(layout.primary.x - layout.button.x) < 1)
      assert.ok(Math.abs(layout.primary.width - layout.button.width) < 1)
      assert.equal(layout.primary.height, layout.button.height)
      assert.ok(layout.divider.y + layout.divider.height < layout.button.y)
    }
    const bounds = await page.locator('.form-card').boundingBox()
    const image = await cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true, clip: { ...bounds, scale: 1 } })
    await writeFile(new URL(`${phase}-${name}-card.png`, output), Buffer.from(image.data, 'base64'))
    await Promise.all(pending)
    assert.ok(assets.some(x => x.path.includes('/Login-')))
    assert.equal(pageErrors, 0)
    assert.equal(forbiddenNavigation, 0)
    rows.push({ name, phase, origin, cacheDisabled: true, proxyBypassed: true, layout, assets, pageErrors, sidRequests: 0, result: 'PASS' })
    await context.close()
  }
} finally {
  await browser.close()
  await writeFile(new URL(`${phase}-results.json`, output), JSON.stringify(rows, null, 2) + '\n')
}
console.log(JSON.stringify({ phase, cases: rows.length, result: 'PASS', realSIDLogin: 'NOT_EXECUTED' }))
