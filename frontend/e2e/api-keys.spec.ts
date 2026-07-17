import { expect, test } from '@playwright/test'

const sessionToken = process.env.AIFERRY_SESSION_TOKEN

test('creates, displays once, edits and deletes an API key', async ({ context, page }, testInfo) => {
  test.skip(!sessionToken, 'AIFERRY_SESSION_TOKEN is required for authenticated API key acceptance')
  await context.addCookies([{
    name: 'aiferry_session',
    value: sessionToken!,
    url: process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:8080',
    httpOnly: true,
    sameSite: 'Lax',
  }])

  const keyName = `acceptance-api-key-${Date.now()}`
  await page.goto('/api-keys')
  await expect(page.getByRole('button', { name: '创建密钥' }).first()).toBeVisible()
  await expect(page.getByText('还没有访问密钥')).toBeVisible()
  await page.screenshot({ path: `test-results/api-keys-${testInfo.project.name}.png`, fullPage: true })
  await page.getByRole('button', { name: '创建密钥' }).first().click()
  await page.getByPlaceholder('例如 开发环境').fill(keyName)
  await page.getByRole('button', { name: '创建密钥' }).last().click()

  await expect(page.getByText('访问密钥只显示这一次')).toBeVisible()
  await expect(page.locator('.secret-value code')).toHaveText(/^sk-/)
  await page.getByRole('button', { name: '完成' }).click()
  await expect(page.getByText(keyName)).toBeVisible()

  const editedName = `${keyName}-edited`
  const createdRow = page.locator('.el-table__row').filter({ hasText: keyName })
  await createdRow.getByRole('button', { name: '编辑密钥' }).click()
  await page.getByPlaceholder('例如 开发环境').fill(editedName)
  await page.getByRole('button', { name: '保存密钥' }).click()
  await expect(page.getByText(editedName)).toBeVisible()

  const row = page.locator('.el-table__row').filter({ hasText: editedName })
  await row.getByRole('button', { name: '删除密钥' }).click()
  await page.getByRole('button', { name: '删除' }).click()
  await expect(page.getByText(editedName)).toHaveCount(0)
})
