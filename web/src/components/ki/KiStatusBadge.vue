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

const tone = computed<Tone>(() => {
  const s = (props.status || '').toLowerCase()
  if (['succeeded', 'healthy', 'running_ok', 'ready', 'valid', 'pass', 'ok', 'approved', 'active', 'online'].includes(s)) {
    return 'ok'
  }
  if (['running', 'probing', 'in_progress', 'applying', 'planning'].includes(s)) {
    return 'running'
  }
  if (['awaiting_approval', 'pending', 'warn', 'drift', 'degraded', 'paused'].includes(s)) {
    return 'warn'
  }
  if (['failed', 'rejected', 'blocked', 'unhealthy', 'revoked', 'error', 'tampered', 'offline'].includes(s)) {
    return 'danger'
  }
  return 'neutral'
})

const displayLabel = computed(() => props.label || props.status || '—')
</script>

<template>
  <span class="ki-badge ki-status-badge ki-mono" :class="`ki-status-badge--${tone}`" role="status">
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
