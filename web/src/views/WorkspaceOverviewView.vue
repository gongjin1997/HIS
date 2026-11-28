<script setup lang="ts">
import { useRouter } from 'vue-router'
import { ArrowRight, Boxes, Compass, FolderKanban, Layers } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { useScopeStore } from '@/stores/scope'

const router = useRouter()
const scope = useScopeStore()

async function openProject(pid: string) {
  await scope.switchProject(pid)
  router.push('/project/overview')
}
</script>

<template>
  <div class="ks-page">
    <KsBanner
      :title="scope.currentTenant?.name || '企业空间'"
      description="统一管理当前企业空间下的应用集群、接入密钥以及各环境项目工作台。"
      :icon="Compass"
      :badge="`${scope.apps.length} 个应用 · ${scope.projects.length} 个项目`"
    >
      <template #actions>
        <el-button @click="router.push('/workspace/apps')"><Boxes :size="14" /><span class="ks-gap">应用管理</span></el-button>
        <el-button type="primary" @click="router.push('/workspace/projects')"><FolderKanban :size="14" /><span class="ks-gap">项目管理</span></el-button>
      </template>
    </KsBanner>

    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>空间应用 ({{ scope.apps.length }})</span>
          <el-button size="small" @click="router.push('/workspace/apps')">管理应用</el-button>
        </div>
        <div class="ks-list">
          <div v-for="a in scope.apps" :key="a.app_id" class="ks-list-row">
            <div>
              <div class="ks-list-row__name">{{ a.name }}</div>
              <div class="ks-list-row__sub">{{ a.description || a.code }}</div>
            </div>
            <el-button size="small" :type="a.app_id === scope.appId ? 'primary' : 'default'" @click="scope.switchApp(a.app_id)">
              {{ a.app_id === scope.appId ? '当前选中' : '切换' }}
            </el-button>
          </div>
        </div>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>所属项目 ({{ scope.projects.length }})</span>
          <el-button size="small" @click="router.push('/workspace/projects')">管理项目</el-button>
        </div>
        <div class="ks-list">
          <div v-for="p in scope.projects" :key="p.project_id" class="ks-list-row">
            <div>
              <div class="ks-list-row__name">{{ p.name }}</div>
              <div class="ks-list-row__sub">{{ p.description || p.code }}</div>
            </div>
            <el-button size="small" type="primary" @click="openProject(p.project_id)">
              <Layers :size="12" /><span class="ks-gap-xs">进入工作台</span><ArrowRight :size="12" class="ks-gap-xs" />
            </el-button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-gap-xs {
  margin-left: 4px;
}

.ks-two-col {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ki-space-4);
}

@media (max-width: 980px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
}

.ks-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ks-list-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ks-list-row__name {
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-list-row__sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}
</style>
