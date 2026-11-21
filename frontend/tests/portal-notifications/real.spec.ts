import { test, expect, type Browser, type Page } from '@playwright/test'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'

// Actual isolated deployment: no route interception, fixture tokens or mocked APIs.
// The two accounts authenticate through the normal password login page.
const runtime = JSON.parse(readFileSync(process.env.PORTAL_REAL_RUNTIME!, 'utf8'))
const evidence = process.env.PORTAL_REAL_EVIDENCE!
const fixture = runtime.fixture
const checks: Record<string, boolean> = {}
const attempt = Date.now().toString(36)

async function login(browser: Browser, role: 'owner' | 'applicant') {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  await context.addInitScript(() => {
    localStorage.setItem('locale', 'zh-CN')
    localStorage.setItem('weknora:new-user-guide-done:v1', '1')
  })
  const page = await context.newPage()
  await page.goto(runtime.url + '/login')
  await page.locator('input[autocomplete="email"]').fill(runtime.accounts[role].email)
  await page.locator('input[autocomplete="current-password"]').fill(runtime.accounts[role].password)
  await page.locator('button.submit-button').click()
  await expect(page).toHaveURL(/\/portal/)
  await expect(page.getByRole('button', { name: '消息中心', exact: true })).toBeVisible()
  checks[role + '_password_login'] = true
  return page
}

async function api(page: Page, method: string, path: string, body?: unknown, tenant?: number) {
  return page.evaluate(async ({ method, path, body, tenant }) => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json', Authorization: 'Bearer ' + localStorage.getItem('weknora_token') }
    const selected = tenant || Number(localStorage.getItem('weknora_selected_tenant_id') || 0)
    if (selected) headers['X-Tenant-ID'] = String(selected)
    const response = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
    return { status: response.status, data: await response.json() }
  }, { method, path, body, tenant })
}

async function submit(page: Page, reason: string) {
  await page.goto(runtime.url + '/portal')
  await page.getByRole('button', { name: /^概念2 ·/ }).click()
  const dialog = page.locator('.t-dialog').filter({ has: page.getByPlaceholder('请说明您需要访问该空间的原因') })
  await dialog.getByPlaceholder('请说明您需要访问该空间的原因').fill(reason)
  const response = page.waitForResponse(r => r.url().endsWith(`/portal/spaces/${fixture.tenant_id}/access-requests`) && r.request().method() === 'POST')
  await dialog.getByRole('button', { name: '申请访问', exact: true }).click()
  expect((await response).status()).toBe(201)
}

async function ownerReview(page: Page, reason: string, decision: 'approve' | 'reject', note: string) {
  await page.goto(runtime.url + '/portal')
  await page.getByRole('button', { name: '消息中心', exact: true }).click()
  const message = page.locator('.inbox-card').filter({ hasText: reason })
  await expect(message).toBeVisible()
  await message.locator('.message-link').click()
  await expect(page).toHaveURL(/section=members/)
  const request = page.locator('.request-list article').filter({ hasText: reason })
  await expect(request).toBeVisible()
  await request.getByPlaceholder('审批意见（最多 1000 字）').fill(note)
  const response = page.waitForResponse(r => r.url().endsWith('/' + decision) && r.request().method() === 'POST')
  await request.getByRole('button', { name: decision === 'approve' ? '批准' : '拒绝', exact: true }).click()
  expect((await response).status()).toBe(200)
  await expect(request).not.toBeVisible()
}

test('two persisted accounts complete rejection, approval, notification entry and knowledge access', async ({ browser }) => {
  mkdirSync(evidence, { recursive: true })
  let applicant: Page | undefined, owner: Page | undefined
  try {
    applicant = await login(browser, 'applicant')
    const rejectedReason = '真实双账号拒绝流程：请审批研发知识访问申请 ' + attempt
    await submit(applicant, rejectedReason)
    checks['ui_request_submitted'] = true
    const duplicate = await api(applicant, 'POST', `/api/v1/portal/spaces/${fixture.tenant_id}/access-requests`, { reason: 'Duplicate must fail' })
    expect(duplicate.status).toBe(409)
    checks['duplicate_pending_denied'] = true
    const mine = await api(applicant, 'GET', '/api/v1/me/tenant-access-requests')
    const pending = mine.data.data.find((r: any) => r.reason === rejectedReason)
    expect(pending.status).toBe('pending')
    const forbidden = await api(applicant, 'POST', `/api/v1/tenants/${fixture.tenant_id}/access-requests/${pending.id}/approve`, { review_note: 'Must fail' }, fixture.tenant_id)
    expect(forbidden.status).toBe(403)
    checks['non_owner_review_denied'] = true
    owner = await login(browser, 'owner')
    await ownerReview(owner, rejectedReason, 'reject', '请补充业务范围后重新申请')
    checks['owner_received_and_rejected'] = true
    await applicant.reload()
    await applicant.getByRole('button', { name: '消息中心', exact: true }).click()
    await expect(applicant.locator('.inbox-card').filter({ hasText: '知识空间访问申请未通过' })).toContainText('请补充业务范围后重新申请')
    await applicant.getByText('我的申请', { exact: true }).click()
    await expect(applicant.locator('.inbox-card').filter({ hasText: rejectedReason })).toContainText('已拒绝')
    checks['applicant_rejection_and_note_visible'] = true
    await applicant.locator('.t-drawer__close-btn:visible').click()
    const approvedReason = '真实双账号通过流程：需要检索概念2的研发规范 ' + attempt
    await submit(applicant, approvedReason)
    await ownerReview(owner, approvedReason, 'approve', '同意以 Viewer 访问研发规范')
    checks['owner_approved_from_notification'] = true
    await applicant.reload()
    await applicant.getByRole('button', { name: '消息中心', exact: true }).click()
    const approved = applicant.locator('.inbox-card').filter({ hasText: '知识空间访问申请已通过' })
    await expect(approved).toContainText('同意以 Viewer 访问研发规范')
    await approved.locator('.message-link').click()
    await expect(applicant).toHaveURL(/\/platform\/knowledge-bases/)
    await expect(applicant.getByText(fixture.kb_name, { exact: true }).first()).toBeVisible()
    checks['approval_notification_enters_space'] = true
    const metadata = await api(applicant, 'GET', '/api/v1/auth/me')
    const membership = metadata.data.data.memberships.find((m: any) => Number(m.tenant_id) === Number(fixture.tenant_id))
    expect(membership.role).toBe('viewer')
    checks['viewer_membership_granted'] = true
    const document = await api(applicant, 'GET', `/api/v1/knowledge/${fixture.knowledge_id}`)
    expect(document.status).toBe(200)
    const search = await api(applicant, 'GET', '/api/v1/knowledge/search?keyword=PORTAL_ACCESS_CANARY')
    expect(search.status).toBe(200)
    expect(JSON.stringify(search.data)).toContain(fixture.knowledge_id)
    checks['knowledge_browse_and_keyword_search'] = true
    await applicant.goto(runtime.url + '/platform/knowledge-bases/' + fixture.kb_id)
    const filtered = applicant.waitForResponse(r => r.request().method() === 'GET' && r.url().includes('/knowledge') && r.url().includes('keyword=PORTAL_ACCESS_CANARY'))
    await applicant.getByPlaceholder('搜索文档名称').fill('PORTAL_ACCESS_CANARY')
    await applicant.getByPlaceholder('搜索文档名称').press('Enter')
    expect((await filtered).status()).toBe(200)
    await expect(applicant.getByText(fixture.knowledge_title, { exact: true }).first()).toBeVisible()
    checks['ui_keyword_search_found_document'] = true
    await applicant.screenshot({ path: join(evidence, 'applicant-authorized-search.png'), animations: 'disabled' })
    const repeat = await api(applicant, 'POST', `/api/v1/portal/spaces/${fixture.tenant_id}/access-requests`, { reason: 'Already member' })
    expect(repeat.status).toBe(409)
    checks['authorized_user_cannot_reapply'] = true
    const history = await api(applicant, 'GET', '/api/v1/me/tenant-access-requests')
    const grant = history.data.data.find((r: any) => r.reason === approvedReason)
    const ownerUnread = await api(owner, 'GET', '/api/v1/me/notifications')
    const ownerNotice = ownerUnread.data.data.find((r: any) => r.reference_id === grant.id)
    const wrongRead = await api(applicant, 'POST', `/api/v1/me/notifications/${ownerNotice.id}/read`)
    expect(wrongRead.status).toBe(404)
    checks['another_users_notification_denied'] = true
    const before = await api(applicant, 'GET', '/api/v1/me/notifications')
    const again = await api(owner, 'POST', `/api/v1/tenants/${fixture.tenant_id}/access-requests/${grant.id}/approve`, { review_note: 'Idempotent retry' }, fixture.tenant_id)
    expect(again.status).toBe(200)
    const after = await api(applicant, 'GET', '/api/v1/me/notifications')
    expect(after.data.total).toBe(before.data.total)
    checks['duplicate_review_does_not_duplicate_notification'] = true
    await api(applicant, 'POST', '/api/v1/me/notifications/read-all')
    await applicant.reload()
    const count = await api(applicant, 'GET', '/api/v1/me/notifications/unread-count')
    expect(count.data.data.unread_count).toBe(0)
    checks['read_state_persists_after_reload'] = true
  } finally {
    writeFileSync(join(evidence, 'acceptance.json'), JSON.stringify({ source_sha: runtime.source_sha, actual_password_accounts: 2, api_mocking: false, checks, all_passed: Object.keys(checks).length === 16 && Object.values(checks).every(Boolean), content_scope: 'synthetic manual content and title-keyword search; no semantic model or employee SSO attestation' }, null, 2))
    if (applicant) await applicant.context().close()
    if (owner) await owner.context().close()
  }
})
