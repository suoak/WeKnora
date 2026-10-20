import { createRequire } from 'node:module'
import fs from 'node:fs'
const require = createRequire('E:/code/WeKnora/frontend/package.json')
const { chromium } = require('@playwright/test')
const result = { real_authentication: false, production_called: false, tunnel_used: false, daily_chrome_changed: false, results: [] }
let browser
try {
  browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true, args: ['--no-proxy-server', '--disable-gpu'] })
  const context = await browser.newContext()
  await context.route('**/*', route => {
    const u = new URL(route.request().url())
    return u.origin === 'http://knowhub.ruijie.com.cn' ? route.continue() : route.abort()
  })
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page)
  await cdp.send('Network.enable')
  const responses = []
  const observe = r => {
    const u = new URL(r.url)
    if (u.hostname !== 'knowhub.ruijie.com.cn') return
    const marker = Object.entries(r.headers).find(([k]) => k.toLowerCase() === 'x-knowhub-environment')?.[1]
    responses.push({ path: u.pathname, status: r.status, ip: r.remoteIPAddress, port: r.remotePort, environment: marker ?? 'NOT_CAPTURED' })
  }
  cdp.on('Network.responseReceived', e => observe(e.response))
  cdp.on('Network.requestWillBeSent', e => { if (e.redirectResponse) observe(e.redirectResponse) })
  await page.goto('http://knowhub.ruijie.com.cn/login', { waitUntil: 'networkidle', timeout: 40000 })
  result.desktop_login_visible = await page.locator('input[type="password"]').count() > 0
  await page.screenshot({ path: 'docs/integration/sourceid/evidence/company-entry-login-desktop-20261017.png' })
  await page.setViewportSize({ width: 390, height: 844 })
  result.mobile_horizontal_overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
  await page.screenshot({ path: 'docs/integration/sourceid/evidence/company-entry-login-mobile-20261017.png' })
  for (const path of ['/api/v1/auth/oidc/config', '/api/v1/auth/oidc/callback']) await page.goto('http://knowhub.ruijie.com.cn' + path, { waitUntil: 'networkidle', timeout: 30000 })
  result.results = responses.filter(r => r.path === '/login' || r.path.startsWith('/assets/') || r.path.startsWith('/api/v1/auth/oidc/'))
  result.all_four_classes_observed = ['/login', '/assets/', '/api/v1/auth/oidc/config', '/api/v1/auth/oidc/callback'].every(prefix => result.results.some(r => r.path.startsWith(prefix)))
  result.all_connections_packaging_80 = result.results.length > 0 && result.results.every(r => r.ip === '172.30.232.42' && r.port === 80 && r.environment === 'company-test-f40-db3029')
  // Check the direct APP port without any auth data or browser CORS override.
  const r = await context.request.get('http://knowhub.ruijie.com.cn:8080/health')
  result.app_8080 = { status: r.status(), environment: r.headers()['x-knowhub-environment'] }
  result.result = result.all_four_classes_observed && result.all_connections_packaging_80 && result.desktop_login_visible && !result.mobile_horizontal_overflow && result.app_8080.status === 200 && result.app_8080.environment === 'company-test-f40-db3029' ? 'PASS' : 'FAIL'
} catch (e) { result.result = 'FAIL'; result.failure_class = e.name }
finally {
  await browser?.close()
  fs.writeFileSync('docs/integration/sourceid/evidence/company-entry-browser-20261017.json', JSON.stringify(result, null, 2))
  console.log(JSON.stringify({ result: result.result, all_four_classes_observed: result.all_four_classes_observed, all_connections_packaging_80: result.all_connections_packaging_80, real_authentication: false, failure_class: result.failure_class }))
}
