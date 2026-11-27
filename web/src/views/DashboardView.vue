<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Server, Box, ClipboardCheck, ShieldCheck, ArrowRight, Terminal, Layers, HardDrive } from 'lucide-vue-next'
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
  const total = Math.max(1, instances.value.length)
  return [
    { key: 'host-binary', label: '主机二进制 (Systemd)', count: counts['host-binary'], pct: Math.round((counts['host-binary'] / total) * 100), cls: 'bar-brand' },
    { key: 'host-container', label: '单机容器 (Podman/Docker)', count: counts['host-container'], pct: Math.round((counts['host-container'] / total) * 100), cls: 'bar-running' },
    { key: 'kubernetes', label: 'Kubernetes 编排', count: counts.kubernetes, pct: Math.round((counts.kubernetes / total) * 100), cls: 'bar-ok' },
  ]
})

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

    <!-- 第一层：左侧平台健康分环 + 右侧 4 个 KPI 卡片与项目就绪卡片（HCS Dashboard 黄金比例） -->
    <div class="dash-top">
      <div class="ki-card score-card">
        <div class="ki-card__title">
          <span>交付健康评分</span>
          <el-link type="primary" underline="never" @click="quickVerifyAudit">立即巡检</el-link>
        </div>

        <div class="gauge-wrap">
          <svg viewBox="0 0 180 112" class="gauge-svg" aria-label="平台交付健康分">
            <path d="M 22 98 A 68 68 0 0 1 158 98" fill="none" class="gauge-track" stroke-width="13" stroke-linecap="round" />
            <path
              d="M 22 98 A 68 68 0 0 1 158 98"
              fill="none"
              class="gauge-fill"
              stroke-width="13"
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
          <div><span class="ki-muted">运行中</span><b class="ki-num">{{ runningOpsCount }}</b></div>
          <div><span class="ki-muted">失败操作</span><b class="ki-num" :class="{ 'ki-text-danger': failedOpsCount > 0 }">{{ failedOpsCount }}</b></div>
        </div>
      </div>

      <div class="dash-right">
        <div class="ki-grid-4">
          <StatCard
            title="CMDB 纳管主机"
            :value="hosts.length"
            unit="台"
            :sub="`含 arm64-64K 页 ${arm64kCount} 台`"
            :icon="Server"
            tone="brand"
            clickable
            @click="router.push('/hosts')"
          />
          <StatCard
            title="交付实例总数"
            :value="instances.length"
            unit="套"
            :sub="`覆盖 ${plugins.length} 套内置插件`"
            :icon="Box"
            tone="ok"
            clickable
            @click="router.push('/instances')"
          />
          <StatCard
            title="待四眼审批"
            :value="ws.pendingApprovalCount"
            unit="单"
            sub="生产高危变更双人复核"
            :icon="ClipboardCheck"
            tone="warn"
            clickable
            @click="router.push('/operations/approvals')"
          />
          <StatCard
            title="审计链完整度"
            :value="auditValid === false ? '异常' : '100%'"
            sub="SHA-256 + 国密 SM3 双链"
            :icon="ShieldCheck"
            :tone="auditValid === false ? 'danger' : 'ok'"
            clickable
            @click="router.push('/security/audit')"
          />
        </div>

        <!-- 快捷入口卡片带（对标 HCS 租户/能力条） -->
        <div class="quick-cards">
          <div class="quick-card ki-surface" @click="router.push('/instances')">
            <div class="quick-card__head">
              <Layers :size="16" class="quick-icon" />
              <b>两段式 Plan & 3-Way Diff 交付</b>
              <ArrowRight :size="14" class="ki-muted quick-arrow" />
            </div>
            <p class="ki-muted">基于 JSON-Schema 动态表单与外部手工漂移检测，保障生产安全变更。</p>
          </div>

          <div class="quick-card ki-surface" @click="router.push('/artifacts/media-policies')">
            <div class="quick-card__head">
              <HardDrive :size="16" class="quick-icon" />
              <b>三元介质源（Local / OBS·S3 / Online）</b>
              <ArrowRight :size="14" class="ki-muted quick-arrow" />
            </div>
            <p class="ki-muted">物理隔离模式自动封禁公网源，支持 15m 预签名直拉与中转缓存。</p>
          </div>
        </div>
      </div>
    </div>

    <!-- 第二层：三模形态分布 + 插件生态能力概览 -->
    <div class="ki-grid-2 ki-mt-4">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>三元部署形态分布 (ADR-066)</span>
          <el-link type="primary" underline="never" @click="router.push('/instances')">管理实例</el-link>
        </div>
        <div class="dist-list">
          <div v-for="item in targetStats" :key="item.key" class="dist-row">
            <span class="dist-label">{{ item.label }}</span>
            <div class="dist-track">
              <i class="dist-bar" :class="item.cls" :style="{ width: `${Math.max(4, item.pct)}%` }" />
            </div>
            <b class="ki-num dist-val">{{ item.count }}</b>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>内置全栈插件生态<span class="sub">{{ plugins.length }} 套插件开箱即用</span></span>
          <el-link type="primary" underline="never" @click="router.push('/instances')">进入插件市场</el-link>
        </div>
        <div class="plugin-chips">
          <button
            v-for="plg in plugins.slice(0, 12)"
            :key="String(plg.name)"
            type="button"
            class="plugin-pill"
            @click="router.push('/instances')"
          >
            <strong class="ki-mono">{{ plg.name }}</strong>
            <span class="ki-mono ki-muted">v{{ plg.version }}</span>
          </button>
        </div>
      </div>
    </div>

    <!-- 第三层：近期操作流水 + 近期实例清单 -->
    <div class="ki-grid-2 ki-mt-4">
      <div class="ki-card">
        <div class="ki-card__title">
          <span><Terminal :size="15" class="title-ico" /> 近期执行流水</span>
          <el-link type="primary" underline="never" @click="router.push('/operations')">全部操作</el-link>
        </div>
        <el-table
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
          <el-table-column label="状态" width="136">
            <template #default="{ row }">
              <KiStatusBadge :status="String(row.state || 'pending')" />
            </template>
          </el-table-column>
          <el-table-column label="更新时间" width="110">
            <template #default="{ row }">
              <span class="ki-muted">{{ formatRelTime(String(row.updatedAt || row.createdAt || '')) }}</span>
            </template>
          </el-table-column>
          <template #empty><el-empty description="暂无执行记录" :image-size="60" /></template>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>交付实例概览</span>
          <el-link type="primary" underline="never" @click="router.push('/instances')">全部实例</el-link>
        </div>
        <el-table
          :data="instances.slice(0, 8)"
          size="small"
          @row-click="router.push('/instances')"
        >
          <el-table-column prop="name" label="实例名称" min-width="140">
            <template #default="{ row }">
              <b class="ki-mono">{{ row.name }}</b>
            </template>
          </el-table-column>
          <el-table-column prop="product" label="产品组件" width="110" />
          <el-table-column label="交付形态" width="138">
            <template #default="{ row }">
              <KiDomainTag kind="target" :value="String(row.deployTarget || 'host-binary')" />
            </template>
          </el-table-column>
          <el-table-column label="版本" width="90">
            <template #default="{ row }">
              <span class="ki-mono ki-secondary">{{ row.productVersion }}</span>
            </template>
          </el-table-column>
          <template #empty><el-empty description="尚未创建实例，可从插件市场一键部署" :image-size="60" /></template>
        </el-table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash-top {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
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
  width: 184px;
  height: 114px;
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
  border-left: 4px solid var(--ki-brand);
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
  gap: var(--ki-space-3);
}

.dist-row {
  display: grid;
  grid-template-columns: 170px minmax(0, 1fr) 36px;
  align-items: center;
  gap: var(--ki-space-3);
}

.dist-label {
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.dist-track {
  height: 8px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  overflow: hidden;
}

.dist-bar {
  display: block;
  height: 100%;
  border-radius: var(--ki-radius-xs);
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
  text-align: right;
  color: var(--ki-text-primary);
}

.plugin-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ki-space-2);
}

.plugin-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.plugin-pill:hover {
  border-color: var(--ki-brand-border);
  background-color: var(--ki-brand-subtle);
}

.title-ico {
  vertical-align: -2px;
  margin-right: 4px;
  color: var(--ki-brand);
}

@media (max-width: 1199px) {
  .dash-top {
    grid-template-columns: minmax(0, 1fr);
  }

  .quick-cards {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
