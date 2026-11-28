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
      :title="scope.currentProject?.name || scope.currentApp?.name || '项目工作台'"
      description="统一管理当前项目的 CMDB 业务与资源资产、多环境加密配置集及并发发布流水线。"
      :icon="Activity"
      :badge="scope.currentApp?.name || scope.appId"
    />

    <div class="ks-hub-grid">
      <div class="ki-card ks-hub-card" @click="router.push('/project/assets')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-icon--ok"><GitFork :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.assets }}</span>
        </div>
        <div class="ks-hub-card__title">配置管理数据库 (CMDB)</div>
        <p class="ks-hub-card__desc">已内置 {{ stats.models }} 类标准模型，支持业务/资源双树目录、自定义拓扑与 Excel 批量导入</p>
        <div class="ks-hub-card__cta"><span>管理资产与拓扑</span><ArrowRight :size="14" /></div>
      </div>

      <div class="ki-card ks-hub-card" @click="router.push('/project/configs')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-icon--blue"><KeyRound :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.configs }}</span>
        </div>
        <div class="ks-hub-card__title">配置中心与环境管理</div>
        <p class="ks-hub-card__desc">管理 {{ stats.envs }} 套环境配置合集，提供加密凭证受控查阅、版本 Diff 对比与一键回滚</p>
        <div class="ks-hub-card__cta"><span>管理配置与环境</span><ArrowRight :size="14" /></div>
      </div>

      <div class="ki-card ks-hub-card" @click="router.push('/project/jobs')">
        <div class="ks-hub-card__head">
          <div class="ks-hub-card__icon ks-icon--ok"><Workflow :size="20" /></div>
          <span class="ki-num ks-hub-card__metric">{{ stats.jobs }}</span>
        </div>
        <div class="ks-hub-card__title">发布与作业编排</div>
        <p class="ks-hub-card__desc">支持多阶段并发发布流水线可视化追踪、实时日志流与异常分支隔离保护</p>
        <div class="ks-hub-card__cta"><span>发起与追踪发布</span><ArrowRight :size="14" /></div>
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

.ks-icon--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}

.ks-icon--blue {
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
