<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowRight,
  Building2,
  CheckCircle2,
  FileCheck2,
  FolderKanban,
  Gauge,
  GitFork,
  KeyRound,
  Network,
  Server,
  ShieldCheck,
  Workflow,
} from 'lucide-vue-next'
import { api } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface PortalOverview {
  probes: { ready: boolean; db: boolean }
  counts: {
    tenants: number
    apps: number
    projects: number
    nodes: number
    nodesOnline: number
    cmdbAssets: number
    configs: number
    jobs: number
    routes: number
  }
  auditChain: { valid: boolean; total: number }
}

const router = useRouter()
const scope = useScopeStore()
const overview = ref<PortalOverview | null>(null)

async function load() {
  const { data } = await api.get<PortalOverview>('/api/v1/platform/overview')
  overview.value = data
}

async function enterWorkspace(tenantId: string) {
  await scope.switchTenant(tenantId)
  router.push('/workspace/overview')
}

async function enterProject(projectId: string, path = '/project/overview') {
  await scope.switchProject(projectId)
  router.push(path)
}

onMounted(load)
</script>

<template>
  <div class="ks-portal">
    <!-- Iconic KubeSphere Dark Slate Welcome Hero Banner -->
    <section class="ks-hero">
      <div class="ks-hero__left">
        <div class="ks-hero__eyebrow">
          <CheckCircle2 :size="14" class="ks-hero__ok" />
          <span>{{ overview?.probes?.ready ? '平台控制面运行正常' : '正在连接控制面' }}</span>
        </div>
        <h1 class="ks-hero__title">你好，{{ scope.context?.displayName || '管理员' }}</h1>
        <p class="ks-hero__sub">
          欢迎使用 LiveOps 云原生运维治理平台。请选择企业空间或项目工作台开始日常资产、配置与发布管理。
        </p>
      </div>

      <div class="ks-hero__metrics">
        <div class="ks-hero-stat">
          <div class="ks-hero-stat__val ki-num">{{ overview?.counts.tenants ?? scope.tenants.length }}</div>
          <div class="ks-hero-stat__label">企业空间</div>
        </div>
        <div class="ks-hero-stat">
          <div class="ks-hero-stat__val ki-num">{{ overview?.counts.cmdbAssets ?? 0 }}</div>
          <div class="ks-hero-stat__label">纳管资产</div>
        </div>
        <div class="ks-hero-stat">
          <div class="ks-hero-stat__val ki-num">{{ overview?.counts.configs ?? 0 }}</div>
          <div class="ks-hero-stat__label">配置集</div>
        </div>
        <div class="ks-hero-stat">
          <div class="ks-hero-stat__val ki-num">{{ overview?.counts.nodes ?? 0 }}</div>
          <div class="ks-hero-stat__label">接入节点</div>
        </div>
      </div>
    </section>

    <!-- Section 1: Quick Project Workbench Shortcuts -->
    <section class="ks-section">
      <div class="ks-section__head">
        <div>
          <h2 class="ks-section__title">常用工作台</h2>
          <p class="ks-section__desc">直达当前项目核心运维能力：CMDB 资产、配置中心与发布编排</p>
        </div>
        <el-button size="small" @click="router.push('/project/overview')">进入当前项目</el-button>
      </div>

      <div class="ks-entry-grid ks-entry-grid--3">
        <button type="button" class="ks-entry-card" @click="enterProject(scope.projectId, '/project/assets')">
          <div class="ks-entry-card__icon ks-icon--ok"><GitFork :size="20" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">配置管理数据库 (CMDB)</div>
            <div class="ks-entry-card__desc">业务/资源双视角树形目录、模型字段与 Excel 批量导入导出</div>
          </div>
          <ArrowRight :size="16" class="ks-entry-card__arrow" />
        </button>

        <button type="button" class="ks-entry-card" @click="enterProject(scope.projectId, '/project/configs')">
          <div class="ks-entry-card__icon ks-icon--blue"><KeyRound :size="20" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">配置中心与环境管理</div>
            <div class="ks-entry-card__desc">加密凭证管理、版本历史对比回滚与多环境配置冲突检测</div>
          </div>
          <ArrowRight :size="16" class="ks-entry-card__arrow" />
        </button>

        <button type="button" class="ks-entry-card" @click="enterProject(scope.projectId, '/project/jobs')">
          <div class="ks-entry-card__icon ks-icon--ok"><Workflow :size="20" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">发布与作业编排</div>
            <div class="ks-entry-card__desc">可视化多阶段并发流水线、任务重试控制与实时执行日志</div>
          </div>
          <ArrowRight :size="16" class="ks-entry-card__arrow" />
        </button>
      </div>
    </section>

    <!-- Section 2: Enterprise Workspaces (KubeSphere Signature Workspace Cards) -->
    <section class="ks-section">
      <div class="ks-section__head">
        <div>
          <h2 class="ks-section__title">企业空间</h2>
          <p class="ks-section__desc">按业务域与租户隔离应用、项目与权限边界</p>
        </div>
        <el-button size="small" @click="router.push('/platform/tenants')">管理全部空间</el-button>
      </div>

      <div class="ks-ws-grid">
        <div v-for="t in scope.tenants" :key="t.tenant_id" class="ks-ws-card" @click="enterWorkspace(t.tenant_id)">
          <div class="ks-ws-card__top">
            <div class="ks-ws-card__icon"><Building2 :size="18" /></div>
            <div class="ks-ws-card__info">
              <div class="ks-ws-card__name">{{ t.name }}</div>
              <div class="ks-ws-card__code ki-mono">{{ t.code }}</div>
            </div>
            <span v-if="t.tenant_id === scope.tenantId" class="ki-badge ks-pill-ok">当前空间</span>
          </div>
          <p class="ks-ws-card__desc">{{ t.description || '独立多租户业务治理空间' }}</p>
          <div v-if="t.tenant_id === scope.tenantId && scope.projects.length > 0" class="ks-ws-card__projects" @click.stop>
            <button
              v-for="p in scope.projects"
              :key="p.project_id"
              type="button"
              class="ks-proj-chip"
              @click="enterProject(p.project_id)"
            >
              <FolderKanban :size="12" />
              <span>{{ p.name }}</span>
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- Section 3: Platform Management Modules -->
    <section class="ks-section">
      <div class="ks-section__head">
        <div>
          <h2 class="ks-section__title">平台管理</h2>
          <p class="ks-section__desc">面向平台管理员的节点基础设施、访问控制、网关与操作审计</p>
        </div>
      </div>

      <div class="ks-entry-grid ks-entry-grid--4">
        <button type="button" class="ks-entry-card" @click="router.push('/platform/overview')">
          <div class="ks-entry-card__icon ks-icon--blue"><Gauge :size="18" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">运行总览</div>
            <div class="ks-entry-card__desc">服务探活与全局资源态势</div>
          </div>
        </button>

        <button type="button" class="ks-entry-card" @click="router.push('/platform/nodes')">
          <div class="ks-entry-card__icon ks-icon--ok"><Server :size="18" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">节点管理</div>
            <div class="ks-entry-card__desc">主机接入、心跳与凭据轮换</div>
          </div>
        </button>

        <button type="button" class="ks-entry-card" @click="router.push('/platform/access')">
          <div class="ks-entry-card__icon ks-icon--blue"><ShieldCheck :size="18" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">访问控制</div>
            <div class="ks-entry-card__desc">账号、角色与接口鉴权策略</div>
          </div>
        </button>

        <button type="button" class="ks-entry-card" @click="router.push('/platform/gateway')">
          <div class="ks-entry-card__icon ks-icon--ok"><Network :size="18" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">网关与制品</div>
            <div class="ks-entry-card__desc">流量路由、监控与制品验签</div>
          </div>
        </button>

        <button type="button" class="ks-entry-card" @click="router.push('/platform/audit')">
          <div class="ks-entry-card__icon ks-icon--blue"><FileCheck2 :size="18" /></div>
          <div class="ks-entry-card__body">
            <div class="ks-entry-card__title">操作审计</div>
            <div class="ks-entry-card__desc">
              {{ overview?.auditChain?.valid ? `审计链完整（${overview.auditChain.total} 条）` : '关键操作记录与完整性校验' }}
            </div>
          </div>
        </button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.ks-portal {
  display: flex;
  flex-direction: column;
  gap: 28px;
}

.ks-hero {
  position: relative;
  padding: 28px 32px;
  border-radius: var(--ki-radius-xl);
  background: var(--ki-login-hero-bg);
  color: var(--ki-login-hero-text);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  overflow: hidden;
  box-shadow: var(--ki-shadow-hover);
}

.ks-hero::before {
  content: "";
  position: absolute;
  inset: 0;
  background: var(--ki-login-hero-glow);
  pointer-events: none;
}

.ks-hero__left {
  position: relative;
  max-width: 560px;
}

.ks-hero__eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-login-hero-badge-bg);
  border: 1px solid var(--ki-login-hero-badge-border);
  color: var(--ki-login-hero-badge-text);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
}

.ks-hero__ok {
  color: var(--ki-login-hero-accent);
}

.ks-hero__title {
  margin: 12px 0 6px;
  font-size: 24px;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
}

.ks-hero__sub {
  margin: 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-login-hero-secondary);
  line-height: 1.6;
}

.ks-hero__metrics {
  position: relative;
  display: grid;
  grid-template-columns: repeat(4, minmax(96px, 1fr));
  gap: 12px;
}

.ks-hero-stat {
  padding: 14px 16px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-login-hero-surface);
  border: 1px solid var(--ki-login-hero-border);
  text-align: center;
}

.ks-hero-stat__val {
  font-size: 24px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-login-hero-text);
}

.ks-hero-stat__label {
  margin-top: 2px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-login-hero-muted);
}

.ks-section__head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}

.ks-section__title {
  font-size: 16px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-section__desc {
  margin: 3px 0 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-entry-grid {
  display: grid;
  gap: 14px;
}

.ks-entry-grid--3 {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.ks-entry-grid--4 {
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
}

@media (max-width: 960px) {
  .ks-entry-grid--3 {
    grid-template-columns: 1fr;
  }
}

.ks-entry-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-surface);
  text-align: left;
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease),
    box-shadow var(--ki-dur-fast) var(--ki-ease),
    transform var(--ki-dur-fast) var(--ki-ease);
}

.ks-entry-card:hover {
  border-color: var(--ki-brand);
  box-shadow: var(--ki-shadow-hover);
  transform: translateY(-1px);
}

.ks-entry-card__icon {
  width: 42px;
  height: 42px;
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

.ks-entry-card__body {
  flex: 1;
  min-width: 0;
}

.ks-entry-card__title {
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-entry-card__desc {
  margin-top: 3px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  line-height: 1.45;
}

.ks-entry-card__arrow {
  color: var(--ki-text-muted);
  flex: none;
}

.ks-ws-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  gap: 14px;
}

.ks-ws-card {
  padding: 18px 20px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-surface);
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition: border-color var(--ki-dur-fast) var(--ki-ease),
    box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.ks-ws-card:hover {
  border-color: var(--ki-brand);
  box-shadow: var(--ki-shadow-hover);
}

.ks-ws-card__top {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ks-ws-card__icon {
  width: 38px;
  height: 38px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-canvas);
  color: var(--ki-text-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}

.ks-ws-card__info {
  flex: 1;
  min-width: 0;
}

.ks-ws-card__name {
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-ws-card__code {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-ws-card__desc {
  margin: 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.ks-ws-card__projects {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-proj-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ks-proj-chip:hover {
  border-color: var(--ki-brand);
  color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}
</style>
