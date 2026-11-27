<script setup lang="ts">
import { computed, ref } from 'vue'
import { AlertTriangle, Layers, Search, ShieldCheck } from 'lucide-vue-next'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

export interface ProcessInventoryItem {
  hostId: string
  hostName: string
  hostAddress: string
  proto: string
  address: string
  port: number
  process: string
  classification: 'managed' | 'system' | 'shadow'
  instanceId?: string
  instanceName?: string
  product?: string
}

export interface HostImpactSummary {
  hostId: string
  hostName: string
  address: string
  state: string
  biz: string
  set: string
  module: string
  region: string
  securityArea: string
  faultZone: string
  instanceCount: number
  stackCount: number
  claimedMemMb: number
  totalMemMb: number
  listeningCount: number
  shadowPortCount: number
  affectedStacks?: string[]
  instances?: Array<{
    instanceId: string
    instanceName: string
    product: string
    env: string
    role: string
    stackName?: string
    memMb: number
  }>
  listeningEntries?: ProcessInventoryItem[]
}

const props = defineProps<{
  impacts: HostImpactSummary[]
  processes: ProcessInventoryItem[]
}>()

const emit = defineEmits<{
  (e: 'open-host', hostId: string): void
}>()

const subView = ref<'impact' | 'processes'>('impact')
const clsFilter = ref<'all' | 'shadow' | 'managed' | 'system'>('all')
const kw = ref('')

const shadowTotal = computed(() => props.processes.filter((p) => p.classification === 'shadow').length)
const managedTotal = computed(() => props.processes.filter((p) => p.classification === 'managed').length)

const filteredProcesses = computed(() => {
  const q = kw.value.trim().toLowerCase()
  return props.processes.filter((p) => {
    if (clsFilter.value !== 'all' && p.classification !== clsFilter.value) return false
    if (!q) return true
    return `${p.hostName} ${p.hostAddress} ${p.port} ${p.process} ${p.instanceName || ''}`.toLowerCase().includes(q)
  })
})

function clsLabel(cls: string): string {
  if (cls === 'managed') return '已纳管实例'
  if (cls === 'system') return '系统端口'
  return '影子服务'
}

function clsStatus(cls: string): string {
  if (cls === 'managed') return 'ok'
  if (cls === 'system') return 'pending'
  return 'warn'
}
</script>

<template>
  <div>
    <div class="ki-flex-between ki-flex-wrap ki-mb-3">
      <div class="ki-pill-tabs">
        <button
          type="button"
          class="ki-pill-tab"
          :class="{ 'ki-pill-tab--active': subView === 'impact' }"
          @click="subView = 'impact'"
        >
          <Layers :size="13" />
          <span>CI 关联影响面 (Blast Radius)</span>
          <span class="ki-pill-tab__count ki-num">{{ impacts.length }}</span>
        </button>
        <button
          type="button"
          class="ki-pill-tab"
          :class="{ 'ki-pill-tab--active': subView === 'processes' }"
          @click="subView = 'processes'"
        >
          <ShieldCheck :size="13" />
          <span>全网进程与监听端口盘点</span>
          <span class="ki-pill-tab__count ki-num">{{ processes.length }}</span>
        </button>
      </div>

      <div class="ki-flex-row summary-badges">
        <span class="ki-badge managed-badge">已纳管端口 {{ managedTotal }}</span>
        <span class="ki-badge shadow-badge"><AlertTriangle :size="12" /> 影子服务端口 {{ shadowTotal }}</span>
      </div>
    </div>

    <template v-if="subView === 'impact'">
      <el-table v-if="impacts.length > 0" :data="impacts" size="small">
        <el-table-column label="主机 CI" min-width="150">
          <template #default="{ row }">
            <div class="ci-host ki-mono">{{ row.hostName }}</div>
            <div class="ki-muted ki-mono ci-sub">{{ row.address }}</div>
          </template>
        </el-table-column>
        <el-table-column label="业务拓扑路径" min-width="175">
          <template #default="{ row }">{{ row.biz }} / {{ row.set }} / {{ row.module }}</template>
        </el-table-column>
        <el-table-column label="承载实例 CI" min-width="210">
          <template #default="{ row }">
            <div v-if="row.instances?.length" class="inst-chips">
              <span v-for="inst in row.instances" :key="`${inst.instanceId}-${inst.role}`" class="ki-badge ki-mono inst-pill">
                {{ inst.instanceName || inst.instanceId }} ({{ inst.product }})
              </span>
            </div>
            <span v-else class="ki-muted">空闲节点（无活跃实例）</span>
          </template>
        </el-table-column>
        <el-table-column label="关联应用栈" min-width="140">
          <template #default="{ row }">
            <span v-if="row.affectedStacks?.length" class="ki-mono">{{ row.affectedStacks.join(', ') }}</span>
            <span v-else class="ki-muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="端口 / 影子风险" width="136">
          <template #default="{ row }">
            <span class="ki-num">{{ row.listeningCount }} 监听</span>
            <span v-if="row.shadowPortCount > 0" class="shadow-inline"> · {{ row.shadowPortCount }} 影子</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="96" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="emit('open-host', row.hostId)">影响面详情</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div v-else class="ki-empty-state">
        <div class="ki-empty-state__title">暂无主机 CI 影响面数据</div>
        <p class="ki-empty-state__desc">纳管主机并部署实例后，此处自动呈现“主机 → 实例 → 应用栈”的依赖影响关系。</p>
      </div>
    </template>

    <template v-else>
      <div class="filter-bar ki-mb-3">
        <el-radio-group v-model="clsFilter" size="small">
          <el-radio-button value="all">全部 ({{ processes.length }})</el-radio-button>
          <el-radio-button value="shadow">影子服务 ({{ shadowTotal }})</el-radio-button>
          <el-radio-button value="managed">已纳管 ({{ managedTotal }})</el-radio-button>
          <el-radio-button value="system">系统端口</el-radio-button>
        </el-radio-group>
        <el-input v-model="kw" size="small" clearable placeholder="搜索主机 / 端口 / 进程 / 实例" class="proc-kw">
          <template #prefix><Search :size="13" /></template>
        </el-input>
      </div>

      <el-table v-if="filteredProcesses.length > 0" :data="filteredProcesses" size="small" max-height="420">
        <el-table-column label="主机" min-width="145">
          <template #default="{ row }">
            <b class="ki-mono">{{ row.hostName }}</b>
            <div class="ki-muted ki-mono ci-sub">{{ row.hostAddress }}</div>
          </template>
        </el-table-column>
        <el-table-column label="监听端点" min-width="150">
          <template #default="{ row }"><span class="ki-mono">{{ row.proto }}://{{ row.address }}:{{ row.port }}</span></template>
        </el-table-column>
        <el-table-column label="进程名" min-width="120">
          <template #default="{ row }"><span class="ki-mono">{{ row.process || '—' }}</span></template>
        </el-table-column>
        <el-table-column label="纳管分类" width="116">
          <template #default="{ row }">
            <KiStatusBadge :status="clsStatus(row.classification)" :label="clsLabel(row.classification)" />
          </template>
        </el-table-column>
        <el-table-column label="关联实例" min-width="150">
          <template #default="{ row }">
            <span v-if="row.instanceName" class="ki-mono">{{ row.instanceName }} ({{ row.product }})</span>
            <span v-else class="ki-muted">{{ row.classification === 'shadow' ? '未纳管进程，部署前将触发端口冲突告警' : '系统服务' }}</span>
          </template>
        </el-table-column>
      </el-table>
      <div v-else class="ki-empty-state">
        <div class="ki-empty-state__title">暂无监听端口盘点记录</div>
        <p class="ki-empty-state__desc">对纳管主机执行「探活」后，系统将在 1-RTT 内自动采集全机监听端口与进程并识别未纳管影子服务。</p>
      </div>
    </template>
  </div>
</template>

<style scoped>
.summary-badges {
  font-size: var(--ki-fs-xs);
}

.managed-badge {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}

.shadow-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
}

.ci-host {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ci-sub {
  font-size: 11px;
}

.inst-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.inst-pill {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.shadow-inline {
  color: var(--ki-status-warn-text);
  font-weight: var(--ki-fw-semibold);
}

.proc-kw {
  width: 240px;
}
</style>
