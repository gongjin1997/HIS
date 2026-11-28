<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowRight, Building2, Plus, RefreshCw } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore, type TenantItem } from '@/stores/scope'

const router = useRouter()
const scope = useScopeStore()
const tenants = ref<TenantItem[]>([])
const loading = ref(false)
const dialogOpen = ref(false)
const form = ref({ code: '', name: '', description: '' })

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: TenantItem[] }>('/api/v1/iam/tenants')
    tenants.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function submitCreate() {
  try {
    await api.post('/api/v1/iam/tenants', form.value)
    ElMessage.success('企业空间已创建并自动初始化默认应用与 CMDB 元模型')
    dialogOpen.value = false
    form.value = { code: '', name: '', description: '' }
    await Promise.all([load(), scope.refreshMe()])
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function enterWorkspace(tid: string) {
  await scope.switchTenant(tid)
  router.push('/workspace/overview')
}

onMounted(load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="企业空间 / 多租户管理"
      description="对齐 KubeSphere 企业空间模型与 LiveOps Composite (tenant_id, app_id) 物理隔离边界，每个空间独立隔离 CMDB、配置集与部署作业。"
      :icon="Building2"
      :badge="`${tenants.length} 个空间`"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="dialogOpen = true"><Plus :size="14" /><span class="ks-gap">创建企业空间</span></el-button>
      </template>
    </KsBanner>

    <div class="ks-ws-grid">
      <div v-for="t in tenants" :key="t.tenant_id" class="ki-card ks-ws-card">
        <div class="ks-ws-card__head">
          <div class="ks-ws-card__icon"><Building2 :size="20" /></div>
          <div class="ks-ws-card__title-wrap">
            <div class="ks-ws-card__name">{{ t.name }}</div>
            <div class="ks-ws-card__code ki-mono">{{ t.tenant_id }} · {{ t.code }}</div>
          </div>
          <span class="ki-badge ks-pill-ok">{{ t.status }}</span>
        </div>
        <p class="ks-ws-card__desc">{{ t.description || '独立多租户隔离企业空间' }}</p>
        <div class="ks-ws-card__foot">
          <span class="ks-ws-card__active" v-if="t.tenant_id === scope.tenantId">当前活跃空间</span>
          <span v-else class="ki-muted">RLS 物理隔离</span>
          <el-button size="small" type="primary" @click="enterWorkspace(t.tenant_id)">
            <span>进入空间</span>
            <ArrowRight :size="13" class="ks-gap" />
          </el-button>
        </div>
      </div>
    </div>

    <el-dialog v-model="dialogOpen" title="创建企业空间 (Tenant)" width="460px">
      <el-form label-position="top">
        <el-form-item label="空间编码 (唯一标识)" required>
          <el-input v-model="form.code" placeholder="例如: east-campus" />
        </el-form-item>
        <el-form-item label="空间名称" required>
          <el-input v-model="form.name" placeholder="例如: 东院区临床业务空间" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">取消</el-button>
        <el-button type="primary" @click="submitCreate">确认创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-ws-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
  gap: var(--ki-space-4);
}

.ks-ws-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ks-ws-card__head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ks-ws-card__icon {
  width: 40px;
  height: 40px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}

.ks-ws-card__title-wrap {
  min-width: 0;
  flex: 1;
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
  min-height: 36px;
}

.ks-ws-card__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-top: 10px;
  border-top: 1px solid var(--ki-border-subtle);
  font-size: var(--ki-fs-xs);
}

.ks-ws-card__active {
  color: var(--ki-status-ok-text);
  font-weight: var(--ki-fw-semibold);
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}
</style>
