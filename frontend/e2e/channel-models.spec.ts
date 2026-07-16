import { expect, test } from '@playwright/test'

const sessionToken = process.env.AIFERRY_SESSION_TOKEN
const channelName = process.env.AIFERRY_TEST_CHANNEL || 'acceptance-ui-model-selection'

test('selects discovered models, tests the channel, and keeps pricing focused', async ({ context, page }, testInfo) => {
  test.skip(!sessionToken, 'AIFERRY_SESSION_TOKEN is required for authenticated channel acceptance')
  await context.addCookies([{
    name: 'aiferry_session',
    value: sessionToken!,
    url: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080',
    httpOnly: true,
    sameSite: 'Lax',
  }])

  await page.goto('/channels', { waitUntil: 'networkidle' })
  const channelRow = page.locator('.el-table__row').filter({ hasText: channelName })
  await expect(channelRow).toBeVisible({ timeout: 15_000 })
  await channelRow.getByRole('button', { name: `发现 ${channelName} 的模型` }).click()

  await expect(page.getByRole('dialog', { name: `选择模型 · ${channelName}` })).toBeVisible()
  await expect(page.locator('.model-check-list code')).toHaveText(['alpha-model', 'mock-gpt', 'zeta-model'])
  await expect(page.locator('.model-selection > .el-loading-mask')).toHaveCount(0)
  const mockModel = page.getByRole('checkbox', { name: 'mock-gpt' })
  if (!await mockModel.isChecked()) await page.locator('.el-checkbox').filter({ hasText: 'mock-gpt' }).click()
  await page.screenshot({ path: `test-results/channel-discovery-${testInfo.project.name}.png` })
  await page.getByRole('button', { name: '确认选择' }).click()
  await expect(channelRow).toContainText('1 / 1')

  await channelRow.getByRole('button', { name: `测试 ${channelName} 的模型` }).click()
  const testDialog = page.getByRole('dialog', { name: `渠道测试 · ${channelName}` })
  await expect(testDialog).toBeVisible()
  await expect(testDialog.locator('.el-select__selected-item.el-select__placeholder')).toHaveText('mock-gpt')
  await page.getByRole('button', { name: '开始测试' }).click()
  await expect(testDialog.getByText('测试通过', { exact: true })).toBeVisible()

  await page.goto('/models')
  await expect(page.getByRole('columnheader', { name: '最近测试' })).toHaveCount(0)
  await page.getByRole('button', { name: '设置 mock-gpt 的价格' }).click()
  const pricingDrawer = page.locator('.el-drawer').filter({ hasText: '价格设置' })
  await expect(page.getByRole('heading', { name: '价格设置' })).toBeVisible()
  const viewport = page.viewportSize()
  await expect.poll(async () => {
    const box = await pricingDrawer.boundingBox()
    return box ? Math.round(box.x + box.width) : Number.POSITIVE_INFINITY
  }).toBeLessThanOrEqual(viewport!.width + 1)
  const drawerBox = await pricingDrawer.boundingBox()
  expect(drawerBox!.x).toBeGreaterThanOrEqual(0)
  await expect(page.getByText('公开模型名称')).toHaveCount(0)
  await expect(page.getByText('对外启用')).toHaveCount(0)
  await expect(page.getByText('USD / 1M Token')).toBeVisible()
  await page.screenshot({ path: `test-results/model-pricing-${testInfo.project.name}.png` })
})
