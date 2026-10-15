// Local UI checks only: every API is mocked and external requests are blocked.
// Starts/stops its own Vite on 127.0.0.1:18190.
// Run: node tests/oa-login/oa-login-ui.mjs [before|after]
import { chromium, expect } from '@playwright/test'
import { mkdir, writeFile, readFile } from 'node:fs/promises'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'

const phase = process.argv[2] || 'after'
assert.ok(['before', 'after'].includes(phase))
const origin = 'http://127.0.0.1:18190'
const output = new URL('../../../docs/qa/oa-login-ui-release-20261015/', import.meta.url)
await mkdir(output, { recursive: true })
const server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '18190', '--strictPort'], { stdio: 'pipe' })
await new Promise((resolve, reject) => {
  server.stdout.on('data', data => { if (data.toString().includes('Local:')) resolve() })
  server.on('error', reject)
  server.on('exit', code => reject(new Error(`Vite exited: ${code}`)))
})
const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : undefined),
  headless: true,
  args: ['--no-proxy-server', '--disable-gpu', '--disable-background-timer-throttling', '--disable-renderer-backgrounding'],
})
const results = []
const cases = [
  ['desktop-zh', 'zh-CN', 1440, 1000, 'light', '使用 OA 登录'],
  ['mobile-zh', 'zh-CN', 390, 844, 'light', '使用 OA 登录'],
  ['desktop-en-dark', 'en-US', 1440, 1000, 'dark', 'Sign in with OA'],
  ['mobile-en', 'en-US', 390, 844, 'light', 'Sign in with OA'],
]
try {
  for (const [name, locale, width, height, theme, label] of cases) {
    const context = await browser.newContext({ viewport: { width, height }, reducedMotion: 'reduce' })
    await context.addInitScript(({ locale }) => localStorage.setItem('locale', locale), { locale })
    let exchangeRequests = 0
    let passwordRequests = 0
    let releasePassword
    const passwordReleased = new Promise(resolve => { releasePassword = resolve })
    let releaseAuthorization
    const authorizationReleased = new Promise(resolve => { releaseAuthorization = resolve })
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin) return route.abort()
      if (url.pathname === '/api/v1/auth/oidc/url') {
        exchangeRequests++
        assert.equal(url.searchParams.get('redirect_uri'), 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback')
        await authorizationReleased
        return route.fulfill({ json: { success: true, authorization_url: `${origin}/__oa_ui_authorize` } })
      }
      if (url.pathname === '/api/v1/auth/login') {
        passwordRequests++
        assert.equal(route.request().method(), 'POST')
        const body = route.request().postDataJSON()
        assert.equal(body.email, 'oa-ui@example.invalid')
        assert.equal(body.password, 'SyntheticPassword123!')
        await passwordReleased
        return route.fulfill({ json: { success: false, message: 'Synthetic sign-in rejected' } })
      }
      if (url.pathname === '/__oa_ui_authorize') return route.fulfill({ contentType: 'text/html', body: '<p>Mock authorization destination</p>' })
      if (url.pathname === '/api/v1/auth/oidc/config') return route.fulfill({ json: {
        success: true, enabled: true, provider_display_name: '企业 SID',
        redirect_uri: 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback',
      } })
      if (url.pathname === '/api/v1/auth/config') return route.fulfill({ json: { success: true, registration_mode: 'invite_only' } })
      if (url.pathname.startsWith('/api/')) return route.fulfill({ json: { success: false, data: [] } })
      return route.continue()
    })
    const page = await context.newPage()
    await page.goto(`${origin}/login`)
    const oa = page.locator('button.oidc-button')
    await expect(oa).toBeVisible({ timeout: 90000 })
    await page.evaluate(theme => document.documentElement.setAttribute('theme-mode', theme), theme)
    await page.addStyleTag({ content: '*, *::before, *::after { animation: none !important; transition: none !important; }' })
    const cdp = await context.newCDPSession(page)
    const screenshot = await cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
    await writeFile(new URL(`${phase}-${name}.png`, output), Buffer.from(screenshot.data, 'base64'))
    const cardBounds = await page.locator('.form-card').boundingBox()
    const cardScreenshot = await cdp.send('Page.captureScreenshot', {
      format: 'png', captureBeyondViewport: true, clip: { ...cardBounds, scale: 1 },
    })
    await writeFile(new URL(`${phase}-${name}-card.png`, output), Buffer.from(cardScreenshot.data, 'base64'))
    const layout = await page.evaluate(() => {
      const box = selector => {
        const { x, y, width, height } = document.querySelector(selector).getBoundingClientRect()
        return { x, y, width, height }
      }
      const primary = document.querySelector('.submit-button')
      return {
        primary: box('.submit-button'), oa: box('.oidc-button'), divider: box('.oidc-divider'),
        background: getComputedStyle(document.querySelector('.login-layout')).backgroundImage,
        primaryColor: getComputedStyle(primary).backgroundColor,
        primaryRadius: getComputedStyle(primary).borderRadius,
        oaRadius: getComputedStyle(document.querySelector('.oidc-button')).borderRadius,
        horizontalOverflow: document.documentElement.scrollWidth > innerWidth,
      }
    })
    if (phase === 'after') {
      await expect(oa).toHaveText(label)
      await expect(page.locator('.oidc-divider')).toHaveText(locale === 'zh-CN' ? '或使用以下方式登录' : 'Or sign in with')
      assert.ok(Math.abs(layout.primary.x - layout.oa.x) < 1)
      assert.ok(Math.abs(layout.primary.width - layout.oa.width) < 1)
      assert.equal(layout.primary.height, layout.oa.height)
      assert.equal(layout.primaryRadius, layout.oaRadius)
      assert.ok(layout.primary.y + layout.primary.height <= layout.divider.y)
      assert.ok(layout.divider.y + layout.divider.height < layout.oa.y)
      assert.equal(layout.horizontalOverflow, false)
      const previous = JSON.parse(await readFile(new URL('before-results.json', output), 'utf8')).find(item => item.name === name)
      assert.equal(layout.background, previous.layout.background)
      assert.equal(layout.primaryColor, previous.layout.primaryColor)
      await page.locator('input[autocomplete="email"]').fill('oa-ui@example.invalid')
      await page.locator('input[autocomplete="current-password"]').fill('SyntheticPassword123!')
      await page.locator('.submit-button').click()
      await expect(oa).toBeDisabled()
      assert.equal(passwordRequests, 1)
      releasePassword()
      await expect(oa).toBeEnabled()
      const normalBackground = await oa.evaluate(el => getComputedStyle(el).backgroundColor)
      await oa.hover()
      await page.waitForTimeout(180)
      const hoverBackground = await oa.evaluate(el => getComputedStyle(el).backgroundColor)
      assert.notEqual(normalBackground, hoverBackground)
      await oa.focus()
      await page.keyboard.press('Tab')
      await page.keyboard.press('Shift+Tab')
      await expect(oa).toBeFocused()
      assert.notEqual(await oa.evaluate(el => getComputedStyle(el).outlineStyle), 'none')
      await oa.click()
      await expect(oa).toBeDisabled()
      await expect(oa).toHaveAttribute('aria-busy', 'true')
      await expect(oa).toHaveText(locale === 'zh-CN' ? '正在跳转到 OA...' : 'Redirecting to OA...')
      await oa.evaluate(el => { el.click(); el.click() })
      assert.equal(exchangeRequests, 1)
      releaseAuthorization()
      await page.waitForURL(`${origin}/__oa_ui_authorize`)
    }
    results.push({ name, layout, authorizationRequests: exchangeRequests, status: 'PASS' })
    releaseAuthorization()
    releasePassword()
    await context.close()
  }
  if (phase === 'after') {
    for (const [locale, label] of [['ja-JP', 'OAでログイン'], ['ko-KR', 'OA로 로그인'], ['ru-RU', 'Войти через OA']]) {
      const context = await browser.newContext()
      await context.addInitScript(locale => localStorage.setItem('locale', locale), locale)
      await context.route('**/*', route => {
        const url = new URL(route.request().url())
        if (url.origin !== origin) return route.abort()
        if (url.pathname === '/api/v1/auth/oidc/config') return route.fulfill({ json: { success: true, enabled: true, sourceid_basic_encoding: 'oauth_form' } })
        if (url.pathname.startsWith('/api/')) return route.fulfill({ json: { success: false, message: 'Synthetic authorization rejected' } })
        return route.continue()
      })
      const page = await context.newPage()
      await page.goto(`${origin}/login`)
      const oa = page.locator('button.oidc-button')
      await expect(oa).toHaveText(label)
      await oa.click()
      await expect(page.locator('.t-message')).toContainText('Synthetic authorization rejected')
      await expect(oa).toBeEnabled()
      assert.equal(new URL(page.url()).pathname, '/login')
      results.push({ name: `${locale}-error-recovery`, status: 'PASS' })
      await context.close()
    }
    for (const [provider, label] of [['Acme SSO', '?? Acme SSO ??'], ['', '?? OIDC ??']]) {
      const context = await browser.newContext()
      await context.addInitScript(() => localStorage.setItem('locale', 'zh-CN'))
      await context.route('**/*', route => {
        const url = new URL(route.request().url())
        if (url.origin !== origin) return route.abort()
        if (url.pathname === '/api/v1/auth/oidc/config') return route.fulfill({ json: { success: true, enabled: true, provider_display_name: provider } })
        if (url.pathname.startsWith('/api/')) return route.fulfill({ json: { success: false, message: 'Synthetic provider rejection' } })
        return route.continue()
      })
      const page = await context.newPage()
      await page.goto(`${origin}/login`)
      const button = page.locator('.oidc-button')
      await expect(button).toHaveText(label)
      await expect(page.locator('.oa-button')).toHaveCount(0)
      await button.click()
      await expect(page.locator('.t-message')).toContainText('Synthetic provider rejection')
      await expect(button).toBeEnabled()
      results.push({ name: provider ? 'other-provider-name-and-event' : 'generic-provider-fallback', status: 'PASS' })
      await context.close()
    }
    const context = await browser.newContext()
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      if (url.origin !== origin) return route.abort()
      if (url.pathname.startsWith('/api/')) return route.fulfill({ json: { success: true, enabled: false, registration_mode: 'invite_only' } })
      return route.continue()
    })
    const page = await context.newPage()
    await page.goto(`${origin}/login`)
    await expect(page.locator('.submit-button')).toBeVisible()
    await expect(page.locator('.oidc-button')).toHaveCount(0)
    await expect(page.locator('.oidc-divider')).toHaveCount(0)
    results.push({ name: 'oidc-disabled-password-entry', status: 'PASS' })
    await context.close()
  }
} finally {
  await browser.close()
  server.kill()
  await writeFile(new URL(`${phase}-results.json`, output), JSON.stringify(results, null, 2) + '\n', 'utf8')
}
console.log(JSON.stringify({ phase, cases: results.length, status: 'PASS' }))
