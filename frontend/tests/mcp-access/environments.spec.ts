import { test, expect } from '@playwright/test'
import { keyFixture, openKey, chooseClient } from './fixtures'

test('environment switching preserves the Key, masks previews and copies the right URL for all clients', async ({ page }) => {
  const api = await keyFixture(page)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/platform/mcp-access')
  const help = page.getByRole('link', { name: '使用帮助文档', exact: true })
  await expect(help).toHaveAttribute('href', 'https://ruijie.feishu.cn/docx/E3fSdQCgyoy6bwxHTWVckgWYnLc')
  await expect(help).toHaveAttribute('rel', 'noopener noreferrer')
  await openKey(page, 3)
  await chooseClient(page, 'Codex')
  const dialog = page.locator('.t-dialog:visible')
  await expect(dialog.getByRole('radio', { name: '云外地址', exact: true })).toBeChecked()
  await expect(dialog.locator('.connection-environment')).toContainText('企业内网 · 查询与新增')
  await expect(dialog.locator('.connection-environment')).toContainText('隔离· 仅查询')
  await expect(dialog.locator('.connection-environment .t-alert')).toHaveCount(0)
  await expect(dialog.locator('.connection-help')).not.toHaveAttribute('open')
  for (const client of ['Codex', 'Claude Code', 'GitHub Copilot', 'VS Code', 'WorkBuddy', 'Pi', '其他 MCP 客户端']) {
    if (client !== 'Codex') {
      await dialog.getByRole('button', { name: '更换客户端', exact: true }).click()
      await chooseClient(page, client)
    }
    await dialog.getByText('云内地址', { exact: true }).click()
    await expect(dialog.locator('pre')).toContainText('https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal')
    await expect(dialog.locator('pre')).not.toContainText('synthetic-key-3')
    await dialog.getByRole('button', { name: '复制完整配置（含密钥）', exact: true }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('Bearer synthetic-key-3')
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('https://knowhub.ruijie.com.cn:8443/api/v1/mcp/personal')
    await dialog.getByText('云外地址', { exact: true }).click()
    await dialog.getByRole('button', { name: '复制完整配置（含密钥）', exact: true }).click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(new URL('/api/v1/mcp/personal', page.url()).href)
  }
  expect(api.mutations).toEqual([])
  await dialog.getByText('云内地址', { exact: true }).click()
  const revealCount = api.reveals.length
  await dialog.getByRole('button', { name: '验证 MCP 协议', exact: true }).click()
  await expect(dialog).toContainText('这不代表服务故障')
  expect(api.reveals).toHaveLength(revealCount)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(dialog.locator('.connection-environment')).toBeVisible()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: 'test-results/mcp-access/environments-mobile.png' })
  await dialog.locator('.t-dialog__close').click()
  await openKey(page, 3)
  await chooseClient(page, 'Codex')
  await expect(dialog.getByRole('radio', { name: '云外地址', exact: true })).toBeChecked()
})
