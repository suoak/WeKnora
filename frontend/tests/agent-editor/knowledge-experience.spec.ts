import { test, expect } from '@playwright/test'
import { fixtureApi, testAgent } from './fixtures'

test('folder width persists and document details show full summary and submitter', async ({ page }) => {
  const api = await fixtureApi(page)
  const kb = { id: 'local-kb', tenant_id: 1, creator_id: testAgent.creator_id, name: 'Isolated KB', type: 'document', chunking_config: {}, indexing_strategy: {}, capabilities: { vector: true, keyword: true, wiki: false } }
  const description = Array.from({ length: 20 }, (_, i) => `Summary paragraph ${i + 1}: complete document context.`).join('\n\n')
  const doc = { id: 'local-doc', knowledge_base_id: kb.id, title: 'Example.txt', file_name: 'Example.txt', file_type: 'txt', type: 'file', parse_status: 'completed', summary_status: 'completed', description, created_at: '2026-11-16T00:00:00Z', updated_at: '2026-11-16T00:00:00Z', tags: [], metadata: { submitter: { user_id: 'alice', name: 'Alice' } } }
  await page.addInitScript(() => localStorage.setItem('weknora.kb.docs.viewMode', 'list'))
  await page.route('**/api/v1/knowledge-bases/local-kb**', route => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path.endsWith('/knowledge/folders')) data = { total_document_count: 1, root_document_count: 1, folders: [{ name: 'Deep parent folder', path: 'parent', document_count: 0, total_count: 1, children: [{ name: 'Long nested folder name for width adjustment', path: 'parent/child', document_count: 1, total_count: 1 }] }] }
    else if (path.endsWith('/knowledge')) return route.fulfill({ json: { success: true, data: [doc], total: 1 } })
    else if (path === '/api/v1/knowledge-bases/local-kb') data = kb
    return route.fulfill({ json: { success: true, data, total: 0 } })
  })
  await page.route('**/api/v1/knowledge/local-doc**', route => route.fulfill({ json: { success: true, data: doc } }))
  await page.goto('/platform/knowledge-bases/local-kb')
  const pane = page.locator('.kb-folder-tree')
  const handle = pane.getByRole('separator')
  await expect(handle).toBeVisible()
  const before = (await pane.boundingBox())!
  const box = (await handle.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + 50)
  await page.mouse.down()
  await page.mouse.move(box.x + 210, box.y + 50, { steps: 10 })
  await page.mouse.up()
  expect((await pane.boundingBox())!.width).toBeGreaterThan(before.width + 150)
  const saved = await handle.getAttribute('aria-valuenow')
  await page.reload()
  await expect(handle).toHaveAttribute('aria-valuenow', saved!)
  await handle.focus()
  await page.keyboard.press('Home')
  await expect(handle).toHaveAttribute('aria-valuenow', '180')
  await page.keyboard.press('ArrowRight')
  await expect(handle).toHaveAttribute('aria-valuenow', '200')
  await expect(page.locator('.row-file-meta')).toContainText('提交人: Alice')
  await page.locator('.row-file-name').click()
  const summary = page.locator('.summary_content')
  await expect(summary).toHaveText(description)
  expect(await summary.evaluate(el => el.scrollHeight <= el.clientHeight + 1)).toBe(true)
  await expect(page.locator('.doc-detail-rows')).toContainText('Alice')
  expect(api.pageErrors).toEqual([])
  expect(api.mutations).toEqual([])
})
