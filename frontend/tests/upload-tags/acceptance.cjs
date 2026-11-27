// Run only against a dedicated test environment, with credentials supplied on stdin.
const { chromium, expect } = require('@playwright/test');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

(async () => {
  let input = ''; for await (const chunk of process.stdin) input += chunk;
  const config = JSON.parse(input);
  assert.equal(new URL(config.base).hostname, '127.0.0.1');
  const result = { source_sha: config.source_sha, mode: config.mode, checks: [] };
  let browser;
  const request = async (actor, method, endpoint, data, tenant) => {
    const response = await fetch(config.base + endpoint, {
      signal: AbortSignal.timeout(30000), method, headers: { 'Content-Type': 'application/json', ...(actor ? { Authorization: 'Bearer ' + actor.token } : {}), ...(tenant ? { 'X-Tenant-ID': String(tenant) } : {}) },
      body: data === undefined ? undefined : JSON.stringify(data),
    });
    const body = await response.json();
    return { status: response.status, body };
  };
  const api = async (...args) => {
    const response = await request(...args);
    assert.ok(response.status < 300, 'API ' + args[2] + ' HTTP ' + response.status + ': ' + JSON.stringify(response.body).slice(0, 400));
    return response.body;
  };
  const passed = name => result.checks.push({ name, result: 'passed' });
  try {
    const account = config.accounts.owner;
    const owner = await api(null, 'POST', '/api/v1/auth/login', { email: account.email, password: account.password });
    const tenant = owner.user.tenant_id;
    const model = (await api(owner, 'POST', '/api/v1/models', { name: 'Upload tag acceptance embedding', type: 'Embedding', source: 'remote', parameters: { embedding_parameters: { dimension: 3 }, provider: 'generic' } }, tenant)).data.id;
    const summary = (await api(owner, 'POST', '/api/v1/models', { name: 'Upload tag acceptance summary', type: 'KnowledgeQA', source: 'remote', parameters: { provider: 'generic' } }, tenant)).data.id;
    const kb = (await api(owner, 'POST', '/api/v1/knowledge-bases', {
      name: 'Upload tags acceptance ' + config.mode + ' ' + Date.now(), type: 'document',
      indexing_strategy: { vector_enabled: false, keyword_enabled: false }, embedding_model_id: model, summary_model_id: summary,
      storage_provider_config: { provider: 'local' }, chunking_config: { chunk_size: 512, chunk_overlap: 20 }, wiki_config: { enabled: false },
    }, tenant)).data.id;
    result.kb_id = kb;
    browser = await chromium.launch({ headless: true });
    const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
    await context.addInitScript(({ owner, tenant }) => {
      if (localStorage.getItem('upload_tags_fixture')) return;
      for (const [key, value] of Object.entries({ weknora_token: owner.token, weknora_user: JSON.stringify(owner.user), weknora_tenant: JSON.stringify(owner.active_tenant || owner.tenant), weknora_memberships: JSON.stringify(owner.memberships), weknora_selected_tenant_id: String(tenant), locale: 'zh-CN', 'weknora:new-user-guide-done:v1': '1', upload_tags_fixture: '1' })) localStorage.setItem(key, value);
    }, { owner, tenant });
    const page = await context.newPage();
    await page.goto(config.base + '/platform/knowledge-bases/' + kb, { waitUntil: 'domcontentloaded' });
    const files = Array.from({ length: 11 }, (_, index) => ({ name: 'tag-acceptance-' + String(index + 1).padStart(2, '0') + '.txt', mimeType: 'text/plain', buffer: Buffer.from('Upload tag acceptance document ' + (index + 1) + '. Independent synthetic content only.') }));
    const open = async () => {
      await page.locator('input[type=file]:not([webkitdirectory])').first().setInputFiles(files);
      await expect(page.locator('.upload-confirm-modal')).toBeVisible({ timeout: 30000 });
    };
    await open();
    if (config.mode === 'before') {
      await page.locator('.upload-confirm-modal .t-select').first().click();
      await page.screenshot({ path: path.join(config.output, 'before.png'), fullPage: true });
      passed('original empty tag selector has no creation entry');
    } else {
      const picker = page.locator('.upload-confirm-modal .knowledge-tag-picker');
      await expect(picker).toBeVisible();
      await expect(picker.getByText('暂无标签', { exact: false })).toBeVisible();
      await expect(picker.locator('.tag-picker-more')).toHaveCount(0);
      await picker.getByRole('button', { name: '新建标签', exact: true }).click();
      const search = picker.locator('input').first();
      const create = async name => {
        await search.fill(name);
        await picker.locator('.tag-picker-create').click();
        await expect(search).toHaveValue('');
        await expect(picker.getByText(name.trim(), { exact: true })).toBeVisible();
      };
      await create('  First tag  ');
      passed('create the first tag from an empty knowledge base');
      await create('Second tag');
      await create('Third tag');
      await expect(picker.locator('.t-checkbox.t-is-checked')).toHaveCount(3);
      await expect(page.locator('.files-count').first()).toHaveText('11');
      await page.screenshot({ path: path.join(config.output, 'after.png'), fullPage: true });
      passed('continuous creation selects all tags and retains eleven files');
      await search.fill('First');
      await expect(picker.getByText('First tag', { exact: true })).toBeVisible();
      await expect(picker.locator('.tag-picker-create')).toBeVisible();
      await search.fill('First tag');
      await expect(picker.locator('.tag-picker-create')).toHaveCount(0);
      await search.clear();
      passed('search finds existing tags and excludes exact-name duplicates');
      const catalog = (await api(owner, 'GET', '/api/v1/knowledge-bases/' + kb + '/tags?page_size=100', undefined, tenant)).data.data;
      assert.equal(catalog.length, 3);
      const ids = catalog.map(tag => tag.id).sort();
      result.tag_ids = ids;
      // Cancel must preserve persisted tags while discarding the upload draft.
      await page.locator('.upload-confirm-modal .close-btn').click();
      await expect(page.locator('.upload-confirm-modal')).toBeHidden();
      assert.equal((await api(owner, 'GET', '/api/v1/knowledge-bases/' + kb + '/tags', undefined, tenant)).data.total, 3);
      await open();
      await expect(picker.locator('.t-checkbox.t-is-checked')).toHaveCount(0);
      for (const name of ['First tag', 'Second tag', 'Third tag']) await picker.getByText(name, { exact: true }).click();
      passed('cancel and reopen discard draft selection and retain created tags');
      await page.locator('.upload-confirm-modal .modal-footer').getByRole('button').last().click();
      await expect(page.locator('.upload-confirm-modal')).toBeHidden({ timeout: 30000 });
      let documents = [];
      for (let attempt = 0; attempt < 90; attempt++) {
        const response = await api(owner, 'GET', '/api/v1/knowledge-bases/' + kb + '/knowledge?page_size=100', undefined, tenant);
        documents = response.data || [];
        if (documents.length === 11) break;
        await new Promise(resolve => setTimeout(resolve, 1000));
      }
      assert.equal(documents.length, 11);
      result.documents = [];
      for (const document of documents) {
        const detail = (await api(owner, 'GET', '/api/v1/knowledge/' + document.id, undefined, tenant)).data;
        assert.deepEqual((detail.tags || []).map(tag => tag.id).sort(), ids);
        result.documents.push({ id: detail.id, status: detail.parse_status, tags: detail.tags.map(tag => tag.id) });
      }
      passed('all eleven uploaded documents have exactly the three selected persistent tag relations');
      await page.reload({ waitUntil: 'domcontentloaded' });
      await expect(page.getByText('tag-acceptance-01.txt', { exact: false }).first()).toBeVisible({ timeout: 30000 });
      await page.screenshot({ path: path.join(config.output, 'uploaded.png'), fullPage: true });
      passed('refresh retains documents and catalog');
      for (const name of ['', '   ', 'bad\u0000name', 'x'.repeat(129)]) {
        const response = await request(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name }, tenant);
        assert.equal(response.status, 400);
      }
      const duplicates = await Promise.all(Array.from({ length: 8 }, () => request(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name: 'Concurrent tag' }, tenant)));
      assert.equal(duplicates.filter(response => response.status === 200).length, 1);
      assert.ok(duplicates.every(response => [200, 409].includes(response.status)));
      passed('invalid names and eight concurrent same-name API creations');
    }
    result.result = 'passed';
  } catch (error) { result.result = 'failed'; result.error = String(error.stack || error); process.exitCode = 1; }
  finally {
    if (browser) await browser.close();
    fs.writeFileSync(path.join(config.output, config.mode + '-acceptance.json'), JSON.stringify(result, null, 2));
    console.log(JSON.stringify(result));
  }
})();
