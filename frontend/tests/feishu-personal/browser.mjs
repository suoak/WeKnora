import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { createServer } from 'vite'
import { chromium } from '@playwright/test'

const vite = await createServer({ server: { host: '127.0.0.1', port: 18196, strictPort: true } })
await vite.listen()
const origin = vite.resolvedUrls.local[0]
const localChrome = process.env.CHROME_PATH || (process.platform === 'win32' ? 'C:/Program Files/Google/Chrome/Application/chrome.exe' : '/usr/bin/google-chrome')
const browser = await chromium.launch({ headless: true, ...(existsSync(localChrome) ? { executablePath: localChrome } : {}) })
let count = 0
try {
  for (const locale of ['en-US', 'zh-CN']) {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } })
    await context.route('**/*', route => new URL(route.request().url()).origin === new URL(origin).origin ? route.continue() : route.abort())
    const page = await context.newPage()
    page.setDefaultTimeout(90000)
    const requests = []
    let fail = false
    await page.route('**/api/v1/**', route => {
      const u = new URL(route.request().url())
      assert.equal(u.pathname, '/api/v1/me/feishu/resources')
      requests.push({ folder: u.searchParams.get('folder'), cursor: u.searchParams.get('page_token') })
      if (fail) return route.fulfill({ status: 400, json: { error: 'feishu_operation_failed' } })
      const folder = u.searchParams.get('folder'), cursor = u.searchParams.get('page_token')
      return route.fulfill({ json: folder ? { items: [], has_more: false, next_page_token: '' } : {
        items: cursor ? [{ token: 'doc2', name: 'Second page', type: 'docx', url: 'https://team.feishu.cn/docx/doc2', importable: true }] : [
          { token: 'folder1', name: 'Synthetic folder', type: 'folder', importable: false },
          { token: 'doc1', name: 'Synthetic document', type: 'docx', url: 'https://team.feishu.cn/docx/doc1', importable: true },
          { token: 'unsupported1', name: 'Unsupported', type: 'board', importable: false },
        ], has_more: !cursor, next_page_token: cursor ? '' : 'opaque+/=',
      } })
    })
    await page.goto(origin + 'tests/feishu-personal/?locale=' + locale, { waitUntil: 'domcontentloaded', timeout: 120000 })
    const browse = page.getByRole('button', { name: locale === 'zh-CN' ? '浏览我的云盘' : 'Browse my Drive', exact: true })
    await browse.waitFor()
    assert.equal(requests.length, 0, 'mounting must not call external/API endpoints')
    await page.locator('#disable').click()
    assert.equal(await browse.isDisabled(), true)
    await page.locator('#disable').click()
    await browse.click()
    await page.getByText('Synthetic document', { exact: true }).waitFor()
    await page.getByRole('button', { name: locale === 'zh-CN' ? '选择' : 'Select', exact: true }).click()
    assert.equal(await page.locator('#selected').innerText(), 'https://team.feishu.cn/docx/doc1')
    assert.equal(requests.length, 1, 'selection must not create an import')
    await page.getByRole('button', { name: locale === 'zh-CN' ? '下一页' : 'Next page' }).click()
    await page.getByText('Second page', { exact: true }).waitFor()
    assert.equal(requests.at(-1).cursor, 'opaque+/=')
    await browse.click()
    await page.getByText('Synthetic folder', { exact: true }).waitFor()
    await page.getByRole('button', { name: locale === 'zh-CN' ? '打开文件夹' : 'Open folder' }).click()
    await page.getByText(locale === 'zh-CN' ? '此文件夹暂无文件。' : 'No files in this folder.').waitFor()
    assert.equal(requests.at(-1).folder, 'folder1')
    await page.getByRole('button', { name: locale === 'zh-CN' ? '返回上一级' : 'Back', exact: true }).click()
    await page.getByText('Synthetic document', { exact: true }).waitFor()
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    fail = true
    await browse.click()
    await page.getByRole('alert').waitFor()
    assert.equal(await page.getByText('Synthetic document', { exact: true }).count(), 0, 'failed page must not expose stale entries')
    await context.close()
    count++
  }
  console.log(JSON.stringify({ result: 'PASS', locales: count, real_feishu_calls: 0, real_sid_calls: 0, viewport: '390x844', cases: ['disabled', 'no_automatic_request', 'personal_page', 'select_without_write', 'pagination', 'folder_and_back', 'unsupported', 'failure_hides_stale_results'] }))
} finally {
  await browser.close()
  await vite.close()
}
