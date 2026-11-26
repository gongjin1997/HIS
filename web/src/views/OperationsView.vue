<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiStatusBadge from '@/components/KiStatusBadge.vue'
import OperationTerminal from '@/components/OperationTerminal.vue'

const ws = useWorkspaceStore()
const route = useRoute()

const operations = ref<Array<Record<string, unknown>>>([])
const selectedOpId = ref<string>(String(route.query.id || ''))

async function loadOps() {
  if (!ws.currentProjectId) return
  const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/operations`
  )
  operations.value = data.items || []
  if (!selectedOpId.value && operations.value.length > 0) {
    selectedOpId.value = String(operations.value[0].id)
  }
}

async function resumeOp(opId: string) {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/operations/${opId}:resume`, { idempotent: true })
    ElMessage.success('操作已从断点恢复排队 (--resume)')
    await loadOps()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadOps)
onMounted(loadOps)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">操作执行中心与 SSE 实时终端</h1>
        <div class="ki-muted" style="font-size: 12px">支持 Last-Event-ID 断网零丢帧续传、多副本无缝重连与 --resume 断点续装</div>
      </div>
      <el-button @click="loadOps">刷新列表</el-button>
    </div>

    <div class="ki-panel" style="margin-bottom: 16px">
      <el-table :data="operations" highlight-current-row @row-click="(r: Record<string, unknown>) => (selectedOpId = String(r.id))">
        <el-table-column prop="id" label="操作 ID" width="210">
          <template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column prop="action" label="动作" width="110" />
        <el-table-column prop="targetId" label="目标资源" width="200">
          <template #default="{ row }"><span class="ki-mono">{{ row.targetType }}/{{ row.targetId }}</span></template>
        </el-table-column>
        <el-table-column prop="state" label="状态" width="160">
          <template #default="{ row }"><KiStatusBadge :status="String(row.state)" /></template>
        </el-table-column>
        <el-table-column prop="fencingToken" label="Fencing" width="90">
          <template #default="{ row }"><span class="ki-mono">#{{ row.fencingToken }}</span></template>
        </el-table-column>
        <el-table-column label="断点操作" width="120">
          <template #default="{ row }">
            <el-button v-if="row.state === 'failed'" size="small" type="warning" @click.stop="resumeOp(String(row.id))">
              --resume
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <OperationTerminal
      v-if="selectedOpId && ws.currentProjectId"
      :key="selectedOpId"
      :project-id="ws.currentProjectId"
      :op-id="selectedOpId"
    />
  </div>
</template>
