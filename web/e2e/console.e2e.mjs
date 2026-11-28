#!/usr/bin/env node
// Playwright Headless Chrome E2E & Visual Audit across all 13 KubeSphere Console views (1366x820)
import fs from 'node:fs'
import path from 'node:path'
import { chromium } from 'playwright-core'

const BASE_URL = process.env.BASE_URL || 'http://127.0.0.1:8080'
const SHOT_DIR = process.env.SHOT_DIR || '/home/gongjin/.gemini/antigravity-cli/brain/6a1baf2b-19ee-41db-84fb-3d3cdff76c78/scratch/liveops-ks-screenshots'
const CHROME_PATH = process.env.CHROME_PATH || '/usr/bin/google-chrome'

fs.mkdirSync(SHOT_DIR, { recursive: true })

const VIEWS = [
  { name: '01-platform-overview', path: '/platform/overview' },
  { name: '02-platform-tenants', path: '/platform/tenants' },
  { name: '03-platform-access', path: '/platform/access' },
  { name: '04-platform-nodes', path: '/platform/nodes' },
  { name: '05-platform-gateway', path: '/platform/gateway' },
  { name: '06-platform-audit', path: '/platform/audit' },
  { name: '07-workspace-overview', path: '/workspace/overview' },
  { name: '08-workspace-apps', path: '/workspace/apps' },
  { name: '09-workspace-projects', path: '/workspace/projects' },
  { name: '10-project-overview', path: '/project/overview' },
  { name: '11-project-assets', path: '/project/assets' },
  { name: '12-project-configs', path: '/project/configs' },
  { name: '13-project-jobs', path: '/project/jobs' },
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

  // 1. Login screen screenshot & authentication
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
  await page.screenshot({ path: path.join(SHOT_DIR, '00-login.png') })
  await page.click('[data-testid="login-submit"]')
  await page.waitForURL('**/platform/overview', { timeout: 10000 })
  await page.waitForLoadState('networkidle')

  // Test KubeSphere Platform Mega-Menu Switcher
  await page.click('[data-testid="ks-platform-switcher"]')
  await page.waitForSelector('[data-testid="ks-mega-menu"]', { timeout: 3000 })
  await page.screenshot({ path: path.join(SHOT_DIR, '01b-mega-menu.png') })
  await page.click('[data-testid="ks-platform-switcher"]')

  // 2. Visit all 13 Platform / Workspace / Project views & check zero horizontal overflow
  for (const v of VIEWS) {
    await page.goto(`${BASE_URL}${v.path}`, { waitUntil: 'networkidle' })
    await page.waitForTimeout(350)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    if (overflow > 4) {
      overflowIssues.push(`${v.path}: horizontal overflow ${overflow}px`)
    }
    await page.screenshot({ path: path.join(SHOT_DIR, `${v.name}.png`) })
  }

  // 3. Interactive live DAG Job submission + live decoupled Worker execution proof on /project/jobs!
  await page.goto(`${BASE_URL}/project/jobs`, { waitUntil: 'networkidle' })
  await page.click('[data-testid="submit-dag-btn"]')
  await page.waitForSelector('[data-testid="confirm-submit-dag"]')
  await page.click('[data-testid="confirm-submit-dag"]')
  await page.waitForTimeout(2500)
  await page.screenshot({ path: path.join(SHOT_DIR, '14-live-dag-executed.png') })

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
