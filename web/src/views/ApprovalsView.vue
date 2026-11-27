<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ShieldAlert, Check, X } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiEnvBadge from '@/components/ki/KiEnvBadge.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiEmptyState from '@/components/ki/KiEmptyState.vue'
import KiBanner from '@/components/ki/KiBanner.vue'

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
const { formatAbsTime } = useKiFormat()

const loading = ref(false)
const approvals = ref<ApprovalItem[]>([])
const noteMap = ref<Record<string, string>>({})

async function loadApprovals() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: ApprovalItem[] }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/approvals`
    )
    approvals.value = data.items || []
    await ws.refreshApprovalBadge()
  } finally {
    loading.value = false
  }
}

async function decide(opId: string, approve: boolean) {
  const action = approve ? ':approve' : ':reject'
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/operations/${opId}${action}`, {
      body: { note: noteMap.value[opId] || (approve ? 'Approved via Change Approval Console' : 'Rejected') },
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
  <div v-loading="loading">
    <PageHeader title="变更审批" :refresh="loadApprovals" :refreshing="loading" />

    <KiBanner
      variant="info"
      text="生产环境及高危变更强制执行双人复核（四眼原则），系统在界面、服务层与数据库层面三重禁止发起人自审。"
      class="ki-mb-4"
    />

    <div v-if="approvals.length === 0" class="ki-card">
      <KiEmptyState
        variant="first-run"
        title="当前项目暂无待审批变更"
        description="当生产环境实例提交 Apply、逆序卸载或强行覆盖配置漂移时，待复核工单将自动进入此列表。"
      />
    </div>

    <div v-for="ap in approvals" :key="ap.operationId" class="ki-card ki-mb-3 approval-card">
      <div class="ki-card__title">
        <div class="ki-flex-row">
          <KiEnvBadge :env="ap.envName" />
          <KiMonoText :value="ap.operationId" :head="14" :tail="6" />
          <KiStatusBadge :status="ap.status" />
        </div>
        <span class="ki-muted ki-mono ap-meta">
          发起人：{{ ap.requestedBy }} · 过期：{{ formatAbsTime(ap.expiresAt) }}
        </span>
      </div>

      <p class="ap-reason">{{ ap.reason }}</p>

      <div v-if="auth.user && ap.requestedBy === auth.user.id" class="self-guard ki-mb-3">
        <ShieldAlert :size="15" aria-hidden="true" />
        <span>当前登录账号为变更发起人，依据双人复核安全策略禁止自审，请由另一位具备审批权限的成员复核。</span>
      </div>

      <div class="ki-flex-row ki-flex-wrap">
        <el-input v-model="noteMap[ap.operationId]" placeholder="填写审批复核意见（可选）" class="ap-note" />
        <el-tooltip
          :disabled="!(auth.user && ap.requestedBy === auth.user.id)"
          content="遵循双人复核原则，发起人不可审批自己的请求"
        >
          <span>
            <el-button
              v-if="auth.can('operation:approve', ws.currentProjectId)"
              type="primary"
              :disabled="Boolean(auth.user && ap.requestedBy === auth.user.id)"
              @click="decide(ap.operationId, true)"
            >
              <Check :size="14" class="btn-ico" /> 批准执行
            </el-button>
          </span>
        </el-tooltip>
        <el-button
          v-if="auth.can('operation:approve', ws.currentProjectId)"
          type="danger"
          plain
          @click="decide(ap.operationId, false)"
        >
          <X :size="14" class="btn-ico" /> 驳回
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-card {
  border-left: 4px solid var(--ki-status-warn);
}

.ap-meta {
  font-size: var(--ki-fs-xs);
}

.ap-reason {
  margin: 0 0 12px;
  color: var(--ki-text-primary);
}

.self-guard {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
  font-size: var(--ki-fs-xs);
}

.ap-note {
  max-width: 360px;
}

.btn-ico {
  margin-right: 4px;
}
</style>
