<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Copy, Download, KeyRound, Network, Plus } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()
const { formatAbsTime } = useKiFormat()

const loading = ref(false)
const allInstances = ref<Array<Record<string, unknown>>>([])
const selectedClusterId = ref('')
const kubeconfigs = ref<Array<Record<string, unknown>>>([])
const issuedYaml = ref('')
const revokeTarget = ref<string | null>(null)
const revokeModalOpen = ref(false)

const reqForm = reactive({ k8sRole: 'view', ttlHours: 8 })

const k8sClusters = computed(() =>
  allInstances.value.filter(
    (c) => String(c.product || '').toLowerCase() === 'kubernetes' || String(c.deployTarget || '') === 'kubernetes'
  )
)

const selectedCluster = computed(
  () => k8sClusters.value.find((c) => String(c.id) === selectedClusterId.value) || null
)

async function loadClusters() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/instances`
    )
    allInstances.value = data.items || []
    const valid = k8sClusters.value.some((c) => String(c.id) === selectedClusterId.value)
    if (!valid) {
      selectedClusterId.value = k8sClusters.value.length > 0 ? String(k8sClusters.value[0].id) : ''
      kubeconfigs.value = []
      issuedYaml.value = ''
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
    ElMessage.success('已签发短期 RBAC kubeconfig（零 system:masters 权限）')
    await loadKubeconfigs()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function copyYaml() {
  if (!issuedYaml.value) return
  try {
    await navigator.clipboard.writeText(issuedYaml.value)
    ElMessage.success('Kubeconfig YAML 已复制到剪贴板')
  } catch {
    ElMessage.warning('浏览器未授权剪贴板访问，请点击右侧「下载文件」')
  }
}

function downloadYaml() {
  if (!issuedYaml.value) return
  const name = String(selectedCluster.value?.name || 'cluster')
  const blob = new Blob([issuedYaml.value], { type: 'application/yaml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${name}-${reqForm.k8sRole}-kubeconfig.yaml`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
  ElMessage.success(`已下载 ${name}-${reqForm.k8sRole}-kubeconfig.yaml`)
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

watch(() => ws.currentProjectId, () => {
  selectedClusterId.value = ''
  kubeconfigs.value = []
  issuedYaml.value = ''
  void loadClusters()
})
onMounted(loadClusters)
</script>

<template>
  <div>
    <PageHeader
      title="多集群管理"
      subtitle="按最小权限原则签发与吊销短期 RBAC Kubeconfig（默认 8h，cluster-admin 封顶 4h，严禁 system:masters）"
      :refresh="loadClusters"
      :refreshing="loading"
    >
      <template #actions>
        <el-button type="primary" @click="router.push('/instances?tab=catalog&q=kubernetes')">
          <Plus :size="14" class="btn-ico" /> 部署 Kubernetes 集群
        </el-button>
      </template>
    </PageHeader>

    <KiBanner
      v-if="k8sClusters.length === 0 && !loading"
      variant="info"
      text="当前项目尚未部署 Kubernetes 集群实例（本页面自动过滤非 K8s 组件）。请点击右上角「部署 Kubernetes 集群」创建 K3s 或 kubeadm 集群后再签发凭据。"
      class="ki-mb-4"
    />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>短期 RBAC Kubeconfig 签发<span class="sub">可选 Kubernetes 集群：{{ k8sClusters.length }} 套</span></span>
      </div>
      <div class="filter-bar">
        <el-select
          v-model="selectedClusterId"
          :placeholder="k8sClusters.length ? '选择 Kubernetes 集群' : '暂无可用 Kubernetes 集群'"
          :disabled="k8sClusters.length === 0"
          class="sel-cluster"
          @change="loadKubeconfigs"
        >
          <el-option v-for="c in k8sClusters" :key="String(c.id)" :label="`${c.name} (${c.productVersion})`" :value="String(c.id)" />
        </el-select>
        <el-select v-model="reqForm.k8sRole" class="sel-role">
          <el-option label="view · 集群只读" value="view" />
          <el-option label="edit · 命名空间编辑" value="edit" />
          <el-option label="ns-admin · 命名空间管理员" value="ns-admin" />
          <el-option label="cluster-admin · 集群管理 (上限 4h)" value="cluster-admin" />
        </el-select>
        <div class="ttl-wrap">
          <span class="ki-muted">有效期 (h)</span>
          <el-input-number v-model="reqForm.ttlHours" :min="1" :max="reqForm.k8sRole === 'cluster-admin' ? 4 : 24" class="ttl-input" />
        </div>
        <el-button
          v-if="auth.can('cluster:kubeconfig', ws.currentProjectId)"
          type="primary"
          :disabled="!selectedClusterId"
          @click="issueKubeconfig"
        >
          <KeyRound :size="14" class="btn-ico" /> 签发短期 Kubeconfig
        </el-button>
      </div>

      <div v-if="issuedYaml" class="yaml-wrap">
        <div class="yaml-bar">
          <span class="ki-mono">kubeconfig.yaml</span>
          <div class="ki-flex-row">
            <el-button size="small" plain @click="copyYaml"><Copy :size="13" class="btn-ico" /> 复制</el-button>
            <el-button size="small" type="primary" plain @click="downloadYaml">
              <Download :size="13" class="btn-ico" /> 下载文件
            </el-button>
          </div>
        </div>
        <pre class="ki-mono yaml-out">{{ issuedYaml }}</pre>
      </div>
    </div>

    <div class="ki-card">
      <div class="ki-card__title">
        <span>已签发 Kubeconfig 凭据台账<span class="sub">共 {{ kubeconfigs.length }} 条</span></span>
      </div>
      <el-table v-if="kubeconfigs.length > 0 || loading" v-loading="loading" :data="kubeconfigs">
        <el-table-column label="凭据 ID" width="220">
          <template #default="{ row }"><KiMonoText :value="String(row.id || '')" /></template>
        </el-table-column>
        <el-table-column prop="k8sRole" label="RBAC 角色" width="150" />
        <el-table-column label="到期时间" min-width="180">
          <template #default="{ row }"><span class="ki-mono">{{ formatAbsTime(String(row.expiresAt || '')) }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="110">
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
      </el-table>
      <div v-else class="ki-empty-state">
        <div class="ki-empty-state__icon"><Network :size="20" /></div>
        <div class="ki-empty-state__title">尚未签发集群短期凭据</div>
        <p class="ki-empty-state__desc">部署或纳管 Kubernetes 集群实例后，可在此签发带自动过期的短期 RBAC kubeconfig 并随时吊销。</p>
      </div>
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
  width: 240px;
}

.ttl-wrap {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-xs);
}

.ttl-input {
  width: 112px;
}

.btn-ico {
  margin-right: 4px;
}

.yaml-wrap {
  margin-top: 12px;
  border-radius: var(--ki-radius-sm);
  overflow: hidden;
  border: 1px solid var(--ki-border-default);
}

.yaml-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background-color: var(--ki-bg-subtle);
  border-bottom: 1px solid var(--ki-border-subtle);
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.yaml-out {
  margin: 0;
  padding: 12px 14px;
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  max-height: 220px;
  overflow: auto;
}
</style>
