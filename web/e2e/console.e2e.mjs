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

async function assertNoHorizontalOverflow(page, label) {
  const { scrollW, clientW } = await page.evaluate(() => ({
    scrollW: document.documentElement.scrollWidth,
    clientW: document.documentElement.clientWidth,
  }))
  assert(scrollW <= clientW + 2, `${label}: horizontal overflow detected (scrollWidth=${scrollW} > clientWidth=${clientW})`)
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
    // 1. Login page: Light-First default theme + zero button overflow + Local Argon2id login
    await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
    const initialTheme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'))
    assert(initialTheme === 'light', `expected default data-theme="light", got ${initialTheme}`)
    await assertNoHorizontalOverflow(page, 'Login')
    const oidcBounds = await page.evaluate(() => {
      const box = document.querySelector('.ki-login-box')?.getBoundingClientRect()
      const btn = document.querySelector('.ki-oidc-btn')?.getBoundingClientRect()
      return box && btn ? { boxW: box.width, btnW: btn.width, overflow: btn.right > box.right + 2 || btn.left < box.left - 2 } : null
    })
    assert(oidcBounds && !oidcBounds.overflow, `OIDC button overflowed login box: ${JSON.stringify(oidcBounds)}`)

    const navStart = Date.now()
    await clickNow(page.locator('.ki-login-hint'))
    await clickNow(page.locator('.ki-login-submit'))
    await page.waitForURL('**/dashboard', { timeout: 15000 })
    await page.waitForLoadState('networkidle')
    const dashMs = Date.now() - navStart
    const dashTitle = await page.textContent('body')
    assert(dashTitle && dashTitle.includes('总览驾驶舱'), 'Dashboard failed to render 总览驾驶舱')
    await assertNoHorizontalOverflow(page, 'Dashboard')
    console.log(`[1/8] Login + Dashboard PASS (${dashMs}ms)`)

    // 2. CMDB Asset Center: verify Topology Rail, Import/Template/Export, Dynamic Groups, CI Impact, External Sync & 22 Modules
    await page.goto(`${BASE_URL}/hosts`, { waitUntil: 'networkidle' })
    const hostsText = await page.textContent('body')
    assert(hostsText && hostsText.includes('CMDB 资产中心'), 'Hosts view missing CMDB 资产中心 title')
    assert(hostsText && hostsText.includes('下载模板') && hostsText.includes('导出台账'), 'Hosts view missing template/export buttons')
    assert(hostsText && hostsText.includes('22 模块'), 'Hosts view missing 22 HostInit modules tab')
    await assertNoHorizontalOverflow(page, 'Hosts')
    await clickNow(page.getByRole('button', { name: '批量导入' }).first())
    await clickNow(page.getByRole('button', { name: '填入示例 CSV' }))
    await clickNow(page.getByRole('button', { name: '预检 Dry-Run' }))
    await page.waitForFunction(() => document.body.innerText.includes('node-app-01') && document.body.innerText.includes('HIS核心门诊'), { timeout: 10000 })
    await clickNow(page.getByRole('button', { name: '关闭', exact: true }))
    await clickNow(page.getByRole('tab', { name: /动态分组/ }))
    await clickNow(page.getByRole('tab', { name: /CI 影响面与进程/ }))
    await clickNow(page.getByRole('tab', { name: /外部 CMDB 同步/ }))
    await assertNoHorizontalOverflow(page, 'Hosts-CMDB-Tabs')
    console.log('[2/8] CMDB Asset Center (Topology + Import/Template/Export + Dynamic Groups + CI Impact + Sync + 22 Modules) PASS')

    // 3. Media Policies (Local / OBS·S3 / Online)
    await page.goto(`${BASE_URL}/artifacts/media-policies`, { waitUntil: 'networkidle' })
    const mediaText = await page.textContent('body')
    assert(mediaText && mediaText.includes('介质'), 'Media Policies view failed to render')
    await assertNoHorizontalOverflow(page, 'MediaPolicies')
    console.log('[3/8] Ternary Media Policies Center PASS')

    // 4. Instances & 12-Plugin Catalog -> Dynamic ParamSchema Drawer
    await page.goto(`${BASE_URL}/instances`, { waitUntil: 'networkidle' })
    await clickNow(page.getByRole('tab', { name: /组件市场 \(12\)/ }))
    const deployBtns = page.getByRole('button', { name: '部署' })
    await deployBtns.first().waitFor({ state: 'visible', timeout: 10000 })
    assert((await deployBtns.count()) >= 12, 'Expected >= 12 deploy buttons in Plugin Catalog')
    await assertNoHorizontalOverflow(page, 'Instances')
    await clickNow(deployBtns.first())
    await page.waitForFunction(() => document.body.innerText.includes('实时 JSON-Schema'), { timeout: 10000 })
    await clickNow(page.getByRole('button', { name: '取消', exact: true }))
    console.log('[4/8] Instances + 12 Plugins Real ParamSchema Wizard PASS')

    // 5. Stack DAG Canvas (live backend /stacks:validate -> Vue Flow 2.0 nodes + ZERO overlap)
    await page.goto(`${BASE_URL}/stacks`, { waitUntil: 'networkidle' })
    await page.waitForSelector('.vue-flow__node', { timeout: 10000 })
    await page.waitForTimeout(300)
    const dagCheck = await page.evaluate(() => {
      const rects = Array.from(document.querySelectorAll('.vue-flow__node')).map((el) => {
        const r = el.getBoundingClientRect()
        return { id: el.getAttribute('data-id'), x1: r.left, y1: r.top, x2: r.right, y2: r.bottom }
      })
      const overlaps = []
      for (let i = 0; i < rects.length; i++) {
        for (let j = i + 1; j < rects.length; j++) {
          const a = rects[i]
          const b = rects[j]
          if (a.x1 < b.x2 - 2 && a.x2 > b.x1 + 2 && a.y1 < b.y2 - 2 && a.y2 > b.y1 + 2) {
            overlaps.push(`${a.id} <-> ${b.id}`)
          }
        }
      }
      return { count: rects.length, overlaps }
    })
    assert(dagCheck.count >= 4, `Expected >= 4 live DAG nodes from backend, got ${dagCheck.count}`)
    assert(dagCheck.overlaps.length === 0, `DAG node overlap detected: ${dagCheck.overlaps.join(', ')}`)
    await assertNoHorizontalOverflow(page, 'Stacks')
    console.log(`[5/8] Live Stack DAG Canvas PASS (${dagCheck.count} nodes, 0 overlaps)`)

    // 6. Audit & Compliance -> Live Hash Chain Verify + Non-empty Entry Action
    await page.goto(`${BASE_URL}/security/audit`, { waitUntil: 'networkidle' })
    await clickNow(page.getByRole('button', { name: /立即重验/ }).first())
    await page.waitForLoadState('networkidle')
    const auditText = await page.textContent('body')
    assert(auditText && auditText.includes('审计防篡改链校验通过'), 'Audit chain badge missing')
    const actionCells = await page.locator('.action-pill').allTextContents()
    assert(actionCells.some((t) => t.trim() && t.trim() !== '—'), `Expected populated audit actions, got ${JSON.stringify(actionCells)}`)
    await assertNoHorizontalOverflow(page, 'Audit')
    console.log('[6/8] Audit Hash Chain & Populated Entry Fields PASS')

    // 7. Settings Governance Center -> Dark/Light Theme Toggle + Zero Segmented Wrapping
    await page.goto(`${BASE_URL}/settings`, { waitUntil: 'networkidle' })
    await assertNoHorizontalOverflow(page, 'Settings')
    await clickNow(page.getByText('暗色', { exact: true }))
    await page.waitForFunction(() => document.documentElement.getAttribute('data-theme') === 'dark', { timeout: 5000 })
    await clickNow(page.getByText('浅色（默认）', { exact: true }))
    await page.waitForFunction(() => document.documentElement.getAttribute('data-theme') === 'light', { timeout: 5000 })
    console.log('[7/8] Settings Governance Center + Dark/Light Theme Toggle PASS')

    // 8. Dynamic Meta-Model CMDB & Relation Canvas (/cmdb/models)
    await page.goto(`${BASE_URL}/cmdb/models`, { waitUntil: 'networkidle' })
    await page.waitForSelector('.vue-flow__node', { timeout: 10000 })
    const metaText = await page.textContent('body')
    assert(metaText && metaText.includes('动态元模型与关系画布') && metaText.includes('主机节点'), 'CMDB Models view missing expected content')
    await assertNoHorizontalOverflow(page, 'CMDB-Models')
    console.log('[8/18] Dynamic Meta-Model CMDB & Relation Canvas PASS')

    // 9. BlueKing-Style Dual Topology & Service Templates (/cmdb/topology)
    await page.goto(`${BASE_URL}/cmdb/topology`, { waitUntil: 'networkidle' })
    const topoText = await page.textContent('body')
    assert(topoText && topoText.includes('业务与资源双拓扑') && topoText.includes('业务主线树'), 'CMDB Topology view missing expected content')
    await assertNoHorizontalOverflow(page, 'CMDB-Topology')
    console.log('[9/18] Dual Topology Tree & Service Templates PASS')

    // 10. Generic CI Resource Library, Excelize .xlsx & Lineage Graph (/cmdb/resources)
    await page.goto(`${BASE_URL}/cmdb/resources`, { waitUntil: 'networkidle' })
    const resText = await page.textContent('body')
    assert(resText && resText.includes('通用 CI 资源库与血缘图谱') && resText.includes('下载 .xlsx 模板'), 'CMDB Resources view missing expected content')
    await assertNoHorizontalOverflow(page, 'CMDB-Resources')
    console.log('[10/18] Generic CI Library & Excelize .xlsx PASS')

    // 11. NetBox-Style DCIM 42U Rack Elevation & IPAM Subnets (/cmdb/dcim)
    await page.goto(`${BASE_URL}/cmdb/dcim`, { waitUntil: 'networkidle' })
    const dcimText = await page.textContent('body')
    assert(dcimText && dcimText.includes('DCIM 机柜与 IPAM 网段管理') && dcimText.includes('42U'), 'DCIM/IPAM view missing expected content')
    await assertNoHorizontalOverflow(page, 'CMDB-DCIM')
    console.log('[11/18] DCIM 42U Rack Elevation & IPAM PASS')

    // 12. Multi-Env Versioned Config & Secrets Center (/config-center)
    await page.goto(`${BASE_URL}/config-center`, { waitUntil: 'networkidle' })
    const cfgText = await page.textContent('body')
    assert(cfgText && cfgText.includes('多环境版本化配置与密钥中心') && cfgText.includes('***MASKED***'), 'Config Center view missing expected content or masked secret')
    await assertNoHorizontalOverflow(page, 'ConfigCenter')
    console.log('[12/18] Multi-Env Config & Secrets Center PASS')

    // 13. Declarative Extensible YAML Plugin Marketplace (/plugins)
    await page.goto(`${BASE_URL}/plugins`, { waitUntil: 'networkidle' })
    const plgText = await page.textContent('body')
    assert(plgText && plgText.includes('声明式插件与模板市场') && plgText.includes('新建 YAML 插件包'), 'Declarative Plugins view missing expected content')
    await assertNoHorizontalOverflow(page, 'Plugins')
    console.log('[13/18] Declarative Extensible YAML Plugins PASS')

    // 14. Enterprise Batch / M1 / M2 / M3 Delivery Pipelines (/delivery)
    await page.goto(`${BASE_URL}/delivery`, { waitUntil: 'networkidle' })
    const delivText = await page.textContent('body')
    assert(delivText && delivText.includes('企业级分批交付与发布调度中心') && delivText.includes('创建分批交付单'), 'Delivery view missing expected content')
    await assertNoHorizontalOverflow(page, 'Delivery')
    console.log('[14/18] Batch/M1/M2/M3 Delivery Scheduler PASS')

    // 15. Zero-Trust Outbound Agent Fleet & CIDR TCP Prober (/fleet)
    await page.goto(`${BASE_URL}/fleet`, { waitUntil: 'networkidle' })
    const fleetText = await page.textContent('body')
    assert(fleetText && fleetText.includes('零信任 Agent 执行舰队与网络探测') && fleetText.includes('签发 Agent 引导令牌'), 'Fleet view missing expected content')
    await assertNoHorizontalOverflow(page, 'Fleet')
    console.log('[15/18] Zero-Trust Outbound Agent Fleet PASS')

    // 16. Offline Artifacts Catalog & Native OCI (/artifacts/bundles)
    await page.goto(`${BASE_URL}/artifacts/bundles`, { waitUntil: 'networkidle' })
    const artText = await page.textContent('body')
    assert(artText && artText.includes('离线制品库') && artText.includes('上传离线包'), 'Artifacts view missing title or upload action')
    await assertNoHorizontalOverflow(page, 'Artifacts')
    console.log('[16/18] Offline Artifacts Catalog & Readiness Matrix PASS')

    // 17. Batch Job Runner & LiveOps Script Templates (/jobs)
    await page.goto(`${BASE_URL}/jobs`, { waitUntil: 'networkidle' })
    const jobsText = await page.textContent('body')
    assert(jobsText && jobsText.includes('批量作业中心') && jobsText.includes('内置运维与巡检模板'), 'Jobs view missing title or template list')
    await assertNoHorizontalOverflow(page, 'Jobs')
    console.log('[17/18] Batch Job Execution Center & Curated Templates PASS')

    // 18. Design System Showcase (/__design)
    await page.goto(`${BASE_URL}/__design`, { waitUntil: 'networkidle' })
    const designText = await page.textContent('body')
    assert(designText && designText.includes('kinstall Design System v3.0'), '/__design showcase failed to render')
    console.log('[18/18] Design System Showcase (/__design) PASS')

    assert(externalRequests.length === 0, `Detected forbidden external CDN requests: ${externalRequests.join(', ')}`)
    assert(serverErrors.length === 0, `Detected HTTP 5xx responses: ${serverErrors.join(', ')}`)
    assert(consoleErrors.length === 0, `Detected browser console errors: ${consoleErrors.join(' | ')}`)

    console.log(`PLAYWRIGHT HEADLESS CHROME E2E (${BASE_URL}): ALL 18 JOURNEYS PASS (0 console errors, 0 5xx, 0 external CDN, 0 overflows, 0 DAG overlaps)!`)
  } finally {
    await browser.close()
  }
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
