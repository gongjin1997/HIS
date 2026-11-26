<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ShieldAlert, Check, X } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import KiEnvBadge from '@/components/KiEnvBadge.vue'
import KiStatusBadge from '@/components/KiStatusBadge.vue'

interface ApprovalItem {
  operationId: string
  envName: string
  requestedBy: string
  reason: string
  status: string
  expiresAt: string
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const approvals = ref<ApprovalItem[]>([])
const noteMap = ref<Record<string, string>>({})

async function loadApprovals() {
  if (!ws.currentProjectId) return
  const { data } = await apiRequest<{ items: ApprovalItem[] }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/approvals`
  )
  approvals.value = data.items || []
  await ws.refreshApprovalBadge()
}

async function decide(opId: string, approve: boolean) {
  const action = approve ? ':approve' : ':reject'
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/operations/${opId}${action}`, {
      body: { note: noteMap.value[opId] || (approve ? 'Approved via Four-Eyes Console' : 'Rejected') },
      idempotent: true,
    })
    ElMessage.success(approve ? '审批已通过，操作已进入执行队列' : '审批已驳回')
    await loadApprovals()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadApprovals)
onMounted(loadApprovals)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">四眼审批工作台 (Four-Eyes Approval)</h1>
        <div class="ki-muted" style="font-size: 12px">高危/生产变更强制双人复核，前端 + 服务层 + 数据库 chk_no_self_approval 三重防自审 (ADR-037)</div>
      </div>
      <el-button @click="loadApprovals">刷新待审队列</el-button>
    </div>

    <div v-if="approvals.length === 0" class="ki-panel" style="text-align: center; padding: 48px">
      <div class="ki-secondary">当前项目暂无待审批变更</div>
    </div>

    <div v-for="ap in approvals" :key="ap.operationId" class="ki-panel" style="margin-bottom: 12px">
      <div class="ki-flex-between" style="margin-bottom: 10px">
        <div class="ki-flex-row">
          <KiEnvBadge :env="ap.envName" />
          <span class="ki-mono" style="font-weight: 600">{{ ap.operationId }}</span>
          <KiStatusBadge :status="ap.status" />
        </div>
        <span class="ki-muted ki-mono" style="font-size: 12px">发起人: {{ ap.requestedBy }}</span>
      </div>

      <div style="margin-bottom: 12px">{{ ap.reason }}</div>

      <div
        v-if="auth.user && ap.requestedBy === auth.user.id"
        class="ki-flex-row"
        style="padding: 8px 12px; margin-bottom: 10px; border-radius: 4px; background: var(--ki-status-warn-subtle); color: var(--ki-status-warn-text); font-size: 12px"
      >
        <ShieldAlert :size="14" />
        <span>遵循四眼原则 (ADR-037)：当前登录用户为发起人，物理禁止审批自己的高危变更，请由另一位具备审批权限的成员复核。</span>
      </div>

      <div class="ki-flex-row">
        <el-input v-model="noteMap[ap.operationId]" placeholder="审批意见（可选）" style="max-width: 360px" />
        <el-tooltip
          :disabled="!(auth.user && ap.requestedBy === auth.user.id)"
          content="遵循四眼原则（ADR-037），发起人不可审批自己的请求"
        >
          <span>
            <el-button
              type="primary"
              :disabled="Boolean(auth.user && ap.requestedBy === auth.user.id)"
              @click="decide(ap.operationId, true)"
            >
              <Check :size="14" style="margin-right: 4px" /> 批准执行
            </el-button>
          </span>
        </el-tooltip>
        <el-button type="danger" plain @click="decide(ap.operationId, false)">
          <X :size="14" style="margin-right: 4px" /> 驳回
        </el-button>
      </div>
    </div>
  </div>
</template>
