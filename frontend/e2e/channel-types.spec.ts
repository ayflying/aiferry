import { expect, test } from '@playwright/test'

const sessionToken = process.env.AIFERRY_SESSION_TOKEN

test('manages channel types and selects one when adding a channel', async ({ context, page }, testInfo) => {
  test.skip(!sessionToken, 'AIFERRY_SESSION_TOKEN is required for authenticated channel type acceptance')
  await context.addCookies([{
    name: 'aiferry_session',
    value: sessionToken!,
    url: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080',
    httpOnly: true,
    sameSite: 'Lax',
  }])

  await page.goto('/channels', { waitUntil: 'networkidle' })
  await page.getByRole('tab', { name: '渠道类型' }).click()
  const typePanel = page.getByLabel('渠道类型', { exact: true })
  await expect(typePanel.getByText('OpenAI', { exact: true })).toBeVisible()
  await expect(typePanel.getByText('Sub2API', { exact: true })).toBeVisible()
  await typePanel.getByRole('button', { name: '添加渠道类型' }).click()
  await expect(page.getByRole('heading', { name: '添加渠道类型' })).toBeVisible()
  await expect(page.locator('.config-editor textarea')).toHaveValue(/"models"/)
  const typeDrawer = page.locator('.el-drawer').filter({ hasText: '添加渠道类型' })
  const viewport = page.viewportSize()
  await expect.poll(async () => {
    const box = await typeDrawer.boundingBox()
    return box ? Math.round(box.x + box.width) : Number.POSITIVE_INFINITY
  }).toBeLessThanOrEqual(viewport!.width + 1)
  await page.screenshot({ path: `test-results/channel-types-${testInfo.project.name}.png` })
  await page.getByRole('button', { name: '取消' }).last().click()

  await page.getByRole('tab', { name: '渠道', exact: true }).click()
  const channelPanel = page.getByLabel('渠道', { exact: true })
  await channelPanel.getByRole('button', { name: '添加渠道' }).click()
  await expect(page.locator('.el-drawer').getByText('渠道类型', { exact: true })).toBeVisible()
  await page.locator('.el-drawer .el-select').first().click()
  const typeOptions = page.locator('.el-select-dropdown:visible')
  await expect(typeOptions.getByText('OpenAI (openai)', { exact: true })).toBeVisible()
  await expect(typeOptions.getByText('Sub2API (sub2api)', { exact: true })).toBeVisible()
})
