<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ status: string }>()

const cfg = computed(() => {
  const s = (props.status || '').toLowerCase()
  if (['succeeded', 'healthy', 'running_ok', 'ready', 'valid', 'pass', 'ok', 'approved'].includes(s)) {
    return { bg: 'var(--ki-status-ok-subtle)', color: 'var(--ki-status-ok-text)', dot: 'var(--ki-status-ok)', pulse: false }
  }
  if (['running', 'probing', 'in_progress'].includes(s)) {
    return { bg: 'var(--ki-status-running-subtle)', color: 'var(--ki-status-running-text)', dot: 'var(--ki-status-running)', pulse: true }
  }
  if (['awaiting_approval', 'pending', 'warn', 'drift', 'degraded'].includes(s)) {
    return { bg: 'var(--ki-status-warn-subtle)', color: 'var(--ki-status-warn-text)', dot: 'var(--ki-status-warn)', pulse: false }
  }
  if (['failed', 'rejected', 'blocked', 'unhealthy', 'revoked', 'error'].includes(s)) {
    return { bg: 'var(--ki-status-danger-subtle)', color: 'var(--ki-status-danger-text)', dot: 'var(--ki-status-danger)', pulse: false }
  }
  return { bg: 'var(--ki-status-neutral-subtle)', color: 'var(--ki-status-neutral-text)', dot: 'var(--ki-status-neutral)', pulse: false }
})
</script>

<template>
  <span class="ki-badge ki-mono" :style="{ background: cfg.bg, color: cfg.color }">
    <span class="ki-dot" :class="{ 'ki-dot-pulse': cfg.pulse }" :style="{ background: cfg.dot }" />
    {{ status }}
  </span>
</template>
