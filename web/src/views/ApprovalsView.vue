<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Ban, Check, ExternalLink, ShieldAlert, Terminal, Users, X } from 'lucide-vue-next'
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
  requestedByName?: string
  targetType?: string
  targetName?: string
  action?: string
  reason: string
  status: string
  expiresAt: string
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()
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

async function withdrawSelfRequest(opId: string) {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/operations/${opId}:cancel`, {
      body: { reason: noteMap.value[opId] || '发起人主动撤回' },
    })
    ElMessage.success('已撤回变更请求并释放预占资源')
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
    <PageHeader
      title="变更审批"
      subtitle="生产环境与高危操作强制执行四眼双人复核，系统在界面、服务与数据库三层禁止发起人自审"
      :refresh="loadApprovals"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="router.push('/operations')">
          <Terminal :size="14" class="btn-ico" /> 返回执行中心
        </el-button>
      </template>
    </PageHeader>

    <KiBanner
      variant="info"
      text="生产环境及高危变更强制执行双人复核（四眼原则）；发起人可在此主动「撤回」自己的待审申请，或由另一位审批员复核批准。"
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
        <div class="ki-flex-row ki-flex-wrap">
          <KiEnvBadge :env="ap.envName" compact />
          <b class="ap-target">{{ ap.targetName || ap.operationId }}</b>
          <span v-if="ap.action" class="ki-badge ki-mono action-chip">{{ ap.action }}</span>
          <KiStatusBadge :status="ap.status" />
        </div>
        <div class="ki-flex-row">
          <span class="ki-muted ap-meta">
            发起人：<b>{{ ap.requestedByName || ap.requestedBy }}</b> · 截止：{{ formatAbsTime(ap.expiresAt) }}
          </span>
          <el-button link type="primary" size="small" @click="router.push(`/operations?id=${ap.operationId}`)">
            <ExternalLink :size="13" class="btn-ico" /> 查看流水
          </el-button>
        </div>
      </div>

      <div class="ap-reason-row ki-mb-3">
        <span>{{ ap.reason }}</span>
        <KiMonoText :value="ap.operationId" :head="14" :tail="6" />
      </div>

      <div v-if="auth.user && ap.requestedBy === auth.user.id" class="self-guard ki-mb-3">
        <ShieldAlert :size="15" aria-hidden="true" />
        <span>当前登录账号为变更发起人，禁止自审。您可由另一位审批员账号复核，或直接点击右侧「撤回申请」取消。</span>
        <el-button link type="primary" size="small" class="guard-link" @click="router.push('/settings')">
          <Users :size="13" class="btn-ico" /> 管理审批员
        </el-button>
      </div>

      <div class="ki-flex-row ki-flex-wrap">
        <el-input v-model="noteMap[ap.operationId]" placeholder="填写审批复核或撤回意见（可选）" class="ap-note" />
        <el-tooltip
          :disabled="!(auth.user && ap.requestedBy === auth.user.id)"
          content="遵循双人复核原则，发起人不可批准自己的请求"
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
          v-if="auth.user && ap.requestedBy === auth.user.id"
          type="warning"
          plain
          @click="withdrawSelfRequest(ap.operationId)"
        >
          <Ban :size="14" class="btn-ico" /> 撤回申请
        </el-button>
        <el-button
          v-else-if="auth.can('operation:approve', ws.currentProjectId)"
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

.ap-target {
  font-size: var(--ki-fs-section);
  color: var(--ki-text-primary);
}

.action-chip {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.ap-meta {
  font-size: var(--ki-fs-xs);
}

.ap-reason-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sm);
}

.self-guard {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
  font-size: var(--ki-fs-xs);
}

.guard-link {
  margin-left: auto;
}

.ap-note {
  max-width: 380px;
}

.btn-ico {
  margin-right: 4px;
}
</style>
