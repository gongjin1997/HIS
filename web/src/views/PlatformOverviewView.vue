<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Building2,
  CheckCircle2,
  Gauge,
  GitFork,
  KeyRound,
  Network,
  RefreshCw,
  Server,
  ShieldCheck,
  Workflow,
} from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface OverviewData {
  probes: {
    version: string
    pid: string
    db: boolean
    ready: boolean
    timeUtc: string
  }
  counts: {
    tenants: number
    apps: number
    projects: number
    users: number
    nodes: number
    nodesOnline: number
    cmdbModels: number
    cmdbAssets: number
    configs: number
    envs: number
    jobs: number
    routers: number
    routes: number
  }
  routeVersion: number
  auditChain: { valid: boolean; total: number }
  recentJobs: Array<{
    job_id: string
    name: string
    state: string
    created_by: string
    created_at: string
  }>
}

const router = useRouter()
const scope = useScopeStore()
const loading = ref(false)
const overview = ref<OverviewData | null>(null)

function stateLabel(st: string) {
  const map: Record<string, string> = {
    SUCCEEDED: '已完成',
    RUNNING: '执行中',
    PENDING: '等待中',
    FAILED: '执行失败',
    CANCELLED: '已取消',
  }
  return map[st] || st
}

function fmtTime(iso: string) {
  if (!iso) return '-'
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<OverviewData>('/api/v1/platform/overview')
    overview.value = data
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="平台运行总览"
      description="实时掌握全局企业空间、主机节点、资产台账、配置服务与近期发布任务运行状态。"
      :icon="Gauge"
      :badge="overview?.probes?.ready ? '服务正常' : '检测中'"
    >
      <template #actions>
        <el-button :loading="loading" @click="load">
          <RefreshCw :size="14" />
          <span class="ks-gap">刷新数据</span>
        </el-button>
        <el-button type="primary" @click="router.push('/project/jobs')">
          <Workflow :size="14" />
          <span class="ks-gap">进入发布编排</span>
        </el-button>
      </template>
    </KsBanner>

    <div class="ks-kpi-grid">
      <div class="ks-kpi-card" @click="router.push('/platform/tenants')">
        <div class="ks-kpi-card__icon ks-icon--ok"><Building2 :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.tenants ?? 0 }}</div>
          <div class="ks-kpi-card__label">企业空间</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/nodes')">
        <div class="ks-kpi-card__icon ks-icon--blue"><Server :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">
            {{ overview?.counts.nodesOnline ?? 0 }}<span class="ks-kpi-card__sub">/{{ overview?.counts.nodes ?? 0 }}</span>
          </div>
          <div class="ks-kpi-card__label">在线主机节点</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/project/assets')">
        <div class="ks-kpi-card__icon ks-icon--ok"><GitFork :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.cmdbAssets ?? 0 }}</div>
          <div class="ks-kpi-card__label">CMDB 纳管资产（{{ overview?.counts.cmdbModels ?? 0 }} 类模型）</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/project/configs')">
        <div class="ks-kpi-card__icon ks-icon--blue"><KeyRound :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.configs ?? 0 }}</div>
          <div class="ks-kpi-card__label">应用配置集（{{ overview?.counts.envs ?? 0 }} 套环境）</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/gateway')">
        <div class="ks-kpi-card__icon ks-icon--ok"><Network :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.routes ?? 0 }}</div>
          <div class="ks-kpi-card__label">活跃路由规则</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/audit')">
        <div class="ks-kpi-card__icon ks-icon--ok"><ShieldCheck :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.auditChain.total ?? 0 }}</div>
          <div class="ks-kpi-card__label">操作审计记录（{{ overview?.auditChain.valid ? '完整可信' : '待核验' }}）</div>
        </div>
      </div>
    </div>

    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>核心服务状态</span>
          <span class="ki-badge ks-pill-ok"><CheckCircle2 :size="12" /><span>全部健康</span></span>
        </div>
        <div class="ks-svc-list">
          <div class="ks-svc-row">
            <div>
              <div class="ks-svc-row__name">平台控制面服务</div>
              <div class="ks-svc-row__sub">提供身份认证、多租户隔离与业务接口服务</div>
            </div>
            <span class="ki-badge ks-pill-ok">运行正常</span>
          </div>
          <div class="ks-svc-row">
            <div>
              <div class="ks-svc-row__name">元数据与事务存储引擎</div>
              <div class="ks-svc-row__sub">保障资产台账、配置快照与作业状态强一致持久化</div>
            </div>
            <span class="ki-badge ks-pill-ok">{{ overview?.probes?.db ? '已连接' : '异常' }}</span>
          </div>
          <div class="ks-svc-row">
            <div>
              <div class="ks-svc-row__name">后台调度与健康巡检引擎</div>
              <div class="ks-svc-row__sub">异步调度多阶段发布任务与节点心跳超时监测</div>
            </div>
            <span class="ki-badge ks-pill-ok">巡检中</span>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>近期发布记录</span>
          <el-button size="small" @click="router.push('/project/jobs')">全部作业</el-button>
        </div>
        <el-table :data="overview?.recentJobs || []" size="small" empty-text="暂无发布作业记录">
          <el-table-column prop="name" label="发布名称" min-width="180" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <span class="ki-badge" :class="row.state === 'SUCCEEDED' ? 'ks-pill-ok' : row.state === 'FAILED' ? 'ks-pill-danger' : 'ks-pill-blue'">
                {{ stateLabel(row.state) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column prop="created_by" label="发起人" width="90" />
          <el-table-column label="创建时间" width="160">
            <template #default="{ row }"><span class="ki-muted">{{ fmtTime(row.created_at) }}</span></template>
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-kpi-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--ki-space-3);
  margin-bottom: var(--ki-space-4);
}

@media (max-width: 900px) {
  .ks-kpi-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.ks-kpi-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px 18px;
  border-radius: var(--ki-radius-card);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-surface);
  cursor: pointer;
  transition: box-shadow var(--ki-dur-fast) var(--ki-ease),
    border-color var(--ki-dur-fast) var(--ki-ease);
}

.ks-kpi-card:hover {
  box-shadow: var(--ki-shadow-hover);
  border-color: var(--ki-border-strong);
}

.ks-kpi-card__icon {
  width: 40px;
  height: 40px;
  border-radius: var(--ki-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}

.ks-icon--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}

.ks-icon--blue {
  background-color: var(--ki-status-running-subtle);
  color: var(--ki-status-running-text);
}

.ks-kpi-card__val {
  font-size: 24px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
  line-height: 1.2;
}

.ks-kpi-card__sub {
  font-size: 14px;
  color: var(--ki-text-muted);
  font-weight: var(--ki-fw-medium);
}

.ks-kpi-card__label {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-two-col {
  display: grid;
  grid-template-columns: 1fr 1.25fr;
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
}

.ks-svc-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ks-svc-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ks-svc-row__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-svc-row__sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}
</style>
