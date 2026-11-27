<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { FileText, RotateCcw, Terminal } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiOperationTerminal from '@/components/domain/KiOperationTerminal.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()
const route = useRoute()
const router = useRouter()
const { formatRelTime } = useKiFormat()

const loading = ref(false)
const operations = ref<Array<Record<string, unknown>>>([])
const selectedOpId = ref<string>(String(route.query.id || ''))
const stateFilter = ref<string>('ALL')

const filteredOps = computed(() => {
  if (stateFilter.value === 'ALL') return operations.value
  return operations.value.filter((o) => String(o.state || '') === stateFilter.value)
})

const selectedOp = computed(() => operations.value.find((o) => String(o.id) === selectedOpId.value) || null)

async function loadOps() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/operations`
    )
    operations.value = data.items || []
    if (!selectedOpId.value && operations.value.length > 0) {
      selectedOpId.value = String(operations.value[0].id)
    }
  } finally {
    loading.value = false
  }
}

async function resumeOp(opId: string) {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/operations/${opId}:resume`, { idempotent: true })
    ElMessage.success('操作已从断点恢复排队')
    await loadOps()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function downloadReport(opId: string) {
  const url = `/api/v1/projects/${ws.currentProjectId}/operations/${opId}/report?format=markdown`
  window.open(url, '_blank')
}

const rowClass = ({ row }: { row: Record<string, unknown> }) => {
  const s = String(row.state || '')
  if (s === 'failed') return 'ki-row-danger'
  if (s === 'awaiting_approval') return 'ki-row-warn'
  if (s === 'running') return 'ki-row-running'
  if (s === 'succeeded') return 'ki-row-ok'
  return ''
}

watch(() => ws.currentProjectId, loadOps)
onMounted(loadOps)
</script>

<template>
  <div>
    <PageHeader title="执行中心" :refresh="loadOps" :refreshing="loading" />

    <!-- 状态快速筛选条 -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': stateFilter === 'ALL' }" @click="stateFilter = 'ALL'">
        <span class="ki-chip__bar" />
        <div><div class="ki-chip__label">全部操作</div><div class="ki-chip__val ki-num">{{ operations.length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': stateFilter === 'running' }" @click="stateFilter = 'running'">
        <span class="ki-chip__bar ki-chip__bar--running" />
        <div><div class="ki-chip__label">执行中</div><div class="ki-chip__val ki-num">{{ operations.filter((o) => o.state === 'running').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': stateFilter === 'awaiting_approval' }" @click="stateFilter = 'awaiting_approval'">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div><div class="ki-chip__label">待审批</div><div class="ki-chip__val ki-num">{{ operations.filter((o) => o.state === 'awaiting_approval').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': stateFilter === 'succeeded' }" @click="stateFilter = 'succeeded'">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div><div class="ki-chip__label">已成功</div><div class="ki-chip__val ki-num">{{ operations.filter((o) => o.state === 'succeeded').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': stateFilter === 'failed' }" @click="stateFilter = 'failed'">
        <span class="ki-chip__bar ki-chip__bar--danger" />
        <div><div class="ki-chip__label">已失败</div><div class="ki-chip__val ki-num">{{ operations.filter((o) => o.state === 'failed').length }}</div></div>
      </div>
    </div>

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>操作流水与断点管理<span class="sub">点击任意记录挂载下方实时执行日志终端，支持失败任务断点续跑</span></span>
      </div>
      <el-table
        v-if="filteredOps.length > 0 || loading"
        v-loading="loading"
        :data="filteredOps"
        highlight-current-row
        :row-class-name="rowClass"
        @row-click="(r: Record<string, unknown>) => (selectedOpId = String(r.id))"
      >
        <el-table-column label="操作 ID" width="215">
          <template #default="{ row }"><KiMonoText :value="String(row.id || '')" :head="14" :tail="6" /></template>
        </el-table-column>
        <el-table-column prop="action" label="动作" width="104" />
        <el-table-column label="目标资源" min-width="200">
          <template #default="{ row }"><span class="ki-mono">{{ row.targetType }}/{{ row.targetId }}</span></template>
        </el-table-column>
        <el-table-column label="状态" width="136">
          <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'pending')" /></template>
        </el-table-column>
        <el-table-column label="租约序号" width="96">
          <template #default="{ row }"><span class="ki-mono ki-secondary">#{{ row.fencingToken ?? 0 }}</span></template>
        </el-table-column>
        <el-table-column label="更新时间" width="112">
          <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(String(row.updatedAt || row.createdAt || '')) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="156" fixed="right">
          <template #default="{ row }">
            <div class="ki-row-actions">
              <el-button link type="primary" @click.stop="downloadReport(String(row.id))">
                <FileText :size="13" class="btn-ico" /> 报告
              </el-button>
              <el-button
                v-if="row.state === 'failed' && auth.can('operation:resume', ws.currentProjectId)"
                link
                type="warning"
                @click.stop="resumeOp(String(row.id))"
              >
                <RotateCcw :size="13" class="btn-ico" /> 续跑
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
      <div v-else class="ki-empty-state">
        <div class="ki-empty-state__icon"><Terminal :size="20" /></div>
        <div class="ki-empty-state__title">暂无操作执行流水</div>
        <p class="ki-empty-state__desc">发起主机初始化、组件部署或应用栈 Apply 后，可在此实时查看每一步日志并导出交付报告。</p>
        <div class="ki-empty-state__actions">
          <el-button size="small" type="primary" @click="router.push('/instances?tab=catalog')">前往部署组件</el-button>
          <el-button size="small" @click="router.push('/stacks')">打开应用栈编排</el-button>
        </div>
      </div>
    </div>

    <KiOperationTerminal
      v-if="selectedOpId && ws.currentProjectId"
      :key="selectedOpId"
      :project-id="ws.currentProjectId"
      :op-id="selectedOpId"
      :can-resume="Boolean(selectedOp && selectedOp.state === 'failed' && auth.can('operation:resume', ws.currentProjectId))"
      :problem="selectedOp && selectedOp.state === 'failed' ? { code: String(selectedOp.errorCode || 'KI-EXEC-0001'), title: '操作执行异常中断', detail: String(selectedOp.errorMessage || '请查看左侧失败阶段日志并尝试断点续跑') } : null"
      @resume="resumeOp(selectedOpId)"
    />
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 3px;
}
</style>
