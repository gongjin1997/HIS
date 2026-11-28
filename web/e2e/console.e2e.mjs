import fs from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright-core'

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:8080'
const SCREENSHOT_DIR = process.env.SCREENSHOT_DIR || ''

const CHROME_CANDIDATES = [
  process.env.CHROME_PATH,
  '/usr/bin/google-chrome',
  '/usr/bin/google-chrome-stable',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean)

function resolveChrome() {
  for (const p of CHROME_CANDIDATES) {
    if (fs.existsSync(p)) return p
  }
  return undefined
}

const ROUTES = [
  { key: '01-dashboard', path: '/dashboard', expectText: '资产与运维全景' },
  { key: '02-cmdb-models', path: '/cmdb/models', expectText: '动态元模型与关系画布' },
  { key: '03-cmdb-topology', path: '/cmdb/topology', expectText: '蓝鲸双拓扑树与服务模板' },
  { key: '04-cmdb-resources', path: '/cmdb/resources', expectText: '统一资产台账' },
  { key: '05-cmdb-dcim', path: '/cmdb/dcim', expectText: 'DCIM 42U 机柜与 IPAM 网段' },
  { key: '06-config', path: '/config', expectText: '多环境配置与 KMS 密钥中心' },
  { key: '07-plugins', path: '/plugins', expectText: '声明式 YAML 插件库' },
  { key: '08-delivery', path: '/delivery', expectText: 'DAG 与分批灰度发布流水线' },
  { key: '09-ops', path: '/ops', expectText: '执行舰队、批量作业与 CAS 制品仓' },
  { key: '10-governance', path: '/governance', expectText: '权限审计、凭据保险箱与平台设置' },
]

async function main() {
  if (SCREENSHOT_DIR) fs.mkdirSync(SCREENSHOT_DIR, { recursive: true })
  const executablePath = resolveChrome()
  const browser = await chromium.launch({
    headless: true,
    executablePath,
    args: ['--no-sandbox', '--disable-dev-shm-usage'],
  })

  const context = await browser.newContext({ viewport: { width: 1366, height: 820 } })
  const page = await context.newPage()

  const consoleErrors = []
  const pageErrors = []
  const serverErrors = []
  const externalReqs = []
  const baseOrigin = new URL(BASE_URL).origin

  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })
  page.on('pageerror', (err) => pageErrors.push(String(err)))
  page.on('response', (resp) => {
    if (resp.status() >= 500) serverErrors.push(`${resp.status()} ${resp.url()}`)
  })
  page.on('request', (req) => {
    const u = req.url()
    if ((u.startsWith('http://') || u.startsWith('https://')) && !u.startsWith(baseOrigin)) {
      externalReqs.push(u)
    }
  })

  try {
    // 1. Login flow
    await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
    await page.getByPlaceholder('admin').fill('admin')
    await page.getByPlaceholder('请输入登录密码').fill('Kinstall#2026!')
    await page.getByRole('button', { name: '登录进入平台' }).click()
    await page.waitForURL('**/dashboard', { timeout: 10000 })
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(400)

    // 2. Visit all 10 flagship views and check assertions
    for (const r of ROUTES) {
      await page.goto(`${BASE_URL}${r.path}`, { waitUntil: 'networkidle' })
      await page.waitForTimeout(350)
      const bodyText = await page.locator('body').innerText()
      if (!bodyText.includes(r.expectText)) {
        throw new Error(`Route ${r.path} missing expected heading text "${r.expectText}"`)
      }
      const overflow = await page.evaluate(() => ({
        sw: document.documentElement.scrollWidth,
        cw: document.documentElement.clientWidth,
      }))
      if (overflow.sw > overflow.cw + 2) {
        throw new Error(`Route ${r.path} has horizontal overflow: scrollWidth=${overflow.sw} > clientWidth=${overflow.cw}`)
      }
      if (SCREENSHOT_DIR) {
        await page.screenshot({ path: path.join(SCREENSHOT_DIR, `${r.key}.png`), fullPage: false })
      }
      console.log(`PASS ${r.path} (${r.expectText})`)
    }

    // 3. Interactive test: verify Audit Hash Chain on /governance
    await page.goto(`${BASE_URL}/governance`, { waitUntil: 'networkidle' })
    await page.getByRole('tab', { name: /防篡改审计哈希链/ }).click()
    await page.waitForTimeout(200)
    await page.getByRole('button', { name: '执行全链防篡改校验' }).click()
    await page.waitForTimeout(400)
    const govText = await page.locator('body').innerText()
    if (!govText.includes('哈希链完整有效')) {
      throw new Error('Audit hash chain verification badge did not appear')
    }

    // 4. Interactive test: toggle Dark theme and back to Light
    await page.getByRole('button', { name: '切换明暗主题' }).click()
    await page.waitForTimeout(200)
    const darkAttr = await page.evaluate(() => document.documentElement.getAttribute('data-theme'))
    if (darkAttr !== 'dark') throw new Error(`Expected dark theme, got ${darkAttr}`)
    await page.getByRole('button', { name: '切换明暗主题' }).click()
    await page.waitForTimeout(200)

    if (consoleErrors.length) throw new Error(`Console errors: ${consoleErrors.join('; ')}`)
    if (pageErrors.length) throw new Error(`Page errors: ${pageErrors.join('; ')}`)
    if (serverErrors.length) throw new Error(`5xx responses: ${serverErrors.join('; ')}`)
    if (externalReqs.length) throw new Error(`External CDN requests detected: ${externalReqs.join('; ')}`)

    console.log('ALL 10/10 E2E CHECKS PASSED (0 console errors, 0 5xx, 0 external CDN, 0 overflow)')
  } finally {
    await browser.close()
  }
}

main().catch((err) => {
  console.error('E2E FAILED:', err)
  process.exit(1)
})
