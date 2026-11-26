<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Server, Box, ClipboardCheck, ShieldCheck } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiStatusBadge from '@/components/KiStatusBadge.vue'
import KiTargetBadge from '@/components/KiTargetBadge.vue'

const ws = useWorkspaceStore()
const router = useRouter()

const hosts = ref<Array<Record<string, unknown>>>([])
const instances = ref<Array<Record<string, unknown>>>([])
const operations = ref<Array<Record<string, unknown>>>([])
const plugins = ref<Array<Record<string, unknown>>>([])
const auditValid = ref<boolean | null>(null)

async function loadData() {
  if (!ws.currentProjectId) return
  const pid = ws.currentProjectId
  const [h, i, o, p] = await Promise.all([
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/hosts`).catch(() => ({ data: { items: [] } })),
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/instances`).catch(() => ({ data: { items: [] } })),
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${pid}/operations`).catch(() => ({ data: { items: [] } })),
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', '/api/v1/plugins').catch(() => ({ data: { items: [] } })),
  ])
  hosts.value = h.data.items || []
  instances.value = i.data.items || []
  operations.value = o.data.items || []
  plugins.value = p.data.items || []
  await ws.refreshApprovalBadge()
}

async function quickVerifyAudit() {
  try {
    const { data } = await apiRequest<{ valid: boolean }>('POST', '/api/v1/audit:verify')
    auditValid.value = data.valid
  } catch {
    auditValid.value = false
  }
}

watch(() => ws.currentProjectId, loadData)
onMounted(loadData)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">基础设施全局驾驶舱</h1>
        <div class="ki-muted" style="font-size: 12px">实时纳管拓扑、三元部署分布、四眼审批与审计链健康状态</div>
      </div>
      <div class="ki-flex-row">
        <el-button @click="quickVerifyAudit">校验审计链</el-button>
        <el-button type="primary" @click="router.push('/instances')">部署新实例</el-button>
      </div>
    </div>

    <div class="ki-grid-4" style="margin-bottom: 16px">
      <div class="ki-panel">
        <div class="ki-flex-between">
          <span class="ki-secondary">CMDB 纳管主机</span>
          <Server :size="16" style="color: var(--ki-brand)" />
        </div>
        <div class="ki-num" style="font-size: var(--ki-fs-kpi); font-weight: 600; margin-top: 8px">{{ hosts.length }}</div>
        <div class="ki-muted ki-mono" style="font-size: 11px; margin-top: 4px">
          64K页: {{ hosts.filter((x) => Number(x.pageSize) >= 65536).length }} 台
        </div>
      </div>

      <div class="ki-panel">
        <div class="ki-flex-between">
          <span class="ki-secondary">运行中实例</span>
          <Box :size="16" style="color: var(--ki-status-ok)" />
        </div>
        <div class="ki-num" style="font-size: var(--ki-fs-kpi); font-weight: 600; margin-top: 8px">{{ instances.length }}</div>
        <div class="ki-muted ki-mono" style="font-size: 11px; margin-top: 4px">内置插件包: {{ plugins.length }} 套</div>
      </div>

      <div class="ki-panel" style="cursor: pointer" @click="router.push('/operations/approvals')">
        <div class="ki-flex-between">
          <span class="ki-secondary">待四眼审批</span>
          <ClipboardCheck :size="16" style="color: var(--ki-status-warn)" />
        </div>
        <div class="ki-num" style="font-size: var(--ki-fs-kpi); font-weight: 600; margin-top: 8px; color: var(--ki-status-warn-text)">
          {{ ws.pendingApprovalCount }}
        </div>
        <div class="ki-muted" style="font-size: 11px; margin-top: 4px">点击进入审批工作台 →</div>
      </div>

      <div class="ki-panel">
        <div class="ki-flex-between">
          <span class="ki-secondary">审计哈希链</span>
          <ShieldCheck :size="16" style="color: var(--ki-status-ok)" />
        </div>
        <div style="margin-top: 12px">
          <KiStatusBadge :status="auditValid === null ? 'ready' : auditValid ? 'valid' : 'error'" />
        </div>
        <div class="ki-muted" style="font-size: 11px; margin-top: 8px">SHA-256 + SM3 双链防篡改</div>
      </div>
    </div>

    <div class="ki-grid-2">
      <div class="ki-panel">
        <h2 style="font-size: var(--ki-fs-section); margin-bottom: 12px">近期操作流水</h2>
        <el-table :data="operations.slice(0, 8)" size="small">
          <el-table-column prop="id" label="操作 ID" width="180">
            <template #default="{ row }">
              <a class="ki-mono" href="javascript:void(0)" @click="router.push(`/operations?id=${row.id}`)">{{ row.id }}</a>
            </template>
          </el-table-column>
          <el-table-column prop="action" label="动作" width="90" />
          <el-table-column prop="state" label="状态">
            <template #default="{ row }">
              <KiStatusBadge :status="String(row.state)" />
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="ki-panel">
        <h2 style="font-size: var(--ki-fs-section); margin-bottom: 12px">实例与部署形态概览</h2>
        <el-table :data="instances.slice(0, 8)" size="small">
          <el-table-column prop="name" label="实例名" />
          <el-table-column prop="product" label="组件" width="120" />
          <el-table-column prop="deployTarget" label="形态" width="140">
            <template #default="{ row }">
              <KiTargetBadge :target="String(row.deployTarget)" />
            </template>
          </el-table-column>
          <el-table-column prop="productVersion" label="版本" width="90">
            <template #default="{ row }"><span class="ki-mono">{{ row.productVersion }}</span></template>
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>
</template>
