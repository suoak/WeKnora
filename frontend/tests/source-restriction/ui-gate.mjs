import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createServer } from 'vite';
import { chromium } from '@playwright/test';

test('real source notice and action menu distinguish held/revoked/unknown/available', async () => {
  const server = await createServer({ server: { host: '127.0.0.1', port: 19487 }, logLevel: 'error' });
  let browser;
  try {
    await server.listen();
    const port = server.httpServer.address().port;
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();
    await page.route('**/*', route => {
      const url = new URL(route.request().url());
      return url.hostname === '127.0.0.1' && !url.pathname.startsWith('/api/') ? route.continue() : route.abort();
    });
    for (const [state, text] of [
      ['hold', '知识库中存在待核验来源'], ['revoked', '知识库中存在已撤销授权的来源'], ['unknown', '暂时无法确认来源状态'],
    ]) {
      await page.goto(`http://127.0.0.1:${port}/tests/source-restriction/index.html?state=${state}`);
      await page.getByRole('status').waitFor();
      assert.ok((await page.getByRole('status').innerText()).includes(text));
      if (state !== 'unknown') {
        assert.ok((await page.getByRole('status').innerText()).includes('历史处理状态待核验'));
        assert.equal(await page.locator('.doc-action-menu-item').count(), 1, 'only the existing delete action remains');
        const rebuild = await page.evaluate(() => document.body.dataset.rebuildLabel);
        assert.equal(await page.getByText(rebuild, { exact: true }).count(), 0);
      }
    }
    await page.goto(`http://127.0.0.1:${port}/tests/source-restriction/index.html?state=available`);
    await page.locator('.doc-action-menu-item').first().waitFor();
    assert.equal(await page.getByRole('status').count(), 0);
    assert.equal(await page.locator('.doc-action-menu-item').count(), 3);
    const rebuild = await page.evaluate(() => document.body.dataset.rebuildLabel);
    await page.getByText(rebuild, { exact: true }).click();
    assert.equal(await page.evaluate(() => document.body.dataset.reparse), 'emitted');
  } finally { await browser?.close(); await server.close(); }
});
