<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Activity, ArrowRight, GitFork, KeyRound, Workflow } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

const router = useRouter()
const scope = useScopeStore()
const stats = ref({ assets: 0, models: 0, configs: 0, envs: 0, jobs: 0 })

async function load() {
  const [m, i, c, e, j] = await Promise.all([
    api.get<{ total: number }>('/api/v1/assets/models'),
    api.get<{ total: number }>('/api/v1/assets/instances'),
    api.get<{ total: number }>('/api/v1/config/configurations'),
    api.get<{ total: number }>('/api/v1/config/environments'),
    api.get<{ total: number }>('/api/v1/deploy/jobs'),
  ])
  stats.value = {
    models: m.data.total || 0,
    assets: i.data.total || 0,
    configs: c.data.total || 0,
    envs: e.data.total || 0,
    jobs: j.data.total || 0,
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId, scope.projectId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      :title="`项目工作台：${scope.currentProject?.name || scope.projectId || scope.appId}`"
      description="LiveOps 核心项目交付工作台：统一管理 CMDB 元模型与双拓扑、KMS 加密配置与环境合集，以及强幂等 DAG 部署编排。"
      :icon="Activity"
      :badge="`${scope.tenantId} / ${scope.appId}`"
    />

    <div class="ks-hub-grid">
      <div class="ki-card ks-hub-card" @click="router.push('/project/assets')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-hub-card__icon--ok"><GitFork :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.assets }}</span>
        </div>
        <div class="ks-hub-card__title">CMDB 资产与双视角拓扑</div>
        <p class="ks-hub-card__desc">{{ stats.models }} 类元模型、单父无环业务/资源双树、If-Match 乐观锁与原生 .xlsx 批导</p>
        <div class="ks-hub-card__cta"><span>进入 CMDB</span><ArrowRight :size="14" /></div>
      </div>

      <div class="ki-card ks-hub-card" @click="router.push('/project/configs')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-hub-card__icon--blue"><KeyRound :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.configs }}</span>
        </div>
        <div class="ks-hub-card__title">配置中心与环境冲突预检</div>
        <p class="ks-hub-card__desc">{{ stats.envs }} 套环境合集、v2 KMS 信封加密、审计揭示明文与不可变历史快照</p>
        <div class="ks-hub-card__cta"><span>管理配置</span><ArrowRight :size="14" /></div>
      </div>

      <div class="ki-card ks-hub-card" @click="router.push('/project/jobs')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-hub-card__icon--ok"><Workflow :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.jobs }}</span>
        </div>
        <div class="ks-hub-card__title">DAG 部署编排与 SSE 实时流</div>
        <p class="ks-hub-card__desc">Idempotency-Key 幂等提交、Kahn 成环拦截、lease_epoch 围栏与严格 Last-Event-ID 订阅</p>
        <div class="ks-hub-card__cta"><span>提交与观测</span><ArrowRight :size="14" /></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ks-hub-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: var(--ki-space-4);
}

.ks-hub-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  cursor: pointer;
  transition: box-shadow var(--ki-dur-fast) var(--ki-ease),
    border-color var(--ki-dur-fast) var(--ki-ease);
}

.ks-hub-card:hover {
  box-shadow: var(--ki-shadow-hover);
  border-color: var(--ki-brand);
}

.ks-hub-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ks-hub-card__icon {
  width: 42px;
  height: 42px;
  border-radius: var(--ki-radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
}

.ks-hub-card__icon--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}

.ks-hub-card__icon--blue {
  background-color: var(--ki-status-running-subtle);
  color: var(--ki-status-running-text);
}

.ks-hub-card__metric {
  font-size: 28px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-hub-card__title {
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-hub-card__desc {
  margin: 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
  flex: 1;
}

.ks-hub-card__cta {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-brand);
  padding-top: 8px;
  border-top: 1px solid var(--ki-border-subtle);
}
</style>
