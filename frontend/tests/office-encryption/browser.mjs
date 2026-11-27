import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { realpathSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from '@playwright/test'
const root = new URL('../../../internal/officeguard/testdata/', import.meta.url)
const fixture = name => fileURLToPath(new URL(name, root))
const frontend = fileURLToPath(new URL('../..', import.meta.url))
const server = await createServer({ configFile: false, root: frontend,
  plugins: [vue()], cacheDir: 'node_modules/.vite-office-guard',
  resolve: { alias: { '@': fileURLToPath(new URL('../../src', import.meta.url)) }, dedupe: ['vue', 'pinia'] },
  optimizeDeps: { entries: ['tests/office-encryption/harness.html'] },
  server: { host: '127.0.0.1', port: 5189, strictPort: true, fs: { allow: [fileURLToPath(new URL('../../..', import.meta.url)), realpathSync(new URL('../../node_modules', import.meta.url))] } } })
let browser
try {
  await server.listen()
  browser = await chromium.launch({ headless: true })
  const page = await browser.newPage()
  page.setDefaultTimeout(30000)
  let uploads = 0
  await page.route('**/*', route => {
    const url = new URL(route.request().url())
    if (url.pathname.startsWith('/api/')) {
      if (route.request().method() === 'POST' && url.pathname.endsWith('/knowledge/file')) uploads++
      return route.fulfill({ status: 200, json: { success: true, data: { id: 'synthetic-doc' } } })
    }
    if (url.hostname !== '127.0.0.1') return route.abort()
    return route.continue()
  })
  const errors = []
  page.on('pageerror', e => { errors.push(e.message); console.error('Browser error:', e.message) })
  await page.goto('http://127.0.0.1:5189/tests/office-encryption/harness.html')
  await page.locator('input[type=file]').first().setInputFiles([fixture('normal.docx'), fixture('encrypted.xlsx'), fixture('sheet-protected.xlsx')])
  await page.waitForFunction(() => document.querySelector('#status')?.textContent === 'normal.docx,sheet-protected.xlsx')
  assert.equal(uploads, 0, 'selection must not send upload requests')
  assert.match(await page.locator('body').innerText(), /已跳过 1 个受限文件/)
  await page.locator('#send').click()
  await page.waitForFunction(() => document.querySelector('#status')?.textContent === '上传完成')
  assert.equal(uploads, 2)
  const data = [...await readFile(fixture('vendor/example_password.docx'))]
  const result = await page.evaluate(bytes => window.uploadOfficeTest(bytes, 'renamed.txt'), data)
  assert.equal(result, 2400)
  assert.equal(uploads, 2, 'direct API wrapper call must also be blocked')
  for (const name of ['classic-irm.doc', 'classic-irm-viewer.doc', 'classic-irm-disguised.txt']) {
    const result = await page.evaluate(bytes => window.uploadOfficeTest(bytes, 'classic.doc'), [...await readFile(fixture(name))])
    assert.equal(result, 2400)
    assert.equal(uploads, 2, 'classic IRM must be rejected before HTTP')
  }
  assert.deepEqual(errors, [])
  console.log('PASS Chromium: local selection skips encrypted; protected files upload; API wrapper sends zero encrypted requests')
} finally {
  await browser?.close()
  await server.close()
}
