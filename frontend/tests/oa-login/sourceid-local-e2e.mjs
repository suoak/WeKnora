// Launched only by TestSourceIDLocalBrowserBusinessLogin. No real SID endpoints.
import { chromium, expect } from '@playwright/test'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'

const backend = new URL(process.env.SOURCEID_E2E_BACKEND)
const mock = new URL(process.env.SOURCEID_E2E_MOCK)
for (const url of [backend, mock]) {
  assert.equal(url.protocol, 'http:')
  assert.equal(url.hostname, '127.0.0.1')
}
const frontend = 'http://127.0.0.1:18195'
const browserOrigin = 'http://knowhub.ruijie.com.cn'
const server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '18195', '--strictPort'], {
  env: { ...process.env, VITE_DEV_PROXY_TARGET: backend.origin, __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS: 'knowhub.ruijie.com.cn' }, stdio: 'pipe',
})
let browser
const watchdog = setTimeout(() => {
  console.error('SOURCEID_SYNTHETIC_TIMEOUT')
  server.kill()
  void browser?.close()
  setTimeout(() => process.exit(1), 2000)
}, 150000)
const startup = []
server.stdout.on('data', chunk => { startup.push(chunk.toString()); if (startup.length > 12) startup.shift() })
server.stderr.on('data', chunk => { startup.push(chunk.toString()); if (startup.length > 12) startup.shift() })
try {
  console.log('SOURCEID_SYNTHETIC_STAGE: frontend_start')
  const readyDeadline = Date.now() + 30000
  while (true) {
    if (server.exitCode !== null) throw new Error(`Synthetic Vite startup failed: ${startup.join('')}`)
    try {
      const response = await fetch(frontend, { signal: AbortSignal.timeout(1000) })
      if (response.ok) break
    } catch { /* startup is polled with a strict deadline */ }
    if (Date.now() > readyDeadline) throw new Error(`Synthetic Vite not ready: ${startup.join('')}`)
    await new Promise(resolve => setTimeout(resolve, 200))
  }
  console.log('SOURCEID_SYNTHETIC_STAGE: browser_start')
  browser = await chromium.launch({
    executablePath: process.env.CHROME_PATH || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '/usr/bin/google-chrome'),
    headless: true, args: ['--no-proxy-server', '--disable-gpu', '--disable-dev-shm-usage', '--host-resolver-rules=MAP knowhub.ruijie.com.cn 127.0.0.1:18195'],
  })
  const context = await browser.newContext()
  // Chromium resolves this single approved domain directly to the local Vite
  // port. Keep the browser's normal response, cookie and navigation handling;
  // no URL/header rewriting or global hosts/DNS changes are involved.
  await context.route('**/*', async route => {
    const url = new URL(route.request().url())
    if ([browserOrigin, frontend, mock.origin].includes(url.origin)) return route.continue()
    return route.abort()
  })
  const page = await context.newPage()
  page.on('response', async response => {
    const url = new URL(response.url())
    if (!url.pathname.startsWith('/api/v1/auth/oidc/')) return
    const result = new URL(response.headers().location || '/', browserOrigin).hash
    const mark = result.startsWith('#oidc_error=') ? result.slice(1) : result.startsWith('#oidc_complete=') ? 'oidc_complete' : 'none'
    console.log(`SOURCEID_SYNTHETIC_AUTH: ${url.pathname} ${response.status()} ${mark}`)
  })
  page.on('pageerror', error => console.log(`SOURCEID_SYNTHETIC_PAGE_ERROR: ${error.name}`))
  console.log('SOURCEID_SYNTHETIC_STAGE: login_page')
  await page.goto(`${browserOrigin}/login`)
  await expect(page.locator('.oidc-button')).toBeVisible({ timeout: 90000 })
  await page.evaluate(() => localStorage.setItem('weknora_refresh_token', 'synthetic-stale-refresh'))
  const authorizationResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/auth/oidc/url')
  await page.locator('.oidc-button').click()
  const authResponse = await authorizationResponse
  // The SPA immediately navigates after consuming this response. Chromium may
  // discard its DevTools body during navigation; assert the resulting real
  // callback/session instead of reading the authorization URL a second time.
  assert.equal(authResponse.status(), 200)
  console.log('SOURCEID_SYNTHETIC_STAGE: authorization_started')
  await page.waitForURL(/\/portal(?:[/?#]|$)/, { timeout: 90000 })
  await expect(page.locator('.portal-shell')).toBeVisible({ timeout: 30000 })
  console.log('SOURCEID_SYNTHETIC_STAGE: portal_rendered')
  assert.equal(await page.evaluate(() => localStorage.getItem('weknora_refresh_token')), null)
  const api = (path, method = 'GET') => page.evaluate(async ({ path, method }) => {
    const response = await fetch(path, { method, headers: { Authorization: `Bearer ${localStorage.getItem('weknora_token')}` } })
    const body = response.ok ? await response.json() : null
    return { status: response.status, success: body?.success, tenant: body?.data?.tenant?.id, role: body?.data?.memberships?.[0]?.role, ids: Array.isArray(body?.data) ? body.data.map(item => item.id) : [] }
  }, { path, method })
  const me = await api('/api/v1/auth/me')
  assert.equal(me.status, 200); assert.equal(me.success, true)
  assert.equal(me.tenant, 42); assert.equal(me.role, 'contributor')
  await page.reload()
  await expect(page.locator('.portal-shell')).toBeVisible({ timeout: 30000 })
  const restored = await api('/api/v1/auth/me')
  assert.equal(restored.status, 200); assert.equal(restored.tenant, 42); assert.equal(restored.role, 'contributor')
  const knowledge = await api('/api/v1/knowledge-bases')
  assert.equal(knowledge.status, 200); assert.deepEqual(knowledge.ids, ['synthetic-kb'])
  const logout = await api('/api/v1/auth/logout', 'POST')
  assert.equal(logout.status, 200); assert.equal((await api('/api/v1/auth/me')).status, 401)
  await context.request.post(backend.origin + '/__synthetic/disable')
  await context.addCookies([{ name: 'synthetic_sid_sso', value: 'present', url: mock.origin }])
  await page.evaluate(() => { localStorage.removeItem('weknora_token'); localStorage.removeItem('weknora_refresh_token') })
  await page.goto(`${browserOrigin}/login`)
  await expect(page.locator('.oidc-button')).toBeVisible()
  await page.locator('.oidc-button').click()
  await page.waitForURL(/\/login(?:#.*)?$/, { timeout: 60000 })
  await expect(page.locator('.t-message')).toBeVisible()
  assert.equal(await page.evaluate(() => !!localStorage.getItem('weknora_token')), false)
  console.log('SOURCEID_LOCAL_BROWSER_PASS: portal, auth/me, restored session, contributor, fixture knowledge access, logout and disabled-SSO rejection')
} catch (error) {
  // Playwright's default timeout stack can include navigation URLs containing
  // even synthetic authorization codes. Preserve only a class and test line.
  const category = ['TimeoutError', 'AssertionError', 'Error'].includes(error?.name) ? error.name : 'Error'
  const line = /sourceid-local-e2e\.mjs:(\d+)/.exec(error?.stack || '')?.[1] || 'unknown'
  console.error(`SOURCEID_SYNTHETIC_FAILURE: ${category} test_line=${line}`)
  process.exitCode = 1
} finally {
  if (browser) await browser.close()
  server.kill()
  server.stdout.destroy()
  server.stderr.destroy()
  server.stdin.destroy()
  clearTimeout(watchdog)
}
