import { expect, test } from '@playwright/test'

test('protects the console and starts a valid Casdoor login', async ({ context, page }, testInfo) => {
  await context.clearCookies()
  await page.goto('/channels')

  await expect(page).toHaveURL(/\/login\?returnTo=/)
  await expect(page).toHaveTitle('登录 - AiFerry')
  await expect(page.getByRole('heading', { name: '登录 AiFerry' })).toBeVisible()
  await expect(page.getByRole('button', { name: '使用 Casdoor 登录' })).toBeEnabled()

  const layout = await page.evaluate(() => {
    const masthead = document.querySelector('.login-masthead')?.getBoundingClientRect()
    const tool = document.querySelector('.login-tool')?.getBoundingClientRect()
    const foot = document.querySelector('.login-foot')?.getBoundingClientRect()
    return {
      noHorizontalOverflow: document.documentElement.scrollWidth <= window.innerWidth,
      toolInsideViewport: !!tool && tool.left >= 0 && tool.right <= window.innerWidth && tool.top >= 0 && tool.bottom <= window.innerHeight,
      sectionsDoNotOverlap: !!masthead && !!tool && !!foot && masthead.bottom < tool.top && tool.bottom < foot.top,
    }
  })
  expect(layout).toEqual({ noHorizontalOverflow: true, toolInsideViewport: true, sectionsDoNotOverlap: true })

  await page.screenshot({ path: `test-results/auth-${testInfo.project.name}.png`, fullPage: true })
  await Promise.all([
    page.waitForURL((url) => url.hostname === 'oidc.luoe.cn', { timeout: 15_000, waitUntil: 'domcontentloaded' }),
    page.getByRole('button', { name: '使用 Casdoor 登录' }).click(),
  ])

  const stateCookie = (await context.cookies()).find((cookie) => cookie.name === 'aiferry_oauth_state')
  expect(stateCookie).toBeDefined()
  expect(stateCookie?.httpOnly).toBe(true)
  expect(stateCookie?.sameSite).toBe('Lax')
})

test('shows denied-login feedback without exposing the console', async ({ page }) => {
  await page.goto('/login?error=access_denied')
  await expect(page.getByRole('alert')).toContainText('当前账号不具备 AiFerry 访问权限')
  await expect(page.locator('.app-shell')).toHaveCount(0)
})
