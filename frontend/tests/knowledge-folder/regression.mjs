import { chromium, expect } from '@playwright/test'
import { createServer } from 'vite'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'

const server = await createServer({ configFile: false, root: fileURLToPath(new URL('../../', import.meta.url)), resolve: { alias: { '@': fileURLToPath(new URL('../../src', import.meta.url)) } }, optimizeDeps: { noDiscovery: true, include: ['vue', 'pinia', 'vue-router', 'vue-i18n', 'axios', 'tdesign-vue-next'] }, server: { host: '127.0.0.1', port: 45197, strictPort: true } })
await server.listen()
const port = server.httpServer.address().port
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) })
try {
  const page = await browser.newPage()
  let mode = 'success'
  let delayed
  const seen = []
  await page.route('**/api/v1/knowledge-bases/folder-regression/knowledge?**', async route => {
    const url = new URL(route.request().url())
    seen.push(url.searchParams)
    const status = mode === 'forbidden' ? 403 : mode === 'failure' ? 500 : 200
    if (mode === 'delayed') await new Promise(resolve => { delayed = resolve })
    if (mode === 'network') return route.abort('failed')
    return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(status !== 200
      ? { success: false, error: { message: 'injected regression failure' } }
      : mode === 'malformed' ? { success: false }
      : { success: true, total: mode === 'empty' ? 0 : 24, data: mode === 'empty' ? [] : Array.from({ length: 24 }, (_, i) => ({ id: `doc-${i}`, file_name: `纪要-${i}.docx`, folder_path: '拜访纪要', updated_at: '2026-11-09T00:00:00Z' })) }) })
  })
  await page.goto(`http://127.0.0.1:${port}/tests/knowledge-folder/index.html`, { waitUntil: 'domcontentloaded', timeout: 120000 })
  await page.waitForFunction(() => typeof window.loadFolder === 'function', undefined, { timeout: 120000 })
  const load = () => page.evaluate(() => window.loadFolder({ page: 1, page_size: 35, folder_path: '拜访纪要', sort_by: 'updated_at', sort_order: 'desc' }))
  await load()
  await expect(page.getByTestId('rows')).toHaveText('24')
  assert.equal(seen[0].get('folder_path'), '拜访纪要')
  assert.equal(seen[0].get('sort_by'), 'updated_at')
  for (const [next, expected] of [['failure', 'generic'], ['forbidden', 'forbidden'], ['network', 'network'], ['malformed', 'generic']]) {
    mode = next; await load()
    await expect(page.getByTestId('error')).toHaveText(expected)
    await expect(page.getByTestId('rows')).toHaveText('24')
  }
  mode = 'empty'; await load()
  await expect(page.getByTestId('error')).toBeEmpty()
  await expect(page.getByTestId('rows')).toHaveText('0')
  mode = 'delayed'
  const stale = load()
  await expect.poll(() => typeof delayed).toBe('function')
  mode = 'success'; await load(); delayed(); await stale
  await expect(page.getByTestId('error')).toBeEmpty()
  await expect(page.getByTestId('rows')).toHaveText('24')
  console.log('PASS: real catalog hook: Chinese/24 documents, server/permission/network/malformed failures, retry, empty folder, stale requests')
} finally { await browser.close(); await server.close() }

