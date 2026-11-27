import { chromium } from 'playwright-core'

const BASE_URL = process.env.KINSTALL_E2E_URL || 'http://127.0.0.1:8080'
const CHROME_PATH = process.env.KINSTALL_CHROME_PATH || '/usr/bin/google-chrome'

function assert(cond, msg) {
  if (!cond) throw new Error(`E2E Assertion Failed: ${msg}`)
}

async function clickNow(locator) {
  await locator.waitFor({ state: 'visible', timeout: 10000 })
  await locator.evaluate((el) => el.click())
}

async function main() {
  const browser = await chromium.launch({
    executablePath: CHROME_PATH,
    headless: true,
    args: [
      '--no-sandbox',
      '--disable-setuid-sandbox',
      '--disable-dev-shm-usage',
      '--disable-gpu',
      '--disable-software-rasterizer',
    ],
  })

  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' })
  const page = await context.newPage()

  const consoleErrors = []
  const serverErrors = []
  const externalRequests = []

  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })
  page.on('pageerror', (err) => consoleErrors.push(String(err)))
  page.on('request', (req) => {
    const u = req.url()
    if ((u.startsWith('http://') || u.startsWith('https://')) && !u.startsWith(BASE_URL)) {
      externalRequests.push(u)
    }
  })
  page.on('response', (resp) => {
    if (resp.status() >= 500) serverErrors.push(`${resp.status()} ${resp.url()}`)
  })

  try {
    // 1. Login page: verify Light-First default theme + OIDC probe + Local Argon2id login
    await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
    const initialTheme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'))
    assert(initialTheme === 'light', `expected default data-theme="light", got ${initialTheme}`)

    const navStart = Date.now()
    await clickNow(page.locator('.ki-login-hint'))
    await clickNow(page.locator('.ki-login-submit'))
    await page.waitForURL('**/dashboard', { timeout: 15000 })
    await page.waitForLoadState('networkidle')
    const dashMs = Date.now() - navStart
    const dashTitle = await page.textContent('body')
    assert(dashTitle && dashTitle.includes('总览驾驶舱'), 'Dashboard failed to render 总览驾驶舱')
    console.log(`[1/8] Login + Dashboard PASS (${dashMs}ms)`)

    // 2. Hosts CMDB: verify 22-module HostInit tab + Bulk Import Drawer CSV Dry-Run
    await page.goto(`${BASE_URL}/hosts`, { waitUntil: 'networkidle' })
    const hostsText = await page.textContent('body')
    assert(hostsText && hostsText.includes('22 模块'), 'Hosts view missing 22 HostInit modules tab')
    await clickNow(page.getByRole('button', { name: '批量导入' }))
    await clickNow(page.getByRole('button', { name: '填入示例 CSV' }))
    await clickNow(page.getByRole('button', { name: '预检 Dry-Run' }))
    await page.waitForFunction(() => document.body.innerText.includes('node-app-01'), { timeout: 10000 })
    await clickNow(page.getByRole('button', { name: '关闭', exact: true }))
    console.log('[2/8] Hosts CMDB + 22 Modules + Bulk Import Dry-Run PASS')

    // 3. Media Policies (Local / OBS·S3 / Online)
    await page.goto(`${BASE_URL}/artifacts/media-policies`, { waitUntil: 'networkidle' })
    const mediaText = await page.textContent('body')
    assert(mediaText && mediaText.includes('介质'), 'Media Policies view failed to render')
    console.log('[3/8] Ternary Media Policies Center PASS')

    // 4. Instances & 12-Plugin Catalog -> Dynamic ParamSchema Drawer
    await page.goto(`${BASE_URL}/instances`, { waitUntil: 'networkidle' })
    await clickNow(page.getByText(/插件市场 P1–P10 \(12\)/))
    const deployBtns = page.getByRole('button', { name: '部署' })
    await deployBtns.first().waitFor({ state: 'visible', timeout: 10000 })
    assert((await deployBtns.count()) >= 12, 'Expected >= 12 deploy buttons in Plugin Catalog')
    await clickNow(deployBtns.first())
    await page.waitForFunction(() => document.body.innerText.includes('实时 JSON-Schema'), { timeout: 10000 })
    await clickNow(page.getByRole('button', { name: '取消', exact: true }))
    console.log('[4/8] Instances + 12 Plugins Real ParamSchema Wizard PASS')

    // 5. Stack DAG Canvas (live backend /stacks:validate -> Vue Flow 2.0 nodes)
    await page.goto(`${BASE_URL}/stacks`, { waitUntil: 'networkidle' })
    await page.waitForSelector('.vue-flow__node', { timeout: 10000 })
    const dagNodeCount = await page.locator('.vue-flow__node').count()
    assert(dagNodeCount >= 4, `Expected >= 4 live DAG nodes from backend, got ${dagNodeCount}`)
    console.log(`[5/8] Live Stack DAG Canvas PASS (${dagNodeCount} nodes)`)

    // 6. Audit & Compliance -> Live Hash Chain Verify
    await page.goto(`${BASE_URL}/security/audit`, { waitUntil: 'networkidle' })
    await clickNow(page.getByRole('button', { name: /立即重验/ }).first())
    await page.waitForLoadState('networkidle')
    const auditText = await page.textContent('body')
    assert(auditText && auditText.includes('审计防篡改链校验通过'), 'Audit chain badge missing')
    console.log('[6/8] Audit Hash Chain & Compliance Center PASS')

    // 7. Settings Governance Center -> Dark/Light Theme Toggle
    await page.goto(`${BASE_URL}/settings`, { waitUntil: 'networkidle' })
    await clickNow(page.getByText('暗色'))
    await page.waitForFunction(() => document.documentElement.getAttribute('data-theme') === 'dark', { timeout: 5000 })
    await clickNow(page.getByText('浅色（默认）'))
    await page.waitForFunction(() => document.documentElement.getAttribute('data-theme') === 'light', { timeout: 5000 })
    console.log('[7/8] Settings Governance Center + Dark/Light Theme Toggle PASS')

    // 8. Design System Showcase (/__design)
    await page.goto(`${BASE_URL}/__design`, { waitUntil: 'networkidle' })
    const designText = await page.textContent('body')
    assert(designText && designText.includes('kinstall Design System v3.0'), '/__design showcase failed to render')
    console.log('[8/8] Design System Showcase (/__design) PASS')

    assert(externalRequests.length === 0, `Detected forbidden external CDN requests: ${externalRequests.join(', ')}`)
    assert(serverErrors.length === 0, `Detected HTTP 5xx responses: ${serverErrors.join(', ')}`)
    assert(consoleErrors.length === 0, `Detected browser console errors: ${consoleErrors.join(' | ')}`)

    console.log(`PLAYWRIGHT HEADLESS CHROME E2E (${BASE_URL}): ALL 8 JOURNEYS PASS (0 console errors, 0 5xx, 0 external CDN, ${dagNodeCount} live DAG nodes)!`)
  } finally {
    await browser.close()
  }
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
