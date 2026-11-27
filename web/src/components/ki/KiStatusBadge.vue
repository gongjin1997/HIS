<script setup lang="ts">
import { computed } from 'vue'
import { CheckCircle2, AlertTriangle, XCircle, CircleDot, MinusCircle } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    status: string
    label?: string
  }>(),
  { label: '' }
)

type Tone = 'ok' | 'running' | 'warn' | 'danger' | 'neutral'

const STATUS_LABELS: Record<string, string> = {
  awaiting_approval: '待审批',
  pending: '待处理',
  running: '执行中',
  probing: '探活中',
  initializing: '初始化中',
  in_progress: '进行中',
  applying: '变更中',
  planning: '规划中',
  succeeded: '已成功',
  ready: '就绪',
  active: '服役中',
  healthy: '健康',
  online: '在线',
  valid: '校验通过',
  pass: '通过',
  ok: '正常',
  reachable: '可达',
  approved: '已批准',
  warn: '告警',
  drift: '存在漂移',
  degraded: '降级',
  draining: '维护排空',
  paused: '已暂停',
  failed: '已失败',
  rejected: '已驳回',
  blocked: '离线拦截',
  unhealthy: '异常',
  unreachable: '不可达',
  missing: '目录缺失',
  revoked: '已吊销',
  error: '错误',
  tampered: '检测到篡改',
  offline: '离线',
  cancelled: '已取消',
  planned: '已规划',
  draft: '草稿',
  created: '已创建',
  retired: '已下线',
  uninstalled: '已卸载',
  unconfigured: '未配置',
  unprobed: '未探测',
  never: '未执行',
}

const tone = computed<Tone>(() => {
  const s = (props.status || '').toLowerCase()
  if (
    ['succeeded', 'healthy', 'running_ok', 'ready', 'valid', 'pass', 'ok', 'reachable', 'approved', 'active', 'online'].includes(s)
  ) {
    return 'ok'
  }
  if (['running', 'probing', 'initializing', 'in_progress', 'applying', 'planning'].includes(s)) {
    return 'running'
  }
  if (['awaiting_approval', 'pending', 'warn', 'drift', 'degraded', 'draining', 'paused'].includes(s)) {
    return 'warn'
  }
  if (['failed', 'rejected', 'blocked', 'unhealthy', 'unreachable', 'missing', 'revoked', 'error', 'tampered', 'offline'].includes(s)) {
    return 'danger'
  }
  return 'neutral'
})

const displayLabel = computed(() => {
  if (props.label) return props.label
  const key = (props.status || '').toLowerCase()
  return STATUS_LABELS[key] || props.status || '—'
})
</script>

<template>
  <span
    class="ki-badge ki-status-badge"
    :class="`ki-status-badge--${tone}`"
    :data-status="status"
    role="status"
  >
    <CheckCircle2 v-if="tone === 'ok'" :size="12" aria-hidden="true" />
    <CircleDot v-else-if="tone === 'running'" :size="12" class="ki-dot-pulse" aria-hidden="true" />
    <AlertTriangle v-else-if="tone === 'warn'" :size="12" aria-hidden="true" />
    <XCircle v-else-if="tone === 'danger'" :size="12" aria-hidden="true" />
    <MinusCircle v-else :size="12" aria-hidden="true" />
    <span>{{ displayLabel }}</span>
  </span>
</template>

<style scoped>
.ki-status-badge {
  border: 1px solid transparent;
  font-family: var(--ki-font-sans);
}

.ki-status-badge--ok {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
  border-color: var(--ki-status-ok-border);
}

.ki-status-badge--running {
  background-color: var(--ki-status-running-subtle);
  color: var(--ki-status-running-text);
  border-color: var(--ki-status-running-border);
}

.ki-status-badge--warn {
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
  border-color: var(--ki-status-warn-border);
}

.ki-status-badge--danger {
  background-color: var(--ki-status-danger-subtle);
  color: var(--ki-status-danger-text);
  border-color: var(--ki-status-danger-border);
}

.ki-status-badge--neutral {
  background-color: var(--ki-status-neutral-subtle);
  color: var(--ki-status-neutral-text);
  border-color: var(--ki-status-neutral-border);
}
</style>
