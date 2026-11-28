#!/usr/bin/env node
// Playwright Headless Chrome E2E & 1366x820 Visual Audit across Portal Home + all 13 Console views
import fs from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright-core'

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:8080'
const SHOT_DIR = process.env.SHOT_DIR || '/home/gongjin/.gemini/antigravity-cli/brain/6a1baf2b-19ee-41db-84fb-3d3cdff76c78/scratch/liveops-ks-v2-screenshots'
const CHROME_PATH = process.env.CHROME_PATH || '/usr/bin/google-chrome'

fs.mkdirSync(SHOT_DIR, { recursive: true })

const VIEWS = [
  { name: '01-home-portal', path: '/home' },
  { name: '02-platform-overview', path: '/platform/overview' },
  { name: '03-platform-tenants', path: '/platform/tenants' },
  { name: '04-platform-access', path: '/platform/access' },
  { name: '05-platform-nodes', path: '/platform/nodes' },
  { name: '06-platform-gateway', path: '/platform/gateway' },
  { name: '07-platform-audit', path: '/platform/audit' },
  { name: '08-workspace-overview', path: '/workspace/overview' },
  { name: '09-workspace-apps', path: '/workspace/apps' },
  { name: '10-workspace-projects', path: '/workspace/projects' },
  { name: '11-project-overview', path: '/project/overview' },
  { name: '12-project-assets', path: '/project/assets' },
  { name: '13-project-configs', path: '/project/configs' },
  { name: '14-project-jobs', path: '/project/jobs' },
]

const browser = await chromium.launch({
  executablePath: CHROME_PATH,
  headless: true,
  args: ['--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage'],
})

const consoleErrors = []
const pageErrors = []
const failedApi = []
const externalReqs = []
const overflowIssues = []

try {
  const context = await browser.newContext({ viewport: { width: 1366, height: 820 } })
  const page = await context.newPage()

  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })
  page.on('pageerror', (err) => pageErrors.push(String(err)))
  page.on('request', (req) => {
    const u = req.url()
    if (u.startsWith('http') && !u.startsWith(BASE_URL)) externalReqs.push(u)
  })
  page.on('response', (res) => {
    if (res.status() >= 500) failedApi.push(`${res.status()} ${res.url()}`)
  })

  // 1. Login -> redirects to /home KubeSphere Portal!
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
  await page.screenshot({ path: path.join(SHOT_DIR, '00-login.png') })
  await page.click('[data-testid="login-submit"]')
  await page.waitForURL('**/home', { timeout: 10000 })
  await page.waitForLoadState('networkidle')
  await page.waitForTimeout(300)

  // Verify KubeSphere Mega-Menu Popover on /home
  await page.click('[data-testid="ks-platform-switcher"]')
  await page.waitForSelector('[data-testid="ks-mega-menu"]', { timeout: 3000 })
  await page.screenshot({ path: path.join(SHOT_DIR, '01b-mega-menu.png') })
  await page.click('[data-testid="ks-platform-switcher"]')

  // 2. Visit all 14 views & check zero horizontal overflow
  for (const v of VIEWS) {
    await page.goto(`${BASE_URL}${v.path}`, { waitUntil: 'networkidle' })
    await page.waitForTimeout(350)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    if (overflow > 4) overflowIssues.push(`${v.path}: horizontal overflow ${overflow}px`)
    await page.screenshot({ path: path.join(SHOT_DIR, `${v.name}.png`) })
  }

  // 3. Interactive checks: CMDB Topology tab + Asset Detail Drawer
  await page.goto(`${BASE_URL}/project/assets`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(300)
  await page.getByRole('tab', { name: /拓扑可视化/ }).click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: path.join(SHOT_DIR, '12b-cmdb-topology.png') })

  // 4. Interactive check: Live Pipeline Execution on /project/jobs
  await page.goto(`${BASE_URL}/project/jobs`, { waitUntil: 'networkidle' })
  await page.click('[data-testid="submit-dag-btn"]')
  await page.waitForSelector('[data-testid="confirm-submit-dag"]')
  await page.click('[data-testid="confirm-submit-dag"]')
  await page.waitForTimeout(2400)
  await page.screenshot({ path: path.join(SHOT_DIR, '14b-live-pipeline-run.png') })

  console.log(
    JSON.stringify(
      {
        viewsChecked: VIEWS.length,
        consoleErrors,
        pageErrors,
        failedApi,
        externalReqs,
        overflowIssues,
        screenshotsDir: SHOT_DIR,
      },
      null,
      2
    )
  )

  if (consoleErrors.length || pageErrors.length || failedApi.length || externalReqs.length || overflowIssues.length) {
    process.exitCode = 1
  }
} finally {
  await browser.close()
}
