<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { KeyRound } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()
const { formatAbsTime } = useKiFormat()

const loading = ref(false)
const instances = ref<Array<Record<string, unknown>>>([])
const selectedClusterId = ref('')
const kubeconfigs = ref<Array<Record<string, unknown>>>([])
const issuedYaml = ref('')
const revokeTarget = ref<string | null>(null)
const revokeModalOpen = ref(false)

const reqForm = reactive({ k8sRole: 'view', ttlHours: 8 })

async function loadClusters() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/instances`)
    instances.value = data.items || []
    if (instances.value.length > 0 && !selectedClusterId.value) {
      selectedClusterId.value = String(instances.value[0].id)
    }
    if (selectedClusterId.value) await loadKubeconfigs()
  } finally {
    loading.value = false
  }
}

async function loadKubeconfigs() {
  if (!ws.currentProjectId || !selectedClusterId.value) return
  const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/clusters/${selectedClusterId.value}/kubeconfigs`
  )
  kubeconfigs.value = data.items || []
}

async function issueKubeconfig() {
  if (!selectedClusterId.value) return
  try {
    const { data } = await apiRequest<{ kubeconfigYaml: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/clusters/${selectedClusterId.value}/kubeconfigs`,
      { body: { k8sRole: reqForm.k8sRole, ttlHours: reqForm.ttlHours }, idempotent: true }
    )
    issuedYaml.value = data.kubeconfigYaml
    ElMessage.success('已签发短期 RBAC kubeconfig（零 system:masters）')
    await loadKubeconfigs()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function askRevoke(kcId: string) {
  revokeTarget.value = kcId
  revokeModalOpen.value = true
}

async function confirmRevoke() {
  if (!revokeTarget.value) return
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/clusters/kubeconfigs/${revokeTarget.value}`)
  ElMessage.success('kubeconfig 已吊销并记入审计链')
  revokeModalOpen.value = false
  revokeTarget.value = null
  await loadKubeconfigs()
}

watch(() => ws.currentProjectId, loadClusters)
onMounted(loadClusters)
</script>

<template>
  <div>
    <PageHeader title="多集群管理" :refresh="loadClusters" :refreshing="loading" />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>短期 RBAC Kubeconfig 签发<span class="sub">默认 8h · 上限 24h · cluster-admin 强封顶 4h · 永不签发 system:masters</span></span>
      </div>
      <div class="filter-bar">
        <el-select v-model="selectedClusterId" placeholder="选择目标集群实例" class="sel-cluster" @change="loadKubeconfigs">
          <el-option v-for="c in instances" :key="String(c.id)" :label="`${c.name} (${c.product})`" :value="String(c.id)" />
        </el-select>
        <el-select v-model="reqForm.k8sRole" class="sel-role">
          <el-option label="view (只读)" value="view" />
          <el-option label="edit (命名空间编辑)" value="edit" />
          <el-option label="ns-admin" value="ns-admin" />
          <el-option label="cluster-admin (上限4h)" value="cluster-admin" />
        </el-select>
        <el-input-number v-model="reqForm.ttlHours" :min="1" :max="24" />
        <el-button
          v-if="auth.can('cluster:kubeconfig', ws.currentProjectId)"
          type="primary"
          :disabled="!selectedClusterId"
          @click="issueKubeconfig"
        >
          <KeyRound :size="14" class="btn-ico" /> 签发短期 Kubeconfig
        </el-button>
      </div>
      <pre v-if="issuedYaml" class="ki-mono yaml-out">{{ issuedYaml }}</pre>
    </div>

    <div class="ki-card">
      <div class="ki-card__title">
        <span>已签发 Kubeconfig 凭据台账<span class="sub">共 {{ kubeconfigs.length }} 条</span></span>
      </div>
      <el-table v-loading="loading" :data="kubeconfigs">
        <el-table-column label="凭据 ID" width="220">
          <template #default="{ row }"><KiMonoText :value="String(row.id || '')" /></template>
        </el-table-column>
        <el-table-column prop="k8sRole" label="RBAC 角色" width="150" />
        <el-table-column label="到期时间" min-width="180">
          <template #default="{ row }"><span class="ki-mono">{{ formatAbsTime(String(row.expiresAt || '')) }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <KiStatusBadge :status="row.revokedAt ? 'revoked' : 'active'" :label="row.revokedAt ? '已吊销' : '有效'" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="auth.can('cluster:kubeconfig', ws.currentProjectId)"
              link
              type="danger"
              :disabled="Boolean(row.revokedAt)"
              @click="askRevoke(String(row.id))"
            >
              吊销
            </el-button>
          </template>
        </el-table-column>
        <template #empty><el-empty description="尚未签发短期 Kubeconfig" :image-size="60" /></template>
      </el-table>
    </div>

    <KiDangerConfirmModal
      v-model="revokeModalOpen"
      :env="ws.currentEnv"
      title="确认吊销短期 Kubeconfig"
      :resource-name="revokeTarget || ''"
      risk-summary="吊销后该凭据将立即失效，任何使用该 kubeconfig 的自动化脚本或终端会立刻断开连接。"
      confirm-label="立即吊销"
      @confirm="confirmRevoke"
    />
  </div>
</template>

<style scoped>
.sel-cluster {
  width: 240px;
}

.sel-role {
  width: 190px;
}

.btn-ico {
  margin-right: 4px;
}

.yaml-out {
  margin: 12px 0 0;
  padding: 12px 14px;
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  border-radius: var(--ki-radius-sm);
  max-height: 220px;
  overflow: auto;
}
</style>
