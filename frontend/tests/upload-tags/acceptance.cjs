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
  let browser, page;
  const pageErrors = [];
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
    if (config.mode === 'diagnose') {
      const prior = JSON.parse(fs.readFileSync(path.join(config.output, 'dev-acceptance.json')));
      result.documents = [];
      for (const doc of prior.documents || []) {
        const detail = (await api(owner, 'GET', '/api/v1/knowledge/' + doc.id, undefined, tenant)).data;
        result.documents.push({ id: detail.id, status: detail.parse_status, error: detail.error_message });
      }
      result.result = 'passed'; return;
    }
    const model = (await api(owner, 'POST', '/api/v1/models', { name: 'Upload tag acceptance embedding', type: 'Embedding', source: 'remote', parameters: { base_url: config.modelBaseURL || '', api_key: 'synthetic-only', embedding_parameters: { dimension: 3 }, provider: 'generic' } }, tenant)).data.id;
    const summary = (await api(owner, 'POST', '/api/v1/models', { name: 'Upload tag acceptance summary', type: 'KnowledgeQA', source: 'remote', parameters: { base_url: config.modelBaseURL || '', api_key: 'synthetic-only', provider: 'generic' } }, tenant)).data.id;
    const kb = (await api(owner, 'POST', '/api/v1/knowledge-bases', {
      name: 'Upload tags acceptance ' + config.mode + ' ' + Date.now(), type: 'document',
      indexing_strategy: { vector_enabled: false, keyword_enabled: false }, embedding_model_id: model, summary_model_id: summary,
      storage_provider_config: { provider: 'local' }, chunking_config: { chunk_size: 512, chunk_overlap: 20 }, wiki_config: { enabled: false },
      question_generation_config: { enabled: false },
    }, tenant)).data.id;
    result.kb_id = kb;
    const targetFolder = 'Tag destination/Nested destination';
    if (config.mode !== 'before') {
      await api(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/knowledge/folders', { parent_path: '', name: 'Tag destination' }, tenant);
      await api(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/knowledge/folders', { parent_path: 'Tag destination', name: 'Nested destination' }, tenant);
    }
    browser = await chromium.launch({ headless: true });
    const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
    await context.addInitScript(({ owner, tenant }) => {
      if (localStorage.getItem('upload_tags_fixture')) return;
      localStorage.setItem('weknora:contextual-guide-kb-detail:v1', '1');
      for (const [key, value] of Object.entries({ weknora_token: owner.token, weknora_user: JSON.stringify(owner.user), weknora_tenant: JSON.stringify(owner.active_tenant || owner.tenant), weknora_memberships: JSON.stringify(owner.memberships), weknora_selected_tenant_id: String(tenant), locale: 'zh-CN', 'weknora:new-user-guide-done:v1': '1', upload_tags_fixture: '1' })) localStorage.setItem(key, value);
    }, { owner, tenant });
    page = await context.newPage();
    page.on('pageerror', error => pageErrors.push(String(error.message)));
    await page.goto(config.base + '/platform/knowledge-bases/' + kb, { waitUntil: 'domcontentloaded', timeout: 180000 });
    const files = Array.from({ length: 11 }, (_, index) => ({ name: 'tag-acceptance-' + String(index + 1).padStart(2, '0') + '.txt', mimeType: 'text/plain', buffer: Buffer.from('Upload tag acceptance document ' + (index + 1) + '. Independent synthetic content only.') }));
    const directory = path.join(config.output, 'fixture-' + kb);
    for (const [index, file] of files.entries()) {
      const folder = path.join(directory, index % 2 ? 'level-a/level-b' : 'level-a');
      fs.mkdirSync(folder, { recursive: true }); fs.writeFileSync(path.join(folder, file.name), file.buffer);
    }
    if (config.mode !== 'before') {
      const expand = page.locator('.kb-folder-row[title="Tag destination"] .kb-folder-row__toggle');
      if ((await expand.getAttribute('aria-label')).includes('展开')) await expand.click();
      await page.locator('.kb-folder-row').filter({ hasText: 'Nested destination' }).click();
    }
    const open = async () => {
      if (config.mode === 'before') await page.locator('.doc-filter-actions input[type=file]:not([webkitdirectory])').setInputFiles(files);
      else await page.locator('.doc-filter-actions input[webkitdirectory]').setInputFiles(directory);
      await expect(page.locator('.upload-confirm-modal')).toBeVisible({ timeout: 30000 });
      if (config.mode !== 'before') {
        await page.locator('.upload-confirm-modal .nav-item').filter({ hasText: '文档摘要' }).click();
        const summarySwitch = page.locator('[data-section="summary"] .t-switch');
        if ((await summarySwitch.getAttribute('class')).includes('t-is-checked')) await summarySwitch.click();
        await page.locator('.upload-confirm-modal .nav-item').filter({ hasText: '文档标签' }).click();
      }
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
      let prematureUploads = 0;
      page.on('request', request => { if (request.method() === 'POST' && request.url().includes('/knowledge/file')) prematureUploads++; });
      await page.locator('.upload-confirm-modal input[type=file]:not([webkitdirectory])').setInputFiles(path.resolve(__dirname, '../../../internal/officeguard/testdata/encrypted.xlsx'));
      await expect(page.getByText(/打开密码/).last()).toBeVisible();
      await expect(page.locator('.files-count').first()).toHaveText('11');
      assert.equal(prematureUploads, 0);
      passed('encrypted Office file is rejected before upload without losing the eleven valid files');
      await expect(picker.locator('.tag-picker-more')).toHaveCount(0);
      await expect(picker.getByRole('button', { name: '新建标签', exact: true })).toHaveCount(0);
      const search = picker.locator('input[type=text]').first();
      const tagPattern = new RegExp('/knowledge-bases/' + kb + '/tags(?:\\?|$)');
      await page.route(tagPattern, route => route.request().method() === 'GET' ? route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ message: 'Synthetic list failure' }) }) : route.continue());
      await search.fill('offline');
      await expect(picker.getByRole('alert')).toBeVisible();
      await expect(picker.getByText('暂无标签', { exact: false })).toHaveCount(0);
      await page.unroute(tagPattern);
      await search.clear();
      await expect(picker.getByText('暂无标签', { exact: false })).toBeVisible();
      passed('failed listing is distinct from empty and can recover in place');
      for (const failure of ['server', 'timeout']) {
        await page.route(tagPattern, route => route.request().method() !== 'POST' ? route.continue() : failure === 'timeout' ? route.abort('timedout') : route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ message: 'Synthetic create failure' }) }));
        await search.fill('Retry tag');
        await picker.locator('.tag-picker-create').click();
        await expect(search).toBeEnabled();
        await expect(search).toHaveValue('Retry tag');
        await expect(page.locator('.files-count').first()).toHaveText('11');
        await page.unroute(tagPattern);
      }
      passed('server failure and request timeout retain input and all files');
      const create = async (name, race = false, enter = false) => {
        await search.fill(name);
        await expect(picker.locator('.tag-picker-create')).toBeVisible();
        if (race) await api(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name: name.trim() }, tenant);
        if (enter) await search.press('Enter');
        else await picker.locator('.tag-picker-create').click();
        await expect(search).toHaveValue('');
        await expect(picker.getByText(name.trim(), { exact: true })).toBeVisible();
      };
      await create('  First tag  ', false, true);
      passed('create the first tag from an empty knowledge base');
      await create('Second tag');
      await create('Third tag', true);
      passed('real HTTP 409 is recovered by querying and selecting the winning tag');
      await expect(picker.locator('.t-checkbox.t-is-checked')).toHaveCount(3);
      await expect(page.locator('.files-count').first()).toHaveText('11');
      await expect(page.locator('.destination-crumb')).toContainText('Nested destination');
      await expect(page.locator('.t-message')).toHaveCount(0, { timeout: 15000 });
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
        assert.ok(detail.folder_path.startsWith(targetFolder + '/'));
        assert.ok(detail.folder_path.includes('level-a'));
        result.documents.push({ id: detail.id, status: detail.parse_status, folder: detail.folder_path, tags: detail.tags.map(tag => tag.id) });
      }
      passed('all eleven uploaded documents have exactly the three selected persistent tag relations');
      passed('multi-level directory paths and the selected destination survive tag creation');
      if (config.modelBaseURL) {
        for (let attempt = 0; attempt < 90; attempt++) {
          const details = await Promise.all(result.documents.map(async document => (await api(owner, 'GET', '/api/v1/knowledge/' + document.id, undefined, tenant)).data));
          result.parse_results = details.map(detail => ({ id: detail.id, status: detail.parse_status, error: detail.error_message }));
          if (details.every(detail => detail.parse_status === 'completed')) break;
          if (details.some(detail => detail.parse_status === 'failed')) break;
          await new Promise(resolve => setTimeout(resolve, 1000));
        }
        assert.ok(result.parse_results.every(detail => detail.status === 'completed'), JSON.stringify(result.parse_results));
        passed('all eleven real uploaded files complete parsing and indexing with the isolated model fixture');
      }
      await page.reload({ waitUntil: 'domcontentloaded' });
      const parts = result.documents[0].folder.split('/');
      for (let index = 0; index < parts.length; index++) {
        const folderPath = parts.slice(0, index + 1).join('/');
        const target = page.locator('.kb-folder-row').and(page.locator('[title=' + JSON.stringify(folderPath) + ']'));
        if (index === parts.length - 1) await target.click();
        else {
          const toggle = target.locator('.kb-folder-row__toggle');
          if (await toggle.getAttribute('aria-expanded') === 'false') await toggle.click();
        }
      }
      await expect(page.getByText('tag-acceptance-', { exact: false }).first()).toBeVisible({ timeout: 30000 });
      await page.screenshot({ path: path.join(config.output, 'uploaded.png'), fullPage: true });
      passed('refresh retains documents and catalog');
      await page.locator('button[aria-controls="document-filters"]').click();
      await page.locator('.doc-filter-tag').filter({ hasText: 'First tag' }).click();
      await expect(page.locator('.doc-filter-tag').filter({ hasText: 'First tag' })).toHaveClass(/t-is-checked/);
      const filtered = (await api(owner, 'GET', '/api/v1/knowledge-bases/' + kb + '/knowledge?page_size=100&tag_ids=' + ids[0], undefined, tenant)).data;
      assert.equal(filtered.length, 11);
      passed('real browser and API tag filtering work after refresh');
      await page.locator('button[aria-controls="document-filters"]').click();
      const kbInfo = (await api(owner, 'GET', '/api/v1/knowledge-bases/' + kb, undefined, tenant)).data;
      for (const mode of ['url', 'manual', 'reparse']) {
        await page.evaluate(({ mode, kbInfo, kb, documentId, tagId }) => {
          const store = document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('uploadConfirm');
          window.uploadTagModeResult = null;
          const options = { mode: mode === 'url' ? 'file' : mode, kbInfo, tagIds: [tagId], urls: mode === 'url' ? ['https://example.invalid/synthetic-import'] : [], manual: mode === 'manual' ? { kbId: kb, title: 'Synthetic manual confirmation', content: 'Synthetic content for confirmation only.' } : undefined, reparse: mode === 'reparse' ? { knowledgeId: documentId, fileName: 'synthetic.txt' } : undefined };
          store.open(options).then(value => { window.uploadTagModeResult = value }).catch(() => {});
        }, { mode, kbInfo, kb, documentId: result.documents[0].id, tagId: ids[0] });
        await expect(page.locator('.upload-confirm-modal')).toBeVisible();
        if (mode === 'reparse') {
          await expect(page.locator('.upload-confirm-modal .knowledge-tag-picker')).toHaveCount(0);
          await page.locator('.upload-confirm-modal .close-btn').click();
        } else {
          const sharedPicker = page.locator('.upload-confirm-modal .knowledge-tag-picker');
          await sharedPicker.locator('input[type=text]').fill(mode + ' quick tag');
          await sharedPicker.locator('.tag-picker-create').click();
          await expect(sharedPicker.locator('input[type=text]')).toHaveValue('');
          await page.locator('.upload-confirm-modal .modal-footer').getByRole('button').last().click();
          await expect.poll(() => page.evaluate(() => window.uploadTagModeResult?.tagIds?.length)).toBe(2);
        }
        await expect(page.locator('.upload-confirm-modal')).toBeHidden();
      }
      passed('URL and manual confirmation create tags; reparse confirmation keeps tag creation hidden');
      for (const name of (config.mode === 'dev' ? ['', '   '] : ['', '   ', '\u0000', 'x'.repeat(129)])) {
        const response = await request(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name }, tenant);
        assert.equal(response.status, 400);
      }
      const duplicates = await Promise.all(Array.from({ length: 8 }, () => request(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name: 'Concurrent tag' }, tenant)));
      result.concurrent_statuses = duplicates.map(response => response.status);
      assert.equal(duplicates.filter(response => response.status === 200).length, 1);
      if (config.mode !== 'dev') assert.ok(duplicates.every(response => [200, 409].includes(response.status)));
      passed('invalid names and eight concurrent same-name API creations');
      const secondAccount = config.accounts.applicant;
      let second = await api(null, 'POST', '/api/v1/auth/login', { email: secondAccount.email, password: secondAccount.password });
      const members = (await api(owner, 'GET', '/api/v1/tenants/' + tenant + '/members?page_size=100&q=' + encodeURIComponent(second.user.email), undefined, tenant)).data.members;
      const membership = members.find(member => member.user_id === second.user.id);
      const originalRole = membership?.role;
      if (!membership) await api(owner, 'POST', '/api/v1/tenants/' + tenant + '/members', { email: second.user.email, role: 'contributor' }, tenant);
      else await api(owner, 'PUT', '/api/v1/tenants/' + tenant + '/members/' + second.user.id, { role: 'contributor' }, tenant);
      try {
        second = await api(null, 'POST', '/api/v1/auth/login', { email: secondAccount.email, password: secondAccount.password });
        const ownKb = (await api(second, 'POST', '/api/v1/knowledge-bases', { name: 'Ordinary member upload tag acceptance ' + Date.now(), type: 'document', indexing_strategy: { vector_enabled: false, keyword_enabled: false }, embedding_model_id: model, summary_model_id: summary, storage_provider_config: { provider: 'local' }, chunking_config: { chunk_size: 512, chunk_overlap: 20 }, wiki_config: { enabled: false } }, tenant)).data.id;
        const memberContext = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
        await memberContext.addInitScript(({ actor, tenant }) => {
          if (localStorage.getItem('upload_tags_fixture')) return;
          for (const [key, value] of Object.entries({ weknora_token: actor.token, weknora_user: JSON.stringify(actor.user), weknora_tenant: JSON.stringify(actor.active_tenant || actor.tenant), weknora_memberships: JSON.stringify(actor.memberships), weknora_selected_tenant_id: String(tenant), locale: 'zh-CN', 'weknora:new-user-guide-done:v1': '1', 'weknora:contextual-guide-kb-detail:v1': '1', upload_tags_fixture: '1' })) localStorage.setItem(key, value);
        }, { actor: second, tenant });
        const memberPage = await memberContext.newPage();
        await memberPage.goto(config.base + '/platform/knowledge-bases/' + ownKb, { waitUntil: 'domcontentloaded', timeout: 180000 });
        await memberPage.locator('.doc-filter-actions input[type=file]:not([webkitdirectory])').setInputFiles(files.slice(0, 1));
        const memberPicker = memberPage.locator('.upload-confirm-modal .knowledge-tag-picker');
        await memberPicker.locator('input[type=text]').fill('Ordinary member tag');
        await memberPicker.locator('.tag-picker-create').click();
        await expect(memberPicker.locator('.t-checkbox.t-is-checked')).toHaveCount(1);
        await memberPage.screenshot({ path: path.join(config.output, 'ordinary-member.png') });
        await memberPage.locator('.upload-confirm-modal .close-btn').click();
        passed('ordinary contributor with legitimate upload access creates a tag in the real browser');
        const foreignTag = (await api(second, 'GET', '/api/v1/knowledge-bases/' + ownKb + '/tags', undefined, tenant)).data.data[0];
        const invalidAssociation = await request(owner, 'POST', '/api/v1/knowledge-bases/' + kb + '/knowledge/manual', { title: 'Foreign tag must fail', content: 'Synthetic draft', status: 'draft', tag_ids: [foreignTag.id] }, tenant);
        assert.equal(invalidAssociation.status, 400);
        passed('cross-knowledge-base tag association is rejected');
        await api(owner, 'PUT', '/api/v1/tenants/' + tenant + '/members/' + second.user.id, { role: 'admin' }, tenant);
        const managed = (await api(second, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name: 'Management acceptance' }, tenant)).data;
        await api(second, 'PUT', '/api/v1/knowledge-bases/' + kb + '/tags/' + managed.id, { name: 'Management renamed' }, tenant);
        await api(second, 'DELETE', '/api/v1/knowledge-bases/' + kb + '/tags/' + managed.id, undefined, tenant);
        passed('a non-owner administrator can create, rename and delete an unused tag');
        await api(owner, 'PUT', '/api/v1/tenants/' + tenant + '/members/' + second.user.id, { role: 'viewer' }, tenant);
        for (const [method, suffix, data] of [['POST', '', { name: 'Denied' }], ['PUT', '/' + ids[0], { name: 'Denied' }], ['DELETE', '/' + ids[0], undefined]]) {
          const denied = await request(second, method, '/api/v1/knowledge-bases/' + kb + '/tags' + suffix, data, tenant);
          assert.equal(denied.status, 403);
        }
        await memberPage.goto(config.base + '/platform/knowledge-bases/' + kb, { waitUntil: 'domcontentloaded', timeout: 180000 });
        await expect(memberPage.locator('.document-header')).toBeVisible();
        await expect(memberPage.locator('.doc-filter-actions')).toHaveCount(0);
        passed('viewer cannot upload, create, rename or delete tags');
        const crossTenant = await request(second, 'POST', '/api/v1/knowledge-bases/' + kb + '/tags', { name: 'Cross-tenant denied' }, second.user.tenant_id);
        assert.ok([403, 404].includes(crossTenant.status));
        passed('cross-tenant create is denied');
        await memberContext.close();
      } finally {
        if (originalRole) await api(owner, 'PUT', '/api/v1/tenants/' + tenant + '/members/' + second.user.id, { role: originalRole }, tenant);
        else await api(owner, 'DELETE', '/api/v1/tenants/' + tenant + '/members/' + second.user.id, undefined, tenant);
      }
    }
    result.result = 'passed';
  } catch (error) { result.result = 'failed'; result.error = String(error.stack || error); result.page_errors = pageErrors; if (page) await page.screenshot({ path: path.join(config.output, config.mode + '-failure.png') }).catch(() => {}); process.exitCode = 1; }
  finally {
    if (browser) await browser.close();
    fs.writeFileSync(path.join(config.output, config.mode + '-acceptance.json'), JSON.stringify(result, null, 2));
    console.log(JSON.stringify(result));
  }
})();
