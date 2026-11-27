<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowRight,
  Boxes,
  Database,
  GitFork,
  Landmark,
  Network,
  Puzzle,
  Radio,
  Rocket,
  Server,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import StatCard from '@/components/StatCard.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'

interface CMDBStats {
  totalModels: number
  totalCis: number
  totalRelations: number
  totalRacks: number
  totalRackU: number
  usedRackU: number
  totalPrefixes: number
  allocatedIps: number
  totalUsableIps: number
  modelBreakdown: Record<string, number>
  recentHistories: Array<{
    id: string
    ciId: string
    modelCode: string
    action: string
    actor: string
    createdAt: string
  }>
}

const ws = useWorkspaceStore()
const router = useRouter()
const { formatRelTime } = useKiFormat()

const loading = ref(false)
const stats = ref<CMDBStats | null>(null)
const hosts = ref<Array<Record<string, unknown>>>([])
const agents = ref<Array<Record<string, unknown>>>([])
const releases = ref<Array<Record<string, unknown>>>([])
const operations = ref<Array<Record<string, unknown>>>([])
const auditValid = ref<boolean | null>(null)

async function loadData() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [st, h, ag, rel, op] = await Promise.all([
      apiRequest<CMDBStats>('GET', `/api/v1/projects/${pid}/cmdb/stats`).catch(() => ({ data: null as unknown as CMDBStats })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/hosts`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/fleet/agents`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/delivery/releases`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/operations`).catch(() => ({ data: { items: [] } })),
    ])
    stats.value = st.data
    hosts.value = h.data.items || []
    agents.value = ag.data.items || []
    releases.value = rel.data.items || []
    operations.value = op.data.items || []
    await Promise.all([ws.refreshApprovalBadge(), ws.refreshOfflineMode()])
  } finally {
    loading.value = false
  }
}

async function quickVerifyAudit() {
  try {
    const { data } = await apiRequest<{ valid: boolean; totalChecked?: number }>('POST', '/api/v1/audit:verify')
    auditValid.value = data.valid
    if (data.valid) ElMessage.success(`审计防篡改链核验通过（共 ${data.totalChecked ?? 0} 条）`)
    else ElMessage.error('检测到审计链异常，请立即核查')
  } catch {
    auditValid.value = false
  }
}

const onlineAgents = computed(() => agents.value.filter((a) => a.status === 'ONLINE').length)
const rackUtilPct = computed(() => {
  if (!stats.value || stats.value.totalRackU <= 0) return 0
  return Math.round((stats.value.usedRackU / stats.value.totalRackU) * 100)
})
const ipUtilPct = computed(() => {
  if (!stats.value || stats.value.totalUsableIps <= 0) return 0
  return Math.round((stats.value.allocatedIps / stats.value.totalUsableIps) * 100)
})

const breakdownList = computed(() => {
  const m = stats.value?.modelBreakdown || {}
  const entries = Object.entries(m).sort((a, b) => b[1] - a[1])
  const max = entries.length > 0 ? Math.max(...entries.map((e) => e[1]), 1) : 1
  return entries.map(([name, count]) => ({
    name,
    count,
    pct: Math.max(8, Math.round((count / max) * 100)),
  }))
})

const launchpad = [
  { title: '元模型与画布', desc: '自定义模型分组、属性 Schema 校验与可视化关系连线', icon: Boxes, path: '/cmdb/models' },
  { title: '业务与资源双拓扑', desc: '蓝鲸式业务→集群→模块主线树、主机多模块挂载与服务模板', icon: GitFork, path: '/cmdb/topology' },
  { title: '42U 机柜与 IPAM', desc: 'NetBox 式机柜立面图 U 位防冲突上架与 CIDR 自动分配 IP', icon: Landmark, path: '/cmdb/dcim' },
  { title: '配置与密钥中心', desc: '五环境隔离、KV/YAML Diff、不可变快照回滚与审计显密', icon: SlidersHorizontal, path: '/config-center' },
  { title: '声明式插件市场', desc: '在线编排 YAML 插件包（Compose/脚本/探针）动态接入引擎', icon: Puzzle, path: '/plugins' },
  { title: '分批交付与舰队', desc: 'Batch/M1-M3 灰度断点审批、HTTP/TCP 拨测与出站 Agent 舰队', icon: Rocket, path: '/delivery' },
]

const actionLabels: Record<string, string> = {
  create: '新增 CI',
  update: '变更属性',
  delete: '删除 CI',
  collect: '自动采集',
}

watch(() => ws.currentProjectId, loadData)
onMounted(loadData)
</script>

<template>
  <div v-loading="loading" class="ki-dashboard">
    <PageHeader
      title="CMDB 资产大盘 · 总览驾驶舱"
      subtitle="动态元模型 CMDB 资产全景、DCIM 机柜/IPAM、版本交付与双模执行舰队"
      :refresh="loadData"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="quickVerifyAudit">
          <ShieldCheck :size="14" class="btn-ico" /> 核验审计链
        </el-button>
        <el-button @click="router.push('/cmdb/models')">
          <Boxes :size="14" class="btn-ico" /> 元模型画布
        </el-button>
        <el-button type="primary" @click="router.push('/cmdb/resources')">
          <Database :size="14" class="btn-ico" /> 进入 CI 资源库
        </el-button>
      </template>
    </PageHeader>

    <!-- 核心 CMDB 指标总览 -->
    <div class="ki-grid-4 ki-mb-4">
      <StatCard
        title="动态元模型 / CI 实例"
        :value="stats?.totalCis ?? 0"
        unit="个 CI"
        :sub="`${stats?.totalModels ?? 0} 类模型 · ${stats?.totalRelations ?? 0} 条血缘边`"
        :icon="Database"
        tone="brand"
        clickable
        @click="router.push('/cmdb/resources')"
      />
      <StatCard
        title="DCIM 物理机柜 U 位"
        :value="`${stats?.usedRackU ?? 0}/${stats?.totalRackU ?? 0}`"
        unit="U"
        :sub="`${stats?.totalRacks ?? 0} 座机柜 · 利用率 ${rackUtilPct}%`"
        :icon="Landmark"
        tone="ok"
        clickable
        @click="router.push('/cmdb/dcim')"
      />
      <StatCard
        title="IPAM 地址池分配"
        :value="stats?.allocatedIps ?? 0"
        unit="IP"
        :sub="`${stats?.totalPrefixes ?? 0} 个网段 · 共 ${stats?.totalUsableIps ?? 0} 可用 (${ipUtilPct}%)`"
        :icon="Network"
        tone="running"
        clickable
        @click="router.push('/cmdb/dcim')"
      />
      <StatCard
        title="SSH / Agent 双模节点"
        :value="hosts.length + onlineAgents"
        unit="台"
        :sub="`SSH 纳管 ${hosts.length} 台 · 在线 Agent ${onlineAgents} 台`"
        :icon="Radio"
        tone="warn"
        clickable
        @click="router.push('/fleet')"
      />
    </div>

    <!-- 第二层：左侧资产模型分布 + 右侧核心能力快捷矩阵 -->
    <div class="dash-split ki-mb-4">
      <div class="ki-card">
        <div class="card-head">
          <div>
            <h3>CI 模型资产分布</h3>
            <p class="ki-muted sub-note">按动态元模型实时统计资产占比，点击进入对应模型台账与 Excel 导入</p>
          </div>
          <el-button link type="primary" @click="router.push('/cmdb/resources')">查看全部</el-button>
        </div>
        <div v-if="breakdownList.length > 0" class="breakdown-list">
          <div
            v-for="item in breakdownList"
            :key="item.name"
            class="bd-row"
            @click="router.push('/cmdb/resources')"
          >
            <div class="bd-head">
              <span>{{ item.name }}</span>
              <b class="ki-mono">{{ item.count }}</b>
            </div>
            <div class="bd-track">
              <div class="bd-fill" :style="{ width: `${item.pct}%` }" />
            </div>
          </div>
        </div>
        <div v-else class="empty-note ki-muted">暂无 CI 实例数据</div>

        <div class="cap-bars">
          <div class="cap-item">
            <div class="bd-head"><span class="ki-muted">机柜 U 位占用</span><span class="ki-mono">{{ rackUtilPct }}%</span></div>
            <div class="bd-track"><div class="bd-fill bd-fill--ok" :style="{ width: `${rackUtilPct}%` }" /></div>
          </div>
          <div class="cap-item">
            <div class="bd-head"><span class="ki-muted">IPAM 网段分配率</span><span class="ki-mono">{{ ipUtilPct }}%</span></div>
            <div class="bd-track"><div class="bd-fill bd-fill--warn" :style="{ width: `${ipUtilPct}%` }" /></div>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="card-head">
          <div>
            <h3>企业级 CMDB 与交付生态工作台</h3>
            <p class="ki-muted sub-note">对标蓝鲸 bk-cmdb / VeOps / NetBox / LiveOps 全链路能力直达</p>
          </div>
          <el-button link type="primary" @click="router.push('/hosts')">
            <Server :size="13" class="btn-ico" /> 主机采集中心
          </el-button>
        </div>
        <div class="launch-grid">
          <div v-for="card in launchpad" :key="card.path" class="launch-card" @click="router.push(card.path)">
            <div class="launch-card__top">
              <component :is="card.icon" :size="16" class="launch-ico" />
              <b>{{ card.title }}</b>
              <ArrowRight :size="13" class="ki-muted launch-arr" />
            </div>
            <p class="ki-muted launch-desc">{{ card.desc }}</p>
          </div>
        </div>
      </div>
    </div>

    <!-- 第三层：近期 CI 资产变更追踪 + 近期发布与执行流水 -->
    <div class="dash-split">
      <div class="ki-card">
        <div class="card-head">
          <h3>最近 CI 资产变更审计</h3>
          <el-button link type="primary" @click="router.push('/cmdb/resources')">资源库</el-button>
        </div>
        <el-table :data="(stats?.recentHistories || []).slice(0, 6)" size="small">
          <el-table-column label="动作" width="96">
            <template #default="{ row }">
              <span class="ki-badge act-pill">{{ actionLabels[row.action] || row.action }}</span>
            </template>
          </el-table-column>
          <el-table-column prop="modelCode" label="模型" width="110">
            <template #default="{ row }"><span class="ki-mono">{{ row.modelCode }}</span></template>
          </el-table-column>
          <el-table-column label="CI ID" min-width="130">
            <template #default="{ row }"><KiMonoText :value="row.ciId" :head="10" :tail="4" /></template>
          </el-table-column>
          <el-table-column prop="actor" label="操作人" width="100" />
          <el-table-column label="时间" width="100" align="right">
            <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(row.createdAt) }}</span></template>
          </el-table-column>
          <template #empty><span class="ki-muted">暂无 CI 变更记录，前往 CI 资源库创建或导入资产后自动记录快照</span></template>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="card-head">
          <h3>最近交付发布与执行流水</h3>
          <el-button link type="primary" @click="router.push('/delivery')">交付中心 ({{ releases.length }})</el-button>
        </div>
        <el-table :data="operations.slice(0, 6)" size="small">
          <el-table-column label="作业 ID" width="136">
            <template #default="{ row }"><KiMonoText :value="String(row.id || '')" :head="8" :tail="4" /></template>
          </el-table-column>
          <el-table-column prop="type" label="动作类型" min-width="120" />
          <el-table-column label="状态" width="106">
            <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'pending')" /></template>
          </el-table-column>
          <el-table-column label="发起时间" width="100" align="right">
            <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(String(row.createdAt || '')) }}</span></template>
          </el-table-column>
          <template #empty><span class="ki-muted">暂无执行流水，可前往分批交付或批量作业中心触发任务</span></template>
        </el-table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.dash-split {
  display: grid;
  grid-template-columns: 1fr 1.25fr;
  gap: 16px;
}

@media (width <= 1120px) {
  .dash-split {
    grid-template-columns: 1fr;
  }
}

.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.card-head h3 {
  margin: 0;
  font-size: var(--ki-fs-md);
  color: var(--ki-text-primary);
}

.sub-note {
  margin: 3px 0 0;
  font-size: var(--ki-fs-xs);
}

.breakdown-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bd-row {
  cursor: pointer;
}

.bd-head {
  display: flex;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  margin-bottom: 4px;
  color: var(--ki-text-primary);
}

.bd-track {
  height: 7px;
  border-radius: 999px;
  background-color: var(--ki-bg-subtle);
  overflow: hidden;
}

.bd-fill {
  height: 100%;
  border-radius: 999px;
  background-color: var(--ki-brand-primary);
  transition: width 0.25s ease;
}

.bd-fill--ok {
  background-color: var(--ki-status-ok);
}

.bd-fill--warn {
  background-color: var(--ki-status-warn);
}

.cap-bars {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--ki-border-subtle);
}

.launch-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px;
}

.launch-card {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  cursor: pointer;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.launch-card:hover {
  border-color: var(--ki-brand-primary);
  background-color: var(--ki-bg-surface);
}

.launch-card__top {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 5px;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-primary);
}

.launch-ico {
  color: var(--ki-brand-primary);
  flex-shrink: 0;
}

.launch-arr {
  margin-left: auto;
}

.launch-desc {
  margin: 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.45;
}

.act-pill {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.empty-note {
  padding: 20px 0;
  text-align: center;
  font-size: var(--ki-fs-xs);
}
</style>
