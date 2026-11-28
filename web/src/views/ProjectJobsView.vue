<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  CheckCircle2,
  Clock,
  Play,
  Plus,
  Radio,
  RefreshCw,
  SkipForward,
  Square,
  Terminal,
  Trash2,
  Workflow,
  XCircle,
} from 'lucide-vue-next'
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
  attempt_no: number
  owner: string
  last_error?: string
}

interface DeployEdge {
  from_task_id: string
  to_task_id: string
  satisfied: boolean
}

interface TimelineEvent {
  seq: number
  event_type: string
  task_id?: string
  message: string
  created_at: string
}

interface JobSnapshot {
  job: DeployJob
  tasks: DeployTask[]
  edges: DeployEdge[]
  timeline?: TimelineEvent[]
  last_seq: number
}

const scope = useScopeStore()
const jobs = ref<DeployJob[]>([])
const activeSnap = ref<JobSnapshot | null>(null)
const timeline = ref<TimelineEvent[]>([])
const selectedTaskId = ref<string>('')
const sseConnected = ref(false)
const loading = ref(false)
let stopSSE: (() => void) | null = null

// Builder modal
const submitDlg = ref(false)
const buildMode = ref<'diamond' | 'branch_fail' | 'custom'>('diamond')
const jobName = ref('HIS 双区灰度发布流水线')
const customSteps = ref<Array<{ key: string; name: string; taskType: string; deps: string[]; maxRetries: number }>>([
  { key: 'check', name: '环境预检', taskType: 'CHECK', deps: [], maxRetries: 0 },
  { key: 'deploy-a', name: 'A 区更新', taskType: 'SYNC', deps: ['check'], maxRetries: 1 },
  { key: 'verify', name: '探活验收', taskType: 'VERIFY', deps: ['deploy-a'], maxRetries: 0 },
])

// Organize active job's tasks into horizontal topological stage waves (KubeSphere DevOps Pipeline look!)
const pipelineStages = computed<DeployTask[][]>(() => {
  if (!activeSnap.value) return []
  const tasks = activeSnap.value.tasks || []
  const edges = activeSnap.value.edges || []
  const byId = new Map(tasks.map((t) => [t.task_id, t]))
  const inDeg = new Map<string, number>()
  const adj = new Map<string, string[]>()
  for (const t of tasks) {
    inDeg.set(t.task_id, 0)
    adj.set(t.task_id, [])
  }
  for (const e of edges) {
    inDeg.set(e.to_task_id, (inDeg.get(e.to_task_id) || 0) + 1)
    adj.get(e.from_task_id)?.push(e.to_task_id)
  }
  const stages: DeployTask[][] = []
  let wave = tasks.filter((t) => (inDeg.get(t.task_id) || 0) === 0).map((t) => t.task_id)
  const placed = new Set<string>()
  while (wave.length > 0) {
    stages.push(wave.map((id) => byId.get(id)!).filter(Boolean))
    const nextWave: string[] = []
    for (const u of wave) {
      placed.add(u)
      for (const v of adj.get(u) || []) {
        const d = (inDeg.get(v) || 1) - 1
        inDeg.set(v, d)
        if (d === 0) nextWave.push(v)
      }
    }
    wave = nextWave
  }
  const leftovers = tasks.filter((t) => !placed.has(t.task_id))
  if (leftovers.length > 0) stages.push(leftovers)
  return stages
})

const filteredTimeline = computed(() => {
  if (!selectedTaskId.value) return timeline.value
  return timeline.value.filter((ev) => !ev.task_id || ev.task_id === selectedTaskId.value)
})

function stateText(st: string) {
  const m: Record<string, string> = {
    SUCCEEDED: '已完成',
    RUNNING: '执行中',
    RECONCILING: '核对中',
    READY: '待调度',
    PENDING: '等待前置',
    FAILED: '失败',
    CANCELLED: '已取消',
    SKIPPED: '已跳过',
  }
  return m[st] || st
}

function statePill(state: string) {
  if (state === 'SUCCEEDED') return 'ks-pill-ok'
  if (state === 'FAILED') return 'ks-pill-danger'
  if (state === 'CANCELLED' || state === 'SKIPPED') return 'ks-pill-warn'
  return 'ks-pill-blue'
}

function fmtShortTime(iso: string) {
  if (!iso) return ''
  return new Date(iso).toLocaleTimeString('zh-CN', { hour12: false })
}

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
  selectedTaskId.value = ''
  const { data } = await api.get<JobSnapshot>(`/api/v1/deploy/jobs/${jobId}?timeline=true`)
  activeSnap.value = data
  timeline.value = data.timeline || []

  if (!['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(data.job.state)) {
    sseConnected.value = true
    stopSSE = subscribeJobTimeline(jobId, data.last_seq, {
      onEvent: (seq, _evType, evData) => {
        if (!timeline.value.some((t) => t.seq === seq)) {
          timeline.value.push(evData as unknown as TimelineEvent)
        }
        void refreshSnapshotQuiet(jobId)
      },
      onResetRequired: () => void openJob(jobId),
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
    if (['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(data.job.state)) closeStream()
  } catch {
    // ignore transient poll
  }
}

async function submitJob() {
  let payload: Record<string, unknown>
  if (buildMode.value === 'diamond') {
    payload = {
      name: jobName.value,
      projectId: scope.projectId || 'prod-clinical',
      explicitGraph: true,
      tasks: [
        { key: 'preflight', name: '配置与环境预检', taskType: 'CHECK', payload: { delayMs: 320 } },
        { key: 'zone-a', name: 'A 区灰度发布', taskType: 'SYNC', payload: { delayMs: 420 } },
        { key: 'zone-b', name: 'B 区灰度发布', taskType: 'SYNC', payload: { delayMs: 480 } },
        { key: 'verify-all', name: '全链路健康验收', taskType: 'VERIFY', payload: { delayMs: 280 } },
      ],
      edges: [
        { from: 'preflight', to: 'zone-a' },
        { from: 'preflight', to: 'zone-b' },
        { from: 'zone-a', to: 'verify-all' },
        { from: 'zone-b', to: 'verify-all' },
      ],
    }
  } else if (buildMode.value === 'branch_fail') {
    payload = {
      name: `${jobName.value}（容错隔离演练）`,
      projectId: scope.projectId || 'prod-clinical',
      explicitGraph: true,
      tasks: [
        { key: 'fault-node', name: '故障注入节点', taskType: 'CHECK', maxRetries: 1, payload: { delayMs: 220, simulateFail: true } },
        { key: 'downstream', name: '受影响下游步骤', taskType: 'SYNC', payload: { delayMs: 200 } },
        { key: 'healthy-branch', name: '独立健康业务分支', taskType: 'VERIFY', payload: { delayMs: 380 } },
      ],
      edges: [{ from: 'fault-node', to: 'downstream' }],
    }
  } else {
    const edges: Array<{ from: string; to: string }> = []
    for (const s of customSteps.value) {
      for (const dep of s.deps) {
        if (dep) edges.push({ from: dep, to: s.key })
      }
    }
    payload = {
      name: jobName.value,
      projectId: scope.projectId || 'prod-clinical',
      explicitGraph: true,
      tasks: customSteps.value.map((s) => ({
        key: s.key,
        name: s.name,
        taskType: s.taskType,
        maxRetries: s.maxRetries,
        payload: { delayMs: 300 },
      })),
      edges,
    }
  }

  try {
    const res = await api.post<JobSnapshot>('/api/v1/deploy/jobs', payload, {
      idempotencyKey: newIdempotencyKey('release'),
    })
    ElMessage.success('发布作业已提交，后台调度引擎正在执行')
    submitDlg.value = false
    await loadJobs()
    await openJob(res.data.job.job_id)
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function cancelActiveJob() {
  if (!activeSnap.value) return
  try {
    await api.post(
      `/api/v1/deploy/jobs/${activeSnap.value.job.job_id}/cancel`,
      { reason: 'user-cancelled' },
      { ifMatch: activeSnap.value.job.version, idempotencyKey: newIdempotencyKey('cancel') }
    )
    ElMessage.warning('已中止未开始的后续阶段')
    await openJob(activeSnap.value.job.job_id)
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

onMounted(loadJobs)
onBeforeUnmount(closeStream)
watch(() => [scope.tenantId, scope.appId], loadJobs)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="发布与作业编排"
      description="编排多阶段并发发布流水线，可视化跟踪每个执行步骤的状态流转、异常分支隔离与实时日志输出。"
      :icon="Workflow"
      :badge="sseConnected ? '实时日志连接中' : `${jobs.length} 次发布记录`"
    >
      <template #actions>
        <el-button :loading="loading" @click="loadJobs"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" data-testid="submit-dag-btn" @click="submitDlg = true">
          <Play :size="14" /><span class="ks-gap">发起发布作业</span>
        </el-button>
      </template>
    </KsBanner>

    <div class="ks-split">
      <!-- Left: Release Runs History -->
      <div class="ki-card">
        <div class="ki-card__title"><span>发布记录</span></div>
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
              <span class="ki-badge" :class="statePill(j.state)">{{ stateText(j.state) }}</span>
            </div>
            <div class="ks-job-item__meta">
              <span>发起人: {{ j.created_by }}</span>
              <span>{{ fmtShortTime(j.created_at) }}</span>
            </div>
          </button>
        </div>
      </div>

      <!-- Right: Visual Pipeline Stages + Live Streaming Console -->
      <div v-if="activeSnap" class="ks-detail-col">
        <div class="ki-card">
          <div class="ki-card__title">
            <div>
              <span>{{ activeSnap.job.name }}</span>
              <span class="ki-badge ks-ml" :class="statePill(activeSnap.job.state)">{{ stateText(activeSnap.job.state) }}</span>
            </div>
            <el-button
              v-if="!['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(activeSnap.job.state)"
              size="small"
              type="danger"
              plain
              @click="cancelActiveJob"
            >
              <Square :size="12" /><span class="ks-gap-xs">中止发布</span>
            </el-button>
          </div>

          <!-- Authentic KubeSphere Horizontal Pipeline Stages Flow -->
          <div class="ks-pipeline-flow">
            <template v-for="(stage, sIdx) in pipelineStages" :key="sIdx">
              <div class="ks-stage-col">
                <div class="ks-stage-col__label">阶段 {{ sIdx + 1 }}</div>
                <div class="ks-stage-nodes">
                  <button
                    v-for="task in stage"
                    :key="task.task_id"
                    type="button"
                    class="ks-step-card"
                    :class="[
                      `is-${task.state.toLowerCase()}`,
                      { 'is-selected': selectedTaskId === task.task_id },
                    ]"
                    @click="selectedTaskId = selectedTaskId === task.task_id ? '' : task.task_id"
                  >
                    <div class="ks-step-card__head">
                      <CheckCircle2 v-if="task.state === 'SUCCEEDED'" :size="15" class="ks-step-icon--ok" />
                      <XCircle v-else-if="task.state === 'FAILED'" :size="15" class="ks-step-icon--err" />
                      <SkipForward v-else-if="task.state === 'SKIPPED'" :size="15" class="ks-step-icon--warn" />
                      <Clock v-else :size="15" class="ks-step-icon--run" />
                      <span class="ks-step-card__name">{{ task.name }}</span>
                    </div>
                    <div class="ks-step-card__meta">
                      <span>{{ stateText(task.state) }}</span>
                      <span v-if="task.attempt_no > 1">重试 #{{ task.attempt_no }}</span>
                    </div>
                  </button>
                </div>
              </div>
              <div v-if="sIdx < pipelineStages.length - 1" class="ks-stage-arrow">→</div>
            </template>
          </div>
        </div>

        <!-- Authentic Dark Execution Log Terminal -->
        <div class="ki-card ks-term-card">
          <div class="ks-term-head">
            <div class="ks-term-title">
              <Terminal :size="14" />
              <span>实时执行日志</span>
              <span v-if="selectedTaskId" class="ks-term-filter">
                已按选中步骤筛选
                <button type="button" class="ks-term-clear" @click="selectedTaskId = ''">显示全部</button>
              </span>
            </div>
            <div v-if="sseConnected" class="ks-live-pill">
              <Radio :size="12" />
              <span>实时推送中</span>
            </div>
          </div>

          <div class="ks-terminal ki-mono">
            <div v-for="ev in filteredTimeline" :key="ev.seq" class="ks-log-line">
              <span class="ks-log-time">{{ fmtShortTime(ev.created_at) }}</span>
              <span class="ks-log-tag">[{{ ev.event_type }}]</span>
              <span class="ks-log-msg">{{ ev.message }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Pipeline Builder Modal -->
    <el-dialog v-model="submitDlg" title="发起新发布作业" width="580px">
      <el-form label-position="top">
        <el-form-item label="发布作业名称" required><el-input v-model="jobName" /></el-form-item>
        <el-form-item label="选择编排模式">
          <el-radio-group v-model="buildMode">
            <el-radio-button value="diamond">双区并发灰度</el-radio-button>
            <el-radio-button value="branch_fail">容错隔离演练</el-radio-button>
            <el-radio-button value="custom">自定义多阶段</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <div v-if="buildMode === 'diamond'" class="ks-preset-desc">
          自动按 3 阶段编排：<strong>配置预检 → A/B 双区并发发布 → 全链路探活验收</strong>，双区全部完成后自动汇聚验收。
        </div>
        <div v-else-if="buildMode === 'branch_fail'" class="ks-preset-desc">
          演练单分支异常重试与下游自动跳过，同时保障独立健康业务分支继续完成不受影响。
        </div>
        <div v-else class="ks-custom-builder">
          <div class="ks-builder-head">
            <span>自定义发布步骤</span>
            <el-button
              size="small"
              @click="customSteps.push({ key: `step-${customSteps.length + 1}`, name: `步骤 ${customSteps.length + 1}`, taskType: 'SYNC', deps: [], maxRetries: 0 })"
            >
              <Plus :size="12" /><span class="ks-gap-xs">添加步骤</span>
            </el-button>
          </div>
          <div v-for="(st, idx) in customSteps" :key="idx" class="ks-builder-row">
            <el-input v-model="st.key" size="small" placeholder="标识" style="width: 110px" />
            <el-input v-model="st.name" size="small" placeholder="步骤名称" style="flex: 1" />
            <el-select v-model="st.deps" multiple collapse-tags size="small" placeholder="前置依赖" style="width: 160px">
              <el-option v-for="other in customSteps.filter((x) => x.key !== st.key)" :key="other.key" :label="other.name" :value="other.key" />
            </el-select>
            <el-button size="small" type="danger" plain :disabled="customSteps.length <= 1" @click="customSteps.splice(idx, 1)">
              <Trash2 :size="12" />
            </el-button>
          </div>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="submitDlg = false">取消</el-button>
        <el-button type="primary" data-testid="confirm-submit-dag" @click="submitJob">立即启动发布</el-button>
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

.ks-ml {
  margin-left: 8px;
}

.ks-split {
  display: grid;
  grid-template-columns: 290px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
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
  display: flex;
  justify-content: space-between;
  margin-top: 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

/* KubeSphere DevOps Pipeline Stage Graph */
.ks-pipeline-flow {
  display: flex;
  align-items: stretch;
  gap: 12px;
  overflow-x: auto;
  padding: 6px 2px 4px;
}

.ks-stage-col {
  min-width: 190px;
  padding: 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ks-stage-col__label {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
}

.ks-stage-nodes {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-step-card {
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  text-align: left;
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease),
    box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.ks-step-card:hover,
.ks-step-card.is-selected {
  border-color: var(--ki-brand);
  box-shadow: var(--ki-shadow-hover);
}

.ks-step-card.is-succeeded {
  border-left: 3px solid var(--ki-status-ok);
}

.ks-step-card.is-failed {
  border-left: 3px solid var(--ki-status-danger);
}

.ks-step-card.is-skipped {
  border-left: 3px solid var(--ki-status-warn);
}

.ks-step-card__head {
  display: flex;
  align-items: center;
  gap: 7px;
}

.ks-step-icon--ok {
  color: var(--ki-status-ok-text);
  flex: none;
}

.ks-step-icon--err {
  color: var(--ki-status-danger-text);
  flex: none;
}

.ks-step-icon--warn {
  color: var(--ki-status-warn-text);
  flex: none;
}

.ks-step-icon--run {
  color: var(--ki-status-running-text);
  flex: none;
}

.ks-step-card__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-step-card__meta {
  display: flex;
  justify-content: space-between;
  margin-top: 4px;
  font-size: 11px;
  color: var(--ki-text-muted);
}

.ks-stage-arrow {
  display: flex;
  align-items: center;
  color: var(--ki-border-strong);
  font-size: 18px;
  font-weight: var(--ki-fw-bold);
}

/* Dark Streaming Terminal Console */
.ks-term-card {
  padding: 0;
  overflow: hidden;
}

.ks-term-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 18px;
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ks-term-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-term-filter {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  font-weight: var(--ki-fw-regular);
}

.ks-term-clear {
  border: none;
  background: transparent;
  color: var(--ki-text-link);
  cursor: pointer;
  margin-left: 4px;
}

.ks-live-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-status-ok-text);
  font-weight: var(--ki-fw-semibold);
}

.ks-terminal {
  padding: 14px 18px;
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  max-height: 290px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.ks-log-line {
  display: flex;
  gap: 10px;
  font-size: 12px;
  line-height: 1.55;
}

.ks-log-time {
  color: var(--ki-ansi-8);
  flex: none;
}

.ks-log-tag {
  color: var(--ki-ansi-2);
  flex: none;
}

.ks-log-msg {
  color: var(--ki-term-fg);
  overflow-wrap: anywhere;
}

.ks-preset-desc {
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.ks-custom-builder {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-builder-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
}

.ks-builder-row {
  display: flex;
  align-items: center;
  gap: 8px;
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
</style>
