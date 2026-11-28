<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Play, Radio, RefreshCw, Square, Workflow } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError, newIdempotencyKey, subscribeJobTimeline } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface DeployJob {
  job_id: string
  name: string
  state: string
  cancel_requested: boolean
  version: number
  last_seq: number
  created_by: string
  created_at: string
}

interface DeployTask {
  task_id: string
  task_key: string
  name: string
  task_type: string
  state: string
  remaining: number
  lease_epoch: number
  attempt_no: number
  owner: string
  action_key: string
  last_error?: string
}

interface TimelineEvent {
  seq: number
  event_type: string
  task_id?: string
  attempt_no?: number
  lease_epoch?: number
  message: string
  created_at: string
}

interface JobSnapshot {
  job: DeployJob
  tasks: DeployTask[]
  timeline?: TimelineEvent[]
  last_seq: number
}

const scope = useScopeStore()
const jobs = ref<DeployJob[]>([])
const activeSnap = ref<JobSnapshot | null>(null)
const timeline = ref<TimelineEvent[]>([])
const sseConnected = ref(false)
const loading = ref(false)
let stopSSE: (() => void) | null = null

const submitDlg = ref(false)
const submitForm = ref({
  name: 'HIS 菱形并发校验与发布作业',
  preset: 'diamond' as 'diamond' | 'branch_fail',
  idemKey: newIdempotencyKey('dag'),
})

function closeStream() {
  if (stopSSE) {
    stopSSE()
    stopSSE = null
  }
  sseConnected.value = false
}

async function loadJobs() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: DeployJob[] }>('/api/v1/deploy/jobs?limit=30')
    jobs.value = data.items || []
    if (jobs.value.length > 0) {
      const target = jobs.value.find((j) => j.job_id === activeSnap.value?.job.job_id) || jobs.value[0]
      await openJob(target.job_id)
    }
  } finally {
    loading.value = false
  }
}

async function openJob(jobId: string) {
  closeStream()
  const { data } = await api.get<JobSnapshot>(`/api/v1/deploy/jobs/${jobId}?timeline=true`)
  activeSnap.value = data
  timeline.value = data.timeline || []

  // Follow LiveOps U01-U09 strictly: subscribe with snapshot's last_seq!
  if (!['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(data.job.state)) {
    sseConnected.value = true
    stopSSE = subscribeJobTimeline(jobId, data.last_seq, {
      onEvent: (seq, _evType, evData) => {
        if (!timeline.value.some((t) => t.seq === seq)) {
          timeline.value.push(evData as unknown as TimelineEvent)
        }
        void refreshSnapshotQuiet(jobId)
      },
      onResetRequired: () => {
        void openJob(jobId)
      },
      onError: () => {
        sseConnected.value = false
      },
    })
  }
}

async function refreshSnapshotQuiet(jobId: string) {
  try {
    const { data } = await api.get<JobSnapshot>(`/api/v1/deploy/jobs/${jobId}?timeline=false`)
    activeSnap.value = data
    const idx = jobs.value.findIndex((j) => j.job_id === jobId)
    if (idx >= 0) jobs.value[idx] = data.job
    if (['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(data.job.state)) {
      closeStream()
    }
  } catch {
    // ignore transient poll error
  }
}

function openSubmitModal() {
  submitForm.value.idemKey = newIdempotencyKey('dag')
  submitDlg.value = true
}

async function submitJob() {
  const isDiamond = submitForm.value.preset === 'diamond'
  const payload = isDiamond
    ? {
        name: submitForm.value.name,
        projectId: scope.projectId || 'prod-clinical',
        explicitGraph: true,
        tasks: [
          { key: 'preflight', name: '配置与环境预检', taskType: 'CHECK', payload: { delayMs: 350 } },
          { key: 'zone-a', name: 'A 区灰度下发', taskType: 'SYNC', payload: { delayMs: 450 } },
          { key: 'zone-b', name: 'B 区灰度下发', taskType: 'SYNC', payload: { delayMs: 500 } },
          { key: 'verify-all', name: '全链路探活收敛', taskType: 'VERIFY', payload: { delayMs: 300 } },
        ],
        edges: [
          { from: 'preflight', to: 'zone-a' },
          { from: 'preflight', to: 'zone-b' },
          { from: 'zone-a', to: 'verify-all' },
          { from: 'zone-b', to: 'verify-all' },
        ],
      }
    : {
        name: submitForm.value.name + ' (含分支故障隔离)',
        projectId: scope.projectId || 'prod-clinical',
        explicitGraph: true,
        tasks: [
          { key: 'fault-root', name: '模拟异常节点', taskType: 'CHECK', maxRetries: 1, payload: { delayMs: 250, simulateFail: true } },
          { key: 'downstream-pruned', name: '下游剪枝节点 (应 SKIPPED)', taskType: 'SYNC', payload: { delayMs: 200 } },
          { key: 'independent-ok', name: '独立健康分支 (应 SUCCEEDED)', taskType: 'VERIFY', payload: { delayMs: 400 } },
        ],
        edges: [{ from: 'fault-root', to: 'downstream-pruned' }],
      }

  try {
    const res = await api.post<JobSnapshot>('/api/v1/deploy/jobs', payload, {
      idempotencyKey: submitForm.value.idemKey,
    })
    ElMessage.success(res.replayed ? '命中 Idempotency-Key 幂等重放 (D01)' : 'DAG 部署作业已原子入库，Worker 正在调度执行')
    submitDlg.value = false
    await loadJobs()
    await openJob(res.data.job.job_id)
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

async function cancelActiveJob() {
  if (!activeSnap.value) return
  try {
    await api.post(
      `/api/v1/deploy/jobs/${activeSnap.value.job.job_id}/cancel`,
      { reason: 'operator-requested' },
      { ifMatch: activeSnap.value.job.version, idempotencyKey: newIdempotencyKey('cancel') }
    )
    ElMessage.warning('已发送取消指令（If-Match CAS），未解锁后继任务将转为 CANCELLED')
    await openJob(activeSnap.value.job.job_id)
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

function statePill(state: string) {
  if (state === 'SUCCEEDED') return 'ks-pill-ok'
  if (state === 'FAILED') return 'ks-pill-danger'
  if (state === 'CANCELLED' || state === 'SKIPPED') return 'ks-pill-warn'
  return 'ks-pill-blue'
}

onMounted(loadJobs)
onBeforeUnmount(closeStream)
watch(() => [scope.tenantId, scope.appId], loadJobs)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="DAG 部署编排与严格 SSE 时间线 (D01–D19 · U01–U09)"
      description="支持显式有向无环图提交、Idempotency-Key 强幂等、独立 liveops-worker 租约调度 (lease_epoch / attempt_no) 与 Last-Event-ID 实时事件流。"
      :icon="Workflow"
      :badge="sseConnected ? 'SSE 实时流已连接' : `${jobs.length} 条作业`"
    >
      <template #actions>
        <el-button :loading="loading" @click="loadJobs"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" data-testid="submit-dag-btn" @click="openSubmitModal">
          <Play :size="14" /><span class="ks-gap">提交 DAG 部署作业</span>
        </el-button>
      </template>
    </KsBanner>

    <div class="ks-split">
      <!-- Left: Jobs List -->
      <div class="ki-card">
        <div class="ki-card__title"><span>部署作业历史</span></div>
        <div class="ks-job-list">
          <button
            v-for="j in jobs"
            :key="j.job_id"
            type="button"
            class="ks-job-item"
            :class="{ 'is-active': activeSnap?.job.job_id === j.job_id }"
            @click="openJob(j.job_id)"
          >
            <div class="ks-job-item__top">
              <span class="ks-job-item__name">{{ j.name }}</span>
              <span class="ki-badge" :class="statePill(j.state)">{{ j.state }}</span>
            </div>
            <div class="ks-job-item__meta ki-mono">{{ j.job_id }} · seq #{{ j.last_seq }}</div>
          </button>
        </div>
      </div>

      <!-- Right: Snapshot Tasks + Strict SSE Timeline -->
      <div v-if="activeSnap" class="ks-detail-col">
        <div class="ki-card">
          <div class="ki-card__title">
            <span>{{ activeSnap.job.name }}</span>
            <div class="ks-head-actions">
              <span class="ki-badge ks-pill-blue ki-mono">ETag: "{{ activeSnap.job.version }}" · lastSeq #{{ activeSnap.last_seq }}</span>
              <el-button
                v-if="!['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(activeSnap.job.state)"
                size="small"
                type="danger"
                plain
                @click="cancelActiveJob"
              >
                <Square :size="12" /><span class="ks-gap-xs">取消作业</span>
              </el-button>
            </div>
          </div>

          <el-table :data="activeSnap.tasks" size="small" stripe>
            <el-table-column prop="task_key" label="Task Key" width="130"><template #default="{ row }"><span class="ki-mono">{{ row.task_key }}</span></template></el-table-column>
            <el-table-column prop="name" label="任务名称" min-width="150" />
            <el-table-column prop="state" label="状态" width="120">
              <template #default="{ row }"><span class="ki-badge" :class="statePill(row.state)">{{ row.state }}</span></template>
            </el-table-column>
            <el-table-column prop="remaining" label="剩余依赖" width="80" />
            <el-table-column label="租约/尝试" width="120">
              <template #default="{ row }"><span class="ki-mono">e{{ row.lease_epoch }} / a{{ row.attempt_no }}</span></template>
            </el-table-column>
            <el-table-column prop="owner" label="执行 Worker" width="140"><template #default="{ row }"><span class="ki-mono">{{ row.owner || '-' }}</span></template></el-table-column>
          </el-table>
        </div>

        <div class="ki-card">
          <div class="ki-card__title">
            <span><Radio :size="14" class="ks-inline-icon" />单调无空洞事件时间线 (SSE Last-Event-ID)</span>
            <span class="ki-mono ki-muted">{{ timeline.length }} 条事件</span>
          </div>
          <div class="ks-timeline">
            <div v-for="ev in timeline" :key="ev.seq" class="ks-tl-row">
              <span class="ks-tl-seq ki-mono">#{{ ev.seq }}</span>
              <span class="ki-badge ks-pill-neutral ki-mono">{{ ev.event_type }}</span>
              <span class="ks-tl-msg">{{ ev.message }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <el-dialog v-model="submitDlg" title="提交显式 DAG 部署作业" width="520px">
      <el-form label-position="top">
        <el-form-item label="作业名称" required><el-input v-model="submitForm.name" /></el-form-item>
        <el-form-item label="Idempotency-Key (防重提交 / 409 冲突保护)" required>
          <el-input v-model="submitForm.idemKey" class="ki-mono" />
        </el-form-item>
        <el-form-item label="DAG 编排拓扑预设">
          <el-radio-group v-model="submitForm.preset">
            <el-radio value="diamond">Diamond 菱形并发图 (preflight → zone-a, zone-b → verify-all)</el-radio>
            <el-radio value="branch_fail">故障剪枝与独立分支 (验证 D13 重试 + D18 SKIPPED 剪枝)</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="submitDlg = false">取消</el-button>
        <el-button type="primary" data-testid="confirm-submit-dag" @click="submitJob">提交作业</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-gap-xs {
  margin-left: 4px;
}

.ks-inline-icon {
  vertical-align: -2px;
  margin-right: 6px;
  color: var(--ki-brand);
}

.ks-split {
  display: grid;
  grid-template-columns: 310px minmax(0, 1fr);
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-split {
    grid-template-columns: 1fr;
  }
}

.ks-detail-col {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-4);
  min-width: 0;
}

.ks-job-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-job-item {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
}

.ks-job-item.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-sider-active-bg);
}

.ks-job-item__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ks-job-item__name {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
}

.ks-job-item__meta {
  margin-top: 4px;
  font-size: 11px;
  color: var(--ki-text-muted);
}

.ks-head-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ks-timeline {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 280px;
  overflow-y: auto;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.ks-tl-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--ki-fs-xs);
}

.ks-tl-seq {
  width: 34px;
  color: var(--ki-text-muted);
  flex: none;
}

.ks-tl-msg {
  color: var(--ki-text-primary);
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-warn {
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
}

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ks-pill-neutral {
  background-color: var(--ki-status-neutral-subtle);
  border: 1px solid var(--ki-status-neutral-border);
  color: var(--ki-status-neutral-text);
}
</style>
