<script setup lang="ts">
import { computed } from 'vue'
import { Loader2 } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-ghost'
    size?: 'sm' | 'md'
    loading?: boolean
    disabled?: boolean
    type?: 'button' | 'submit' | 'reset'
    ariaLabel?: string
    block?: boolean
  }>(),
  {
    variant: 'secondary',
    size: 'md',
    loading: false,
    disabled: false,
    type: 'button',
    ariaLabel: undefined,
    block: false,
  }
)

const emit = defineEmits<{ (e: 'click', ev: MouseEvent): void }>()

const classes = computed(() => [
  'ki-btn',
  `ki-btn--${props.variant}`,
  `ki-btn--${props.size}`,
  { 'ki-btn--block': props.block, 'ki-btn--loading': props.loading },
])

function handleClick(ev: MouseEvent) {
  if (props.disabled || props.loading) return
  emit('click', ev)
}
</script>

<template>
  <button
    :type="type"
    :class="classes"
    :disabled="disabled || loading"
    :aria-busy="loading || undefined"
    :aria-label="ariaLabel"
    @click="handleClick"
  >
    <Loader2 v-if="loading" :size="14" class="ki-btn__spinner" aria-hidden="true" />
    <span class="ki-btn__label" :class="{ 'ki-btn__label--hidden': loading }">
      <slot />
    </span>
  </button>
</template>

<style scoped>
.ki-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--ki-space-1);
  height: var(--ki-control-h);
  padding: 0 var(--ki-btn-px);
  border-radius: var(--ki-radius-sm);
  border: 1px solid transparent;
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-body);
  font-weight: var(--ki-fw-medium);
  line-height: 1;
  cursor: pointer;
  white-space: nowrap;
  user-select: none;
  transition: background-color var(--ki-dur-fast) var(--ki-ease),
    border-color var(--ki-dur-fast) var(--ki-ease),
    color var(--ki-dur-fast) var(--ki-ease);
}

.ki-btn--sm {
  height: var(--ki-control-h-sm);
  padding: 0 var(--ki-space-2);
  font-size: var(--ki-fs-sm);
}

.ki-btn--block {
  width: 100%;
}

.ki-btn--primary {
  background-color: var(--ki-brand-solid);
  border-color: var(--ki-brand-solid);
  color: var(--ki-text-on-solid);
}

.ki-btn--primary:hover:not(:disabled) {
  background-color: var(--ki-brand-solid-hover);
  border-color: var(--ki-brand-solid-hover);
}

.ki-btn--primary:active:not(:disabled) {
  background-color: var(--ki-brand-solid-active);
}

.ki-btn--secondary {
  background-color: var(--ki-bg-surface);
  border-color: var(--ki-border-control);
  color: var(--ki-text-primary);
}

.ki-btn--secondary:hover:not(:disabled) {
  background-color: var(--ki-bg-subtle);
  border-color: var(--ki-border-strong);
}

.ki-btn--ghost {
  background-color: transparent;
  border-color: transparent;
  color: var(--ki-text-secondary);
}

.ki-btn--ghost:hover:not(:disabled) {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.ki-btn--danger {
  background-color: var(--ki-danger-solid);
  border-color: var(--ki-danger-solid);
  color: var(--ki-text-on-solid);
}

.ki-btn--danger:hover:not(:disabled) {
  background-color: var(--ki-danger-solid-hover);
  border-color: var(--ki-danger-solid-hover);
}

.ki-btn--danger:active:not(:disabled) {
  background-color: var(--ki-danger-solid-active);
}

.ki-btn--danger-ghost {
  background-color: transparent;
  border-color: var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ki-btn--danger-ghost:hover:not(:disabled) {
  background-color: var(--ki-status-danger-subtle);
}

.ki-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.ki-btn__spinner {
  position: absolute;
  animation: ki-spin 0.9s linear infinite;
}

.ki-btn__label {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
}

.ki-btn__label--hidden {
  visibility: hidden;
}

@keyframes ki-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
