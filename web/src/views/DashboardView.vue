<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowRight,
  Box,
  CheckCircle2,
  ClipboardCheck,
  GitBranch,
  HardDrive,
  Layers,
  Server,
  ShieldCheck,
  Terminal,
} from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'

const ws = useWorkspaceStore()
const router = useRouter()
const { formatRelTime } = useKiFormat()

const loading = ref(false)
const hosts = ref<Array<Record<string, unknown>>>([])
const instances = ref<Array<Record<string, unknown>>>([])
const operations = ref<Array<Record<string, unknown>>>([])
const plugins = ref<Array<Record<string, unknown>>>([])
const auditValid = ref<boolean | null>(null)

const pluginLabels: Record<string, { label: string; cat: string }> = {
  kubernetes: { label: 'Kubernetes 集群', cat: '容器编排' },
  postgresql: { label: 'PostgreSQL', cat: '关系数据库' },
  mysql: { label: 'MySQL / MariaDB', cat: '关系数据库' },
  valkey: { label: 'Valkey 缓存', cat: '缓存消息' },
  kafka: { label: 'Apache Kafka', cat: '缓存消息' },
  'messaging-tx': { label: 'RocketMQ / 消息事务', cat: '缓存消息' },
  opensearch: { label: 'OpenSearch', cat: '搜索日志' },
  'elk-pipeline': { label: '日志管道', cat: '搜索日志' },
  'object-storage': { label: '对象存储 S3', cat: '存储监控' },
  skywalking: { label: 'APM / 可观测监控', cat: '存储监控' },
  'devops-stack': { label: 'DevOps 交付链', cat: '研发交付' },
  'datastack-apps': { label: '数据与应用套件', cat: '应用生态' },
}

async function loadData() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [h, i, o, p] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/hosts`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/instances`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/operations`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', '/api/v1/plugins').catch(() => ({ data: { items: [] } })),
    ])
    hosts.value = h.data.items || []
    instances.value = i.data.items || []
    operations.value = o.data.items || []
    plugins.value = p.data.items || []
    await ws.refreshApprovalBadge()
  } finally {
    loading.value = false
  }
}

async function quickVerifyAudit() {
  try {
    const { data } = await apiRequest<{ valid: boolean; totalChecked?: number }>('POST', '/api/v1/audit:verify')
    auditValid.value = data.valid
    if (data.valid) ElMessage.success(`审计防篡改链核验通过（共 ${data.totalChecked ?? 0} 条）`)
    else ElMessage.error('检测到审计链异常，请立即前往审计中心核查')
  } catch {
    auditValid.value = false
  }
}

const arm64kCount = computed(() => hosts.value.filter((x) => Number(x.pageSize) >= 65536).length)
const failedOpsCount = computed(() => operations.value.filter((o) => String(o.state) === 'failed').length)
const runningOpsCount = computed(() => operations.value.filter((o) => String(o.state) === 'running').length)
const showOnboarding = computed(() => hosts.value.length === 0 || instances.value.length === 0)

const healthScore = computed(() => {
  let score = 100
  if (auditValid.value === false) score -= 35
  score -= Math.min(30, failedOpsCount.value * 10)
  score -= Math.min(10, ws.pendingApprovalCount * 2)
  return Math.max(0, score)
})

const gaugeArcLength = 251.2
const gaugeDashOffset = computed(() => gaugeArcLength * (1 - healthScore.value / 100))

const targetStats = computed(() => {
  const counts = { 'host-binary': 0, 'host-container': 0, kubernetes: 0 }
  for (const inst of instances.value) {
    const t = String(inst.deployTarget || 'host-binary') as keyof typeof counts
    if (t in counts) counts[t]++
  }
  const total = instances.value.length
  const calcPct = (n: number) => (total === 0 ? 0 : Math.round((n / total) * 100))
  return [
    { key: 'host-binary', label: '主机二进制 (Systemd)', count: counts['host-binary'], pct: calcPct(counts['host-binary']), cls: 'bar-brand' },
    { key: 'host-container', label: '单机容器 (Podman / Docker)', count: counts['host-container'], pct: calcPct(counts['host-container']), cls: 'bar-running' },
    { key: 'kubernetes', label: 'Kubernetes 集群编排', count: counts.kubernetes, pct: calcPct(counts.kubernetes), cls: 'bar-ok' },
  ]
})

const onboardingSteps = computed(() => [
  {
    num: '01',
    done: hosts.value.length > 0,
    title: '纳管内网主机与基线',
    desc: '通过网段扫描或批量导入注册节点，并执行 22-模块内核初始化。',
    cta: '前往主机资产',
    path: '/hosts',
  },
  {
    num: '02',
    done: instances.value.length > 0,
    title: '选择组件并声明部署',
    desc: `从 ${plugins.value.length} 类内置插件中挑选中间件，自动渲染参数表单并生成 Plan。`,
    cta: '浏览组件市场',
    path: '/instances?tab=catalog',
  },
  {
    num: '03',
    done: operations.value.length > 0,
    title: '拓扑编排与安全发布',
    desc: '通过应用栈 DAG 画布编排多组件依赖，审阅三向差异并追踪实时日志。',
    cta: '打开应用栈编排',
    path: '/stacks',
  },
])

const opRowClass = ({ row }: { row: Record<string, unknown> }) => {
  const s = String(row.state || '')
  if (s === 'failed') return 'ki-row-danger'
  if (s === 'awaiting_approval') return 'ki-row-warn'
  if (s === 'running') return 'ki-row-running'
  if (s === 'succeeded') return 'ki-row-ok'
  return ''
}

watch(() => ws.currentProjectId, loadData)
onMounted(loadData)
</script>

<template>
  <div v-loading="loading" class="ki-dashboard">
    <PageHeader title="总览驾驶舱" :refresh="loadData" :refreshing="loading" />

    <!-- 首屏引导带（仅在项目尚未完成主机或实例接入时呈现，清晰指明交付路径） -->
    <section v-if="showOnboarding" class="onboarding-banner ki-mb-4" aria-label="快速上手指引">
      <div class="onboarding-head">
        <div>
          <h2>快速完成当前项目基础设施初始化</h2>
          <p>按以下三步完成主机接入、核心中间件部署与应用栈编排，全流程支持离线交付与防篡改审计。</p>
        </div>
        <el-button type="primary" @click="router.push(hosts.length === 0 ? '/hosts' : '/instances?tab=catalog')">
          {{ hosts.length === 0 ? '第一步：纳管主机' : '第二步：部署首个组件' }}
          <ArrowRight :size="14" class="ico-r" />
        </el-button>
      </div>

      <div class="onboarding-steps">
        <div
          v-for="step in onboardingSteps"
          :key="step.num"
          class="ob-step"
          :class="{ 'is-done': step.done }"
          @click="router.push(step.path)"
        >
          <div class="ob-step__top">
            <span class="ob-num ki-mono">
              <CheckCircle2 v-if="step.done" :size="15" />
              <template v-else>{{ step.num }}</template>
            </span>
            <strong>{{ step.title }}</strong>
            <ArrowRight :size="14" class="ob-arrow" />
          </div>
          <p>{{ step.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 第一层：左侧交付健康环 + 右侧 4 个 KPI 卡片与核心能力入口 -->
    <div class="dash-top">
      <div class="ki-card score-card">
        <div class="ki-card__title">
          <span>交付健康评分</span>
          <el-link type="primary" underline="never" @click="quickVerifyAudit">立即巡检</el-link>
        </div>

        <div class="gauge-wrap">
          <svg viewBox="0 0 180 112" class="gauge-svg" aria-label="平台交付健康分">
            <path d="M 22 98 A 68 68 0 0 1 158 98" fill="none" class="gauge-track" stroke-width="12" stroke-linecap="round" />
            <path
              d="M 22 98 A 68 68 0 0 1 158 98"
              fill="none"
              class="gauge-fill"
              stroke-width="12"
              stroke-linecap="round"
              :stroke-dasharray="gaugeArcLength"
              :stroke-dashoffset="gaugeDashOffset"
            />
          </svg>
          <div class="gauge-center">
            <b class="ki-num">{{ healthScore }}</b>
            <span>{{ healthScore >= 90 ? '交付状态良好' : healthScore >= 70 ? '存在待关注项' : '需立即处置' }}</span>
          </div>
        </div>

        <div class="score-foot">
          <div><span class="ki-muted">内置插件</span><b class="ki-num">{{ plugins.length }}</b></div>
          <div><span class="ki-muted">执行中</span><b class="ki-num">{{ runningOpsCount }}</b></div>
          <div><span class="ki-muted">异常操作</span><b class="ki-num" :class="{ 'ki-text-danger': failedOpsCount > 0 }">{{ failedOpsCount }}</b></div>
        </div>
      </div>

      <div class="dash-right">
        <div class="ki-grid-4">
          <StatCard
            title="纳管主机"
            :value="hosts.length"
            unit="台"
            :sub="`ARM64-64K 节点 ${arm64kCount} 台`"
            :icon="Server"
            tone="brand"
            clickable
            @click="router.push('/hosts')"
          />
          <StatCard
            title="交付实例"
            :value="instances.length"
            unit="套"
            :sub="`内置 ${plugins.length} 类核心插件`"
            :icon="Box"
            tone="ok"
            clickable
            @click="router.push('/instances')"
          />
          <StatCard
            title="待审批变更"
            :value="ws.pendingApprovalCount"
            unit="单"
            sub="生产高危操作双人复核"
            :icon="ClipboardCheck"
            tone="warn"
            clickable
            @click="router.push('/operations/approvals')"
          />
          <StatCard
            title="审计链状态"
            :value="auditValid === false ? '异常' : '100%'"
            sub="SHA-256 + SM3 双链"
            :icon="ShieldCheck"
            tone="ok"
            clickable
            @click="router.push('/security/audit')"
          />
        </div>

        <div class="quick-cards">
          <div class="quick-card ki-surface" @click="router.push('/instances?tab=catalog')">
            <div class="quick-card__head">
              <Layers :size="16" class="quick-icon" />
              <b>声明式 Plan & 三向漂移审查</b>
              <ArrowRight :size="14" class="ki-muted quick-arrow" />
            </div>
            <p class="ki-muted">动态 JSON-Schema 参数校验，自动识别主机侧手工改动并在变更前生成三向 Diff。</p>
          </div>

          <div class="quick-card ki-surface" @click="router.push('/artifacts/media-policies')">
            <div class="quick-card__head">
              <HardDrive :size="16" class="quick-icon" />
              <b>三级介质源与物理离线策略</b>
              <ArrowRight :size="14" class="ki-muted quick-arrow" />
            </div>
            <p class="ki-muted">统一调度本地制品池、内网 OBS/S3 对象存储与受控上游，离线模式自动阻断公网访问。</p>
          </div>
        </div>
      </div>
    </div>

    <!-- 第二层：部署形态分布 + 内置组件生态 -->
    <div class="ki-grid-2 ki-mt-4">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>部署形态分布<span class="sub">主机二进制 / 容器 / Kubernetes 三态统计</span></span>
          <el-link type="primary" underline="never" @click="router.push('/instances')">管理实例</el-link>
        </div>
        <div class="dist-list">
          <div v-for="item in targetStats" :key="item.key" class="dist-row">
            <span class="dist-label">{{ item.label }}</span>
            <div class="dist-track">
              <i v-if="item.pct > 0" class="dist-bar" :class="item.cls" :style="{ width: `${item.pct}%` }" />
            </div>
            <span class="ki-num dist-val">
              <b>{{ item.count }}</b>
              <small class="ki-muted">{{ item.pct }}%</small>
            </span>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>内置组件生态<span class="sub">{{ plugins.length }} 类基础设施与中间件开箱即用</span></span>
          <el-link type="primary" underline="never" @click="router.push('/instances?tab=catalog')">进入组件市场</el-link>
        </div>
        <div class="plugin-grid">
          <button
            v-for="plg in plugins.slice(0, 12)"
            :key="String(plg.name)"
            type="button"
            class="plugin-tile"
            @click="router.push(`/instances?tab=catalog&q=${encodeURIComponent(String(plg.name))}`)"
          >
            <div class="plugin-tile__top">
              <strong>{{ pluginLabels[String(plg.name)]?.label || plg.name }}</strong>
              <span class="ki-mono plugin-tile__ver">v{{ plg.version }}</span>
            </div>
            <span class="plugin-tile__cat">{{ pluginLabels[String(plg.name)]?.cat || '基础设施' }}</span>
          </button>
        </div>
      </div>
    </div>

    <!-- 第三层：近期执行流水 + 交付实例概览 -->
    <div class="ki-grid-2 ki-mt-4">
      <div class="ki-card">
        <div class="ki-card__title">
          <span><Terminal :size="15" class="title-ico" /> 近期执行流水</span>
          <el-link type="primary" underline="never" @click="router.push('/operations')">全部操作</el-link>
        </div>
        <el-table
          v-if="operations.length > 0"
          :data="operations.slice(0, 8)"
          size="small"
          :row-class-name="opRowClass"
          @row-click="(row: Record<string, unknown>) => router.push(`/operations?id=${row.id}`)"
        >
          <el-table-column label="操作 ID" min-width="175">
            <template #default="{ row }">
              <KiMonoText :value="String(row.id || '')" :head="12" :tail="6" />
            </template>
          </el-table-column>
          <el-table-column prop="action" label="动作" width="88" />
          <el-table-column label="状态" width="128">
            <template #default="{ row }">
              <KiStatusBadge :status="String(row.state || 'pending')" />
            </template>
          </el-table-column>
          <el-table-column label="更新时间" width="108">
            <template #default="{ row }">
              <span class="ki-muted">{{ formatRelTime(String(row.updatedAt || row.createdAt || '')) }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="ki-empty-state">
          <div class="ki-empty-state__icon"><Terminal :size="20" /></div>
          <div class="ki-empty-state__title">暂无执行流水</div>
          <p class="ki-empty-state__desc">执行主机初始化、组件 Apply 或应用栈编排后，实时任务与 SSE 日志将在此处汇总。</p>
          <div class="ki-empty-state__actions">
            <el-button size="small" @click="router.push('/stacks')">
              <GitBranch :size="13" class="ico-l" /> 打开应用栈编排
            </el-button>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>交付实例概览</span>
          <el-link type="primary" underline="never" @click="router.push('/instances')">全部实例</el-link>
        </div>
        <el-table
          v-if="instances.length > 0"
          :data="instances.slice(0, 8)"
          size="small"
          @row-click="router.push('/instances')"
        >
          <el-table-column prop="name" label="实例名称" min-width="140">
            <template #default="{ row }">
              <b class="ki-mono">{{ row.name }}</b>
            </template>
          </el-table-column>
          <el-table-column prop="product" label="组件" width="110" />
          <el-table-column label="交付形态" width="136">
            <template #default="{ row }">
              <KiDomainTag kind="target" :value="String(row.deployTarget || 'host-binary')" />
            </template>
          </el-table-column>
          <el-table-column label="版本" width="90">
            <template #default="{ row }">
              <span class="ki-mono ki-secondary">{{ row.productVersion }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="ki-empty-state">
          <div class="ki-empty-state__icon"><Box :size="20" /></div>
          <div class="ki-empty-state__title">当前项目尚未部署实例</div>
          <p class="ki-empty-state__desc">内置 PostgreSQL、Valkey、Kafka、OpenSearch、Kubernetes 等 12 类核心组件，支持离线一键部署。</p>
          <div class="ki-empty-state__actions">
            <el-button size="small" type="primary" @click="router.push('/instances?tab=catalog')">
              前往组件市场
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.onboarding-banner {
  padding: 18px 20px;
  border-radius: var(--ki-radius-card);
  border: 1px solid var(--ki-banner-border);
  background: var(--ki-banner-bg);
  box-shadow: var(--ki-shadow-surface);
}

.onboarding-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px 16px;
  margin-bottom: 14px;
}

.onboarding-head h2 {
  font-size: var(--ki-fs-page);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.onboarding-head p {
  margin: 4px 0 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.onboarding-steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.ob-step {
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.ob-step:hover {
  border-color: var(--ki-brand);
  box-shadow: var(--ki-shadow-hover);
}

.ob-step.is-done {
  border-color: var(--ki-status-ok-border);
}

.ob-step__top {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.ob-num {
  width: 24px;
  height: 24px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
  font-size: 11px;
  font-weight: var(--ki-fw-bold);
  display: grid;
  place-items: center;
  flex: none;
}

.ob-step.is-done .ob-num {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok);
}

.ob-step__top strong {
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.ob-arrow {
  margin-left: auto;
  color: var(--ki-text-muted);
}

.ob-step p {
  margin: 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.5;
  color: var(--ki-text-muted);
}

.dash-top {
  display: grid;
  grid-template-columns: 272px minmax(0, 1fr);
  gap: var(--ki-space-4);
}

.dash-right {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-4);
  min-width: 0;
}

.score-card {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.gauge-wrap {
  position: relative;
  display: flex;
  justify-content: center;
  padding: 6px 0 4px;
}

.gauge-svg {
  width: 180px;
  height: 110px;
}

.gauge-track {
  stroke: var(--ki-border-subtle);
}

.gauge-fill {
  stroke: var(--ki-status-ok);
  transition: stroke-dashoffset var(--ki-dur-slow) var(--ki-ease);
}

.gauge-center {
  position: absolute;
  bottom: 10px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.gauge-center b {
  font-size: 34px;
  line-height: 1.05;
  color: var(--ki-status-ok);
}

.gauge-center span {
  margin-top: 4px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.score-foot {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  text-align: center;
  padding-top: var(--ki-space-3);
  border-top: 1px dashed var(--ki-border-default);
}

.score-foot div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: var(--ki-fs-xs);
}

.score-foot b {
  font-size: var(--ki-fs-section);
  color: var(--ki-text-primary);
}

.quick-cards {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ki-space-4);
}

.quick-card {
  position: relative;
  padding: 14px 16px;
  cursor: pointer;
  overflow: hidden;
  border-left: 3px solid var(--ki-brand);
  transition: box-shadow var(--ki-dur-base) var(--ki-ease), border-color var(--ki-dur-base) var(--ki-ease);
}

.quick-card:hover {
  box-shadow: var(--ki-shadow-hover);
  border-color: var(--ki-brand);
}

.quick-card__head {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sub);
}

.quick-icon {
  color: var(--ki-brand);
  flex: none;
}

.quick-arrow {
  margin-left: auto;
}

.quick-card p {
  margin: 6px 0 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.5;
}

.dist-list {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-4);
  padding: var(--ki-space-2) 0;
}

.dist-row {
  display: grid;
  grid-template-columns: 185px minmax(0, 1fr) 72px;
  align-items: center;
  gap: var(--ki-space-3);
}

.dist-label {
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
  white-space: nowrap;
}

.dist-track {
  height: 8px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-bg-subtle);
  overflow: hidden;
}

.dist-bar {
  display: block;
  height: 100%;
  border-radius: var(--ki-radius-pill);
}

.bar-brand {
  background-color: var(--ki-brand);
}

.bar-running {
  background-color: var(--ki-status-running);
}

.bar-ok {
  background-color: var(--ki-status-ok);
}

.dist-val {
  display: inline-flex;
  align-items: baseline;
  justify-content: flex-end;
  gap: 5px;
  color: var(--ki-text-primary);
}

.dist-val small {
  font-size: 11px;
}

.plugin-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.plugin-tile {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 9px 11px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
  font-family: var(--ki-font-sans);
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.plugin-tile:hover {
  border-color: var(--ki-brand-border);
  background-color: var(--ki-bg-hover);
}

.plugin-tile__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.plugin-tile__top strong {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plugin-tile__ver {
  font-size: 11px;
  color: var(--ki-text-muted);
  flex: none;
}

.plugin-tile__cat {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.title-ico {
  vertical-align: -2px;
  margin-right: 4px;
  color: var(--ki-brand);
}

.ico-l {
  margin-right: 4px;
}

.ico-r {
  margin-left: 4px;
}

@media (max-width: 1199px) {
  .dash-top,
  .onboarding-steps,
  .quick-cards {
    grid-template-columns: minmax(0, 1fr);
  }

  .plugin-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
