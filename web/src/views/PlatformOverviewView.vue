<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Activity,
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
    role: string
    version: string
    pid: string
    started: boolean
    draining: boolean
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
  auditChain: { valid: boolean; total: number; brokenAt: number }
  recentJobs: Array<{
    job_id: string
    name: string
    state: string
    version: number
    last_seq: number
    created_by: string
    created_at: string
  }>
}

const router = useRouter()
const scope = useScopeStore()
const loading = ref(false)
const overview = ref<OverviewData | null>(null)

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
      description="LiveOps 企业级多租户底座与 KubeSphere 三级治理控制台：实时探活、核心资源计量、防篡改审计链与作业收敛态势。"
      :icon="Gauge"
      :badge="overview?.probes?.ready ? 'Probes Ready' : 'Checking'"
    >
      <template #actions>
        <el-button :loading="loading" @click="load">
          <RefreshCw :size="14" />
          <span class="ks-btn-gap">刷新态势</span>
        </el-button>
        <el-button type="primary" @click="router.push('/project/jobs')">
          <Workflow :size="14" />
          <span class="ks-btn-gap">前往 DAG 部署</span>
        </el-button>
      </template>
    </KsBanner>

    <!-- Primary KubeSphere KPI Ribbon -->
    <div class="ks-kpi-grid">
      <div class="ks-kpi-card" @click="router.push('/platform/tenants')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--ok"><Building2 :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.tenants ?? 0 }}</div>
          <div class="ks-kpi-card__label">企业空间 (Tenants)</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/nodes')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--blue"><Server :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">
            {{ overview?.counts.nodesOnline ?? 0 }}<span class="ks-kpi-card__sub">/{{ overview?.counts.nodes ?? 0 }}</span>
          </div>
          <div class="ks-kpi-card__label">在线零信任节点</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/project/assets')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--ok"><GitFork :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.cmdbAssets ?? 0 }}</div>
          <div class="ks-kpi-card__label">CMDB 纳管资产 ({{ overview?.counts.cmdbModels ?? 0 }} 模型)</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/project/configs')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--blue"><KeyRound :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.configs ?? 0 }}</div>
          <div class="ks-kpi-card__label">KMS 配置集 ({{ overview?.counts.envs ?? 0 }} 环境)</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/gateway')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--ok"><Network :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.counts.routes ?? 0 }}</div>
          <div class="ks-kpi-card__label">动态路由 (v{{ overview?.routeVersion ?? 1 }})</div>
        </div>
      </div>

      <div class="ks-kpi-card" @click="router.push('/platform/audit')">
        <div class="ks-kpi-card__icon ks-kpi-card__icon--ok"><ShieldCheck :size="18" /></div>
        <div>
          <div class="ks-kpi-card__val ki-num">{{ overview?.auditChain.total ?? 0 }}</div>
          <div class="ks-kpi-card__label">
            审计链 {{ overview?.auditChain.valid ? '验签通过' : '待校验' }}
          </div>
        </div>
      </div>
    </div>

    <!-- Two-Column Runtime Health & LiveOps Foundation Guarantees -->
    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>控制面健康探针 (K8s Probes)</span>
          <span class="ki-badge ks-pill-ok">
            <CheckCircle2 :size="13" />
            <span>{{ overview?.probes?.ready ? 'READY' : 'INIT' }}</span>
          </span>
        </div>
        <div class="kv-grid">
          <div class="k">控制面角色</div>
          <div class="ki-mono">{{ overview?.probes?.role || 'liveops-api' }}</div>
          <div class="k">底座版本</div>
          <div class="ki-mono">{{ overview?.probes?.version || 'liveops-foundation/1.0.0' }}</div>
          <div class="k">进程 PID</div>
          <div class="ki-mono">{{ overview?.probes?.pid || '-' }}</div>
          <div class="k">数据库连通</div>
          <div class="ki-text-ok">{{ overview?.probes?.db ? 'Healthy (WAL / RLS)' : 'Disconnected' }}</div>
          <div class="k">探针端点</div>
          <div class="full ki-mono">/startupz · /livez · /readyz · /healthy</div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>LiveOps 底座不变量基线 (40/40 Verified)</span>
          <span class="ki-badge ks-pill-blue">Hexagonal Clean Arch</span>
        </div>
        <div class="ks-guarantee-list">
          <div class="ks-guarantee-item">
            <Activity :size="15" class="ks-g-icon" />
            <div>
              <div class="ks-g-title">D01–D19 作业与租约并发不变量</div>
              <div class="ks-g-desc">Idempotency-Key 幂等重放、Kahn 成环检测、lease_epoch 围栏、Diamond Join 唯一解锁、Downstream SKIPPED 剪枝</div>
            </div>
          </div>
          <div class="ks-guarantee-item">
            <GitFork :size="15" class="ks-g-icon" />
            <div>
              <div class="ks-g-title">S01–S07 租户隔离、CMDB 双拓扑与配置锁</div>
              <div class="ks-g-desc">Composite (tenant_id, app_id) 外键隔离、单父无环树、If-Match 428/412 强乐观锁与原子历史快照</div>
            </div>
          </div>
          <div class="ks-guarantee-item">
            <ShieldCheck :size="15" class="ks-g-icon" />
            <div>
              <div class="ks-g-title">G01–G05 &amp; U01–U09 网关原子切换与严格 SSE 游标</div>
              <div class="ks-g-desc">默认路由单实例锁、制品 VERIFIED 晋升门禁、409/400/410 严格 Last-Event-ID 游标控制</div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Recent Deploy Jobs -->
    <div class="ki-card ks-mt">
      <div class="ki-card__title">
        <span>最近 DAG 部署作业 ({{ scope.appId }})</span>
        <el-button size="small" @click="router.push('/project/jobs')">查看全部作业</el-button>
      </div>
      <el-table :data="overview?.recentJobs || []" size="small" stripe>
        <el-table-column prop="job_id" label="作业 ID" width="210">
          <template #default="{ row }"><span class="ki-mono">{{ row.job_id }}</span></template>
        </el-table-column>
        <el-table-column prop="name" label="作业名称" min-width="220" />
        <el-table-column prop="state" label="状态" width="130">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.state === 'SUCCEEDED' ? 'ks-pill-ok' : row.state === 'FAILED' ? 'ks-pill-danger' : 'ks-pill-blue'">
              {{ row.state }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="last_seq" label="Timeline 水位" width="130">
          <template #default="{ row }"><span class="ki-mono">seq #{{ row.last_seq }}</span></template>
        </el-table-column>
        <el-table-column prop="created_by" label="提交者" width="130" />
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.ks-btn-gap {
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

.ks-kpi-card__icon--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}

.ks-kpi-card__icon--blue {
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
}

.ks-guarantee-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ks-guarantee-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ks-g-icon {
  color: var(--ki-brand);
  margin-top: 2px;
  flex: none;
}

.ks-g-title {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-g-desc {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-mt {
  margin-top: var(--ki-space-4);
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
