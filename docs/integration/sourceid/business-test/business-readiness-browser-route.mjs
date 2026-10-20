import { createRequire } from 'node:module'
import fs from 'node:fs'
const require = createRequire('E:/code/WeKnora/frontend/package.json')
const { chromium } = require('@playwright/test')
const result = { real_sid_called: false, production_called: false, results: [] }
let browser
try {
  browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true,
    args: ['--no-proxy-server', '--disable-gpu', '--host-resolver-rules=MAP knowhub.ruijie.com.cn 127.0.0.1:18190'] })
  const context = await browser.newContext()
  await context.route('**/*', route => {
    const u = new URL(route.request().url())
    const publicSIDMetadata = u.origin === 'https://sid.ruijie.com.cn' && ['/oidc/.well-known/openid-configuration', '/oidc/jwks'].includes(u.pathname) && !u.search
    return u.origin === 'http://knowhub.ruijie.com.cn' || publicSIDMetadata ? route.continue() : route.abort()
  })
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page); await cdp.send('Network.enable')
  const responses = []
  cdp.on('Network.responseReceived', e => {
    const u = new URL(e.response.url)
    if (u.hostname === 'knowhub.ruijie.com.cn') responses.push({ path: u.pathname, status: e.response.status, ip: e.response.remoteIPAddress, port: e.response.remotePort })
  })
  cdp.on('Network.requestWillBeSent', e => {
    if (!e.redirectResponse) return
    const r = e.redirectResponse; const u = new URL(r.url)
    if (u.hostname === 'knowhub.ruijie.com.cn') responses.push({ path: u.pathname, status: r.status, ip: r.remoteIPAddress, port: r.remotePort })
  })
  await page.goto('http://knowhub.ruijie.com.cn/login', { waitUntil: 'networkidle', timeout: 40000 })
  await page.setViewportSize({ width: 390, height: 844 })
  result.horizontal_overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
  for (const path of ['/api/v1/auth/oidc/config', '/api/v1/auth/oidc/callback']) {
    await page.goto('http://knowhub.ruijie.com.cn' + path, { waitUntil: 'networkidle', timeout: 30000 })
  }
  result.results = responses.filter(x => x.path === '/login' || x.path.startsWith('/assets/') || x.path.startsWith('/api/v1/auth/oidc/'))
  const kinds = [responses.some(x => x.path === '/login'), responses.some(x => x.path.startsWith('/assets/')), responses.some(x => x.path === '/api/v1/auth/oidc/config'), responses.some(x => x.path === '/api/v1/auth/oidc/callback')]
  result.all_four_classes_observed = kinds.every(Boolean)
  result.all_connections_controlled_tunnel = responses.length > 0 && responses.every(x => x.ip === '127.0.0.1' && x.port === 18190)
  const metadata = await page.goto('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration', { waitUntil: 'domcontentloaded', timeout: 15000 })
  result.sid_public_metadata_https_status = metadata.status()
  result.real_sid_called = true // Public metadata only, no authorize/token/profile.
  result.real_authentication = false
  result.result = result.all_four_classes_observed && result.all_connections_controlled_tunnel && metadata.status() === 200 && !result.horizontal_overflow ? 'PASS' : 'FAIL'
} catch (e) { result.result = 'FAIL'; result.failure_class = e.name }
finally { await browser?.close(); fs.writeFileSync('docs/integration/sourceid/evidence/business-readiness-browser-route-20261017.json', JSON.stringify(result, null, 2)); console.log(JSON.stringify(result)) }
