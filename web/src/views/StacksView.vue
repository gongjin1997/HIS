<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import StackCanvas, { type StackNodeInput } from '@/components/StackCanvas.vue'

const ws = useWorkspaceStore()
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
  { id: 's3', product: 'object-storage', version: '4.54.0', target: 'host-binary', dependsOn: [], status: 'succeeded' },
  { id: 'kafka', product: 'kafka', version: '4.3.2', target: 'host-binary', dependsOn: [], status: 'succeeded' },
  { id: 'opensearch', product: 'opensearch', version: '3.10.0', target: 'host-binary', dependsOn: ['s3'], status: 'running' },
  { id: 'dataprepper', product: 'elk-pipeline', version: '2.16.0', target: 'host-binary', dependsOn: ['kafka', 'opensearch', 's3'], status: 'pending' },
])

async function loadStacks() {
  if (!ws.currentProjectId) return
  const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/stacks`)
  stacks.value = data.items || []
}

async function createStack() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/stacks`, {
      body: { env: ws.currentEnv, name: stackName.value, specYaml: specYaml.value },
      idempotent: true,
    })
    ElMessage.success('Stack DAG 静态拓扑与 OutputSchema 校验通过并已保存')
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
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">Stack 复合栈可视化 DAG 画布 (Vue Flow 2.0)</h1>
        <div class="ki-muted" style="font-size: 12px">Kahn 拓扑排序 + 环路检测 (KI-STACK-0001) + 跨组件加密 OutputSchema 连线</div>
      </div>
      <el-button type="primary" @click="createStack">校验并保存 Stack</el-button>
    </div>

    <StackCanvas :instances="canvasNodes" style="margin-bottom: 16px" />

    <div class="ki-grid-2">
      <div class="ki-panel">
        <div class="ki-flex-between" style="margin-bottom: 10px">
          <span style="font-weight: 600">声明式 Stack YAML（P3 ELK+Kafka+S3 样板）</span>
          <el-input v-model="stackName" style="width: 220px" class="ki-mono" />
        </div>
        <el-input v-model="specYaml" type="textarea" :rows="14" class="ki-mono" />
      </div>

      <div class="ki-panel">
        <div style="font-weight: 600; margin-bottom: 10px">已注册 Stack 列表</div>
        <el-table :data="stacks" size="small">
          <el-table-column prop="name" label="名称" />
          <el-table-column prop="env" label="环境" width="90" />
          <el-table-column prop="state" label="状态" width="100" />
        </el-table>
      </div>
    </div>
  </div>
</template>
