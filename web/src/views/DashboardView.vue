<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Blocks,
  HardDrive,
  Network,
  Rocket,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
} from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface OverviewStats {
  totalModels: number
  totalCis: number
  totalHosts: number
  onlineAgents: number
  totalRacks: number
  totalPrefixes: number
  allocatedIps: number
  totalConfigs: number
  totalPlugins: number
  totalReleases: number
  totalArtifacts: number
  modelBreakdown: Record<string, number>
  envBreakdown: Record<string, number>
  recentAuditLogs: number
}

interface ReleaseSummary {
  id: string
  name: string
  env: string
  mode: string
  status: string
  createdBy: string
  createdAt: string
}

interface AuditEntry {
  id: string
  actor: string
  action: string
  targetType: string
  detail: string
  createdAt: string
}

const router = useRouter()
const loading = ref(false)
const stats = ref<OverviewStats>({
  totalModels: 0,
  totalCis: 0,
  totalHosts: 0,
  onlineAgents: 0,
  totalRacks: 0,
  totalPrefixes: 0,
  allocatedIps: 0,
  totalConfigs: 0,
  totalPlugins: 0,
  totalReleases: 0,
  totalArtifacts: 0,
  modelBreakdown: {},
  envBreakdown: {},
  recentAuditLogs: 0,
})
const releases = ref<ReleaseSummary[]>([])
const audits = ref<AuditEntry[]>([])

const modelLabels: Record<string, string> = {
  m_host: '主机节点',
  m_database: '数据库实例',
  m_middleware: '中间件服务',
  m_app: '业务应用',
  m_network_dev: '网络设备',
  m_storage: '存储资源',
  m_domain: '域名路由',
  m_rack: '物理机柜',
}

const modelBars = computed(() => {
  const entries = Object.entries(stats.value.modelBreakdown || {})
  const maxVal = Math.max(1, ...entries.map(([, v]) => v))
  return entries
    .map(([code, count]) => ({
      code,
      label: modelLabels[code] || code,
      count,
      pct: Math.round((count / maxVal) * 100),
    }))
    .sort((a, b) => b.count - a.count)
})

const quickLinks = [
  { title: '动态元模型画布', desc: '自定义 CI 模型、属性 Schema 与关系', path: '/cmdb/models', icon: Network },
  { title: '统一资产台账', desc: '主机即 CI · WebSSH · Excel 批量导入', path: '/cmdb/resources', icon: Server },
  { title: '42U 机柜与 IPAM', desc: '机柜 U 位立面图与 CIDR 自动分配', path: '/cmdb/dcim', icon: HardDrive },
  { title: '多环境配置中心', desc: '五环境隔离 · KMS 显密审计 · 快照回滚', path: '/config', icon: SlidersHorizontal },
  { title: 'YAML 插件与发布', desc: '10 套预置声明式插件 · DAG 灰度拨测', path: '/delivery', icon: Rocket },
  { title: '出站 Agent 舰队', desc: '零信任单向纳管 · 批量作业 · OCI 直推', path: '/ops', icon: Terminal },
]

async function loadAll() {
  loading.value = true
  try {
    const [ov, rel, aud] = await Promise.all([
      apiRequest<OverviewStats>('GET', '/api/v1/overview'),
      apiRequest<{ items: ReleaseSummary[] }>('GET', '/api/v1/delivery/releases'),
      apiRequest<{ items: AuditEntry[] }>('GET', '/api/v1/audit/logs?limit=8'),
    ])
    stats.value = ov.data
    releases.value = (rel.data.items || []).slice(0, 6)
    audits.value = aud.data.items || []
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="dash-page">
    <PageHeader
      title="资产与运维全景"
      subtitle="动态元模型 CMDB · 多环境 KMS 配置 · 声明式 YAML 交付"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="router.push('/cmdb/resources')">进入资产台账</el-button>
        <el-button type="primary" @click="router.push('/delivery')">新建发布编排</el-button>
      </template>
    </PageHeader>

    <div class="kpi-grid">
      <div class="kpi-card ki-card" @click="router.push('/cmdb/models')">
        <div class="kpi-top">
          <span class="kpi-label">CMDB 元模型 / CI 总数</span>
          <Network :size="16" class="kpi-ico" />
        </div>
        <div class="kpi-val ki-num">{{ stats.totalModels }} <small>/ {{ stats.totalCis }} CI</small></div>
        <div class="kpi-foot">动态 Schema 校验 · 多跳影响面</div>
      </div>

      <div class="kpi-card ki-card" @click="router.push('/cmdb/resources')">
        <div class="kpi-top">
          <span class="kpi-label">统一主机资产 / 在线 Agent</span>
          <Server :size="16" class="kpi-ico" />
        </div>
        <div class="kpi-val ki-num">{{ stats.totalHosts }} <small>/ {{ stats.onlineAgents }} 在线</small></div>
        <div class="kpi-foot">Host = m_host CI (SSH + Agent)</div>
      </div>

      <div class="kpi-card ki-card" @click="router.push('/cmdb/dcim')">
        <div class="kpi-top">
          <span class="kpi-label">DCIM 机柜 / IPAM 已分配</span>
          <HardDrive :size="16" class="kpi-ico" />
        </div>
        <div class="kpi-val ki-num">{{ stats.totalRacks }} <small>/ {{ stats.allocatedIps }} IP</small></div>
        <div class="kpi-foot">{{ stats.totalPrefixes }} 个 CIDR 网段纳管中</div>
      </div>

      <div class="kpi-card ki-card" @click="router.push('/config')">
        <div class="kpi-top">
          <span class="kpi-label">配置命名空间 / 声明式插件</span>
          <Blocks :size="16" class="kpi-ico" />
        </div>
        <div class="kpi-val ki-num">{{ stats.totalConfigs }} <small>/ {{ stats.totalPlugins }} 插件</small></div>
        <div class="kpi-foot">KMS 信封加密 · YAML 可扩展</div>
      </div>

      <div class="kpi-card ki-card" @click="router.push('/ops')">
        <div class="kpi-top">
          <span class="kpi-label">发布编排单 / CAS 签名制品</span>
          <ShieldCheck :size="16" class="kpi-ico" />
        </div>
        <div class="kpi-val ki-num">{{ stats.totalReleases }} <small>/ {{ stats.totalArtifacts }} 制品</small></div>
        <div class="kpi-foot">审计哈希链 {{ stats.recentAuditLogs }} 条记录</div>
      </div>
    </div>

    <div class="dash-grid">
      <section class="ki-card">
        <div class="ki-card__title">
          <span>核心能力快捷入口</span>
          <span class="sub">以 CMDB 为单一事实源打通配置、编排与执行面</span>
        </div>
        <div class="quick-grid">
          <button
            v-for="q in quickLinks"
            :key="q.path"
            type="button"
            class="quick-item"
            @click="router.push(q.path)"
          >
            <component :is="q.icon" :size="18" class="quick-ico" />
            <div class="quick-body">
              <b>{{ q.title }}</b>
              <span>{{ q.desc }}</span>
            </div>
          </button>
        </div>

        <div class="section-title">CI 模型实例分布</div>
        <div v-if="modelBars.length === 0" class="ki-muted">暂无资产实例</div>
        <div v-else class="bar-list">
          <div v-for="b in modelBars" :key="b.code" class="bar-row">
            <span class="bar-label">{{ b.label }}</span>
            <div class="bar-track">
              <div class="bar-fill" :style="{ width: `${b.pct}%` }" />
            </div>
            <span class="bar-cnt ki-num">{{ b.count }}</span>
          </div>
        </div>
      </section>

      <section class="ki-card">
        <div class="ki-card__title">
          <span>最近发布编排流水线</span>
          <el-button link type="primary" size="small" @click="router.push('/delivery')">查看全部</el-button>
        </div>
        <el-table v-if="releases.length > 0" :data="releases" size="small">
          <el-table-column prop="name" label="发布单名称" min-width="150" show-overflow-tooltip />
          <el-table-column prop="env" label="环境" width="76" />
          <el-table-column prop="mode" label="模式" width="76" />
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <KiStatusBadge
                :status="row.status === 'SUCCEEDED' ? 'ok' : row.status === 'FAILED' ? 'failed' : row.status === 'PAUSED' ? 'warn' : 'running'"
                :label="row.status"
              />
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="empty-inline ki-muted">
          尚未创建发布单，可前往「编排与发布流水线」基于 YAML 插件库一键发起 DAG 或分批灰度发布。
        </div>

        <div class="section-title">最近防篡改审计事件 (SHA-256 哈希链)</div>
        <div class="audit-feed">
          <div v-for="a in audits" :key="a.id" class="audit-row">
            <span class="audit-act ki-mono">{{ a.action }}</span>
            <span class="audit-detail">{{ a.detail }}</span>
            <span class="audit-who ki-muted">{{ a.actor }}</span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: var(--ki-space-3);
  margin-bottom: var(--ki-space-4);
}

.kpi-card {
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.kpi-card:hover {
  border-color: var(--ki-brand-border);
  box-shadow: var(--ki-shadow-hover);
}

.kpi-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}

.kpi-label {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.kpi-ico {
  color: var(--ki-brand);
}

.kpi-val {
  font-size: 24px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
  line-height: 1.25;
}

.kpi-val small {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-muted);
}

.kpi-foot {
  margin-top: 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.dash-grid {
  display: grid;
  grid-template-columns: 1.15fr 1fr;
  gap: var(--ki-space-4);
}

.quick-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ki-space-2);
}

.quick-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
  font-family: var(--ki-font-sans);
}

.quick-item:hover {
  border-color: var(--ki-brand-border);
  background-color: var(--ki-brand-subtle);
}

.quick-ico {
  flex: none;
  margin-top: 2px;
  color: var(--ki-brand);
}

.quick-body b {
  display: block;
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.quick-body span {
  display: block;
  margin-top: 2px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.bar-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.bar-row {
  display: grid;
  grid-template-columns: 92px 1fr 36px;
  align-items: center;
  gap: 10px;
  font-size: var(--ki-fs-sm);
}

.bar-label {
  color: var(--ki-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.bar-track {
  height: 8px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-bg-subtle);
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-brand);
}

.bar-cnt {
  text-align: right;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.empty-inline {
  padding: 16px 12px;
  font-size: var(--ki-fs-sm);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
}

.audit-feed {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.audit-row {
  display: grid;
  grid-template-columns: 150px 1fr 64px;
  gap: 8px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  font-size: var(--ki-fs-xs);
}

.audit-act {
  color: var(--ki-brand);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audit-detail {
  color: var(--ki-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.audit-who {
  text-align: right;
}

@media (max-width: 1180px) {
  .kpi-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .dash-grid {
    grid-template-columns: 1fr;
  }
}
</style>
