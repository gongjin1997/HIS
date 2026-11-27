<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Save } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiEnvBadge from '@/components/ki/KiEnvBadge.vue'
import StackCanvas, { type StackNodeInput } from '@/components/StackCanvas.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()

const loading = ref(false)
const stacks = ref<Array<Record<string, unknown>>>([])
const stackName = ref('prod-t3-logging-platform')
const specYaml = ref(`apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: prod-t3-logging-platform
spec:
  instances:
    - id: s3
      product: object-storage
      productVersion: "4.54.0"
      deployTarget: host-binary
    - id: kafka
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
    - id: opensearch
      product: opensearch
      productVersion: "3.10.0"
      deployTarget: host-binary
      dependsOn: [s3]
    - id: dataprepper
      product: elk-pipeline
      productVersion: "2.16.0"
      deployTarget: host-binary
      dependsOn: [kafka, opensearch, s3]
`)

const canvasNodes = ref<StackNodeInput[]>([
  { id: 's3', product: 'object-storage', version: '4.54.0', target: 'host-binary', dependsOn: [], status: 'succeeded', outputs: [{ name: 's3Endpoint' }, { name: 'secretKey', secret: true }] },
  { id: 'kafka', product: 'kafka', version: '4.3.2', target: 'host-binary', dependsOn: [], status: 'succeeded', outputs: [{ name: 'bootstrapServers' }] },
  { id: 'opensearch', product: 'opensearch', version: '3.10.0', target: 'host-binary', dependsOn: ['s3'], status: 'running', outputs: [{ name: 'httpsUrl' }, { name: 'adminPass', secret: true }] },
  { id: 'dataprepper', product: 'elk-pipeline', version: '2.16.0', target: 'host-binary', dependsOn: ['kafka', 'opensearch', 's3'], status: 'pending', outputs: [{ name: 'ingestUrl' }] },
])

async function loadStacks() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/stacks`)
    stacks.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function createStack() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/stacks`, {
      body: { env: ws.currentEnv, name: stackName.value, specYaml: specYaml.value },
      idempotent: true,
    })
    ElMessage.success('Stack DAG 拓扑排序与 OutputSchema 静态校验通过并已保存')
    await loadStacks()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadStacks)
onMounted(loadStacks)
</script>

<template>
  <div>
    <PageHeader title="Stack DAG 编排" :refresh="loadStacks" :refreshing="loading" />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>复合栈可视化 DAG 画布<span class="sub">Kahn 拓扑排序 + 环路检测 (KI-STACK-0001) + 加密 OutputSchema 句柄</span></span>
        <el-button v-if="auth.can('stack:write', ws.currentProjectId)" type="primary" @click="createStack">
          <Save :size="14" class="btn-ico" /> 校验并保存 Stack
        </el-button>
      </div>
      <StackCanvas :instances="canvasNodes" />
    </div>

    <div class="ki-grid-2">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>声明式 Stack YAML</span>
          <el-input v-model="stackName" size="small" class="ki-mono stack-name-input" />
        </div>
        <el-input v-model="specYaml" type="textarea" :rows="14" class="ki-mono" />
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>已注册 Stack 清单<span class="sub">共 {{ stacks.length }} 个</span></span>
        </div>
        <el-table v-loading="loading" :data="stacks" size="small">
          <el-table-column prop="name" label="名称" min-width="180">
            <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
          </el-table-column>
          <el-table-column label="环境" width="120">
            <template #default="{ row }"><KiEnvBadge :env="String(row.env || ws.currentEnv)" /></template>
          </el-table-column>
          <el-table-column label="状态" width="120">
            <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'ready')" /></template>
          </el-table-column>
          <template #empty><el-empty description="暂无已保存 Stack，点击上方按钮保存当前模板" :image-size="60" /></template>
        </el-table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.stack-name-input {
  width: 220px;
}
</style>
