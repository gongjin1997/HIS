<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    title: string
    size?: 'sm' | 'md'
    closeOnBackdrop?: boolean
  }>(),
  {
    size: 'md',
    closeOnBackdrop: true,
  }
)

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

function close() {
  emit('update:modelValue', false)
}

function onKeydown(e: KeyboardEvent) {
  if (props.modelValue && e.key === 'Escape') {
    close()
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="ki-modal-backdrop" @click.self="closeOnBackdrop && close()">
      <div
        class="ki-modal"
        :class="`ki-modal--${size}`"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
      >
        <header class="ki-modal__head">
          <div class="ki-flex-row">
            <slot name="title-prefix" />
            <h2 class="ki-modal__title">{{ title }}</h2>
          </div>
          <button type="button" class="ki-modal__close" aria-label="关闭对话框" @click="close">
            <X :size="16" />
          </button>
        </header>
        <div class="ki-modal__body">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="ki-modal__footer">
          <slot name="footer" />
        </footer>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ki-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--ki-z-modal);
  background-color: var(--ki-overlay);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--ki-space-4);
}

.ki-modal {
  display: flex;
  flex-direction: column;
  max-width: 96vw;
  max-height: 90vh;
  background-color: var(--ki-bg-elevated);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-lg);
  box-shadow: var(--ki-shadow-modal);
  overflow: hidden;
}

.ki-modal--sm {
  width: var(--ki-modal-w-sm);
}

.ki-modal--md {
  width: var(--ki-modal-w-md);
}

.ki-modal__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-4) var(--ki-space-5);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-modal__title {
  font-size: var(--ki-fs-section);
  line-height: var(--ki-lh-section);
  color: var(--ki-text-primary);
}

.ki-modal__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid transparent;
  background-color: transparent;
  color: var(--ki-text-secondary);
  cursor: pointer;
}

.ki-modal__close:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.ki-modal__body {
  overflow-y: auto;
  padding: var(--ki-space-5);
}

.ki-modal__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--ki-space-2);
  padding: var(--ki-space-3) var(--ki-space-5);
  border-top: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-surface);
}
</style>
