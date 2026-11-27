<script setup lang="ts">
import { Info, AlertTriangle, ShieldAlert, WifiOff } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    variant?: 'info' | 'warn' | 'danger' | 'offline-lock'
    sticky?: boolean
    text?: string
  }>(),
  {
    variant: 'info',
    sticky: false,
    text: '',
  }
)
</script>

<template>
  <div
    class="ki-banner"
    :class="[`ki-banner--${variant}`, { 'ki-banner--sticky': sticky }]"
    :role="variant === 'danger' ? 'alert' : 'status'"
  >
    <div class="ki-banner__content">
      <WifiOff v-if="variant === 'offline-lock'" :size="14" aria-hidden="true" />
      <ShieldAlert v-else-if="variant === 'danger'" :size="14" aria-hidden="true" />
      <AlertTriangle v-else-if="variant === 'warn'" :size="14" aria-hidden="true" />
      <Info v-else :size="14" aria-hidden="true" />
      <slot>{{ text }}</slot>
    </div>
    <div v-if="$slots.actions" class="ki-flex-row">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.ki-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  min-height: 32px;
  padding: var(--ki-space-1) var(--ki-space-3);
  border-radius: var(--ki-radius-sm);
  border: 1px solid transparent;
  font-size: var(--ki-fs-sm);
}

.ki-banner--sticky {
  position: sticky;
  top: 0;
  z-index: var(--ki-z-sticky);
  border-radius: 0;
  border-inline: 0;
}

.ki-banner__content {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  min-width: 0;
}

.ki-banner--info {
  background-color: var(--ki-brand-subtle);
  border-color: var(--ki-brand-border);
  color: var(--ki-text-primary);
}

.ki-banner--warn {
  background-color: var(--ki-status-warn-subtle);
  border-color: var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
}

.ki-banner--danger {
  background-color: var(--ki-status-danger-subtle);
  border-color: var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ki-banner--offline-lock {
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}
</style>
