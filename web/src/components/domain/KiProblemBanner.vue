<script setup lang="ts">
import { AlertOctagon, Lightbulb } from 'lucide-vue-next'
import type { ProblemDetail } from '@/api/client'
import KiMonoText from '@/components/ki/KiMonoText.vue'

withDefaults(
  defineProps<{
    problem: ProblemDetail | null
    compact?: boolean
  }>(),
  {
    compact: false,
  }
)
</script>

<template>
  <div v-if="problem" class="ki-problem" :class="{ 'ki-problem--compact': compact }" role="alert" aria-live="assertive">
    <div class="ki-problem__head">
      <div class="ki-flex-row">
        <AlertOctagon :size="16" class="ki-problem__icon" aria-hidden="true" />
        <span class="ki-badge ki-mono ki-problem__code">{{ problem.code || 'KI-API-0001' }}</span>
        <strong class="ki-problem__title">{{ problem.title || '操作执行失败' }}</strong>
      </div>
      <div v-if="problem.traceId" class="ki-flex-row ki-problem__trace">
        <span class="ki-muted">traceId</span>
        <KiMonoText :value="problem.traceId" :head="10" :tail="6" />
      </div>
    </div>

    <p v-if="problem.detail" class="ki-problem__detail">{{ problem.detail }}</p>

    <div v-if="problem.suggestion" class="ki-problem__sug">
      <Lightbulb :size="13" aria-hidden="true" />
      <span>{{ problem.suggestion }}</span>
    </div>

    <div v-if="$slots.actions" class="ki-problem__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.ki-problem {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-2);
  padding: var(--ki-space-3) var(--ki-space-4);
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  border-radius: var(--ki-radius-md);
}

.ki-problem--compact {
  padding: var(--ki-space-2) var(--ki-space-3);
}

.ki-problem__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  flex-wrap: wrap;
}

.ki-problem__icon {
  color: var(--ki-status-danger-text);
  flex-shrink: 0;
}

.ki-problem__code {
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ki-problem__title {
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-body);
}

.ki-problem__trace {
  font-size: var(--ki-fs-xs);
}

.ki-problem__detail {
  margin: 0;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
}

.ki-problem__sug {
  display: flex;
  align-items: flex-start;
  gap: var(--ki-space-2);
  padding: var(--ki-space-2) var(--ki-space-3);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-sm);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sm);
}

.ki-problem__actions {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  padding-top: var(--ki-space-1);
}
</style>
