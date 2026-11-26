<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'

const ws = useWorkspaceStore()
const instances = ref<Array<Record<string, unknown>>>([])
const selectedClusterId = ref('')
const kubeconfigs = ref<Array<Record<string, unknown>>>([])
const issuedYaml = ref('')

const reqForm = reactive({ k8sRole: 'view', ttlHours: 8 })

async function loadClusters() {
  if (!ws.currentProjectId) return
  const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/instances`)
  instances.value = data.items || []
  if (instances.value.length > 0 && !selectedClusterId.value) {
    selectedClusterId.value = String(instances.value[0].id)
    await loadKubeconfigs()
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

async function revoke(kcId: string) {
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/clusters/kubeconfigs/${kcId}`)
  ElMessage.success('kubeconfig 已吊销并记入审计链')
  await loadKubeconfigs()
}

watch(() => ws.currentProjectId, loadClusters)
onMounted(loadClusters)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">多集群治理与短期 Kubeconfig 签发</h1>
        <div class="ki-muted" style="font-size: 12px">默认 8h / 上限 24h / cluster-admin 强封顶 4h 且使用可吊销 kinstall:cluster-admin（永不签发 system:masters）</div>
      </div>
    </div>

    <div class="ki-panel" style="margin-bottom: 16px">
      <div class="ki-flex-row" style="flex-wrap: wrap">
        <el-select v-model="selectedClusterId" placeholder="选择目标集群实例" style="width: 240px" @change="loadKubeconfigs">
          <el-option v-for="c in instances" :key="String(c.id)" :label="`${c.name} (${c.product})`" :value="String(c.id)" />
        </el-select>
        <el-select v-model="reqForm.k8sRole" style="width: 180px">
          <el-option label="view (只读)" value="view" />
          <el-option label="edit (命名空间编辑)" value="edit" />
          <el-option label="ns-admin" value="ns-admin" />
          <el-option label="cluster-admin (上限4h)" value="cluster-admin" />
        </el-select>
        <el-input-number v-model="reqForm.ttlHours" :min="1" :max="24" />
        <el-button type="primary" :disabled="!selectedClusterId" @click="issueKubeconfig">签发短期 Kubeconfig</el-button>
      </div>
      <pre v-if="issuedYaml" class="ki-mono" style="margin-top: 12px; padding: 12px; background: var(--ki-bg-terminal); color: var(--ki-term-fg); border-radius: 4px; max-height: 220px; overflow: auto">{{ issuedYaml }}</pre>
    </div>

    <div class="ki-panel">
      <el-table :data="kubeconfigs">
        <el-table-column prop="id" label="凭据 ID" width="200"><template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template></el-table-column>
        <el-table-column prop="k8sRole" label="RBAC 角色" width="140" />
        <el-table-column prop="expiresAt" label="到期时间" />
        <el-table-column prop="revokedAt" label="吊销状态" width="140">
          <template #default="{ row }">{{ row.revokedAt ? '已吊销' : '有效' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button size="small" type="danger" plain :disabled="Boolean(row.revokedAt)" @click="revoke(String(row.id))">吊销</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>
