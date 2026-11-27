<script setup lang="ts">
import { Inbox, SearchX, Lock, AlertOctagon } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    variant?: 'first-run' | 'no-result' | 'no-permission' | 'error'
    title: string
    description?: string
  }>(),
  {
    variant: 'first-run',
    description: '',
  }
)
</script>

<template>
  <div class="ki-empty" :class="`ki-empty--${variant}`">
    <div class="ki-empty__icon" aria-hidden="true">
      <Inbox v-if="variant === 'first-run'" :size="24" />
      <SearchX v-else-if="variant === 'no-result'" :size="24" />
      <Lock v-else-if="variant === 'no-permission'" :size="24" />
      <AlertOctagon v-else :size="24" />
    </div>
    <h3 class="ki-empty__title">{{ title }}</h3>
    <p v-if="description" class="ki-empty__desc">{{ description }}</p>
    <div v-if="$slots.actions" class="ki-empty__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.ki-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: var(--ki-space-8) var(--ki-space-6);
  gap: var(--ki-space-2);
}

.ki-empty__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.ki-empty--error .ki-empty__icon {
  background-color: var(--ki-status-danger-subtle);
  color: var(--ki-status-danger-text);
}

.ki-empty--no-permission .ki-empty__icon {
  background-color: var(--ki-status-warn-subtle);
  color: var(--ki-status-warn-text);
}

.ki-empty__title {
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.ki-empty__desc {
  margin: 0;
  max-width: 420px;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
}

.ki-empty__actions {
  margin-top: var(--ki-space-2);
  display: flex;
  gap: var(--ki-space-2);
}
</style>
