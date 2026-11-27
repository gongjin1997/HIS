<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    modelValue: boolean
    title: string
    subtitle?: string
    size?: 'sm' | 'md' | 'lg'
  }>(),
  {
    subtitle: '',
    size: 'md',
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
    <div v-if="modelValue" class="ki-drawer-backdrop" @click.self="close">
      <aside
        class="ki-drawer"
        :class="`ki-drawer--${size}`"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
      >
        <header class="ki-drawer__head">
          <div class="ki-drawer__titles">
            <h2 class="ki-drawer__title">{{ title }}</h2>
            <p v-if="subtitle" class="ki-drawer__sub">{{ subtitle }}</p>
          </div>
          <button type="button" class="ki-drawer__close" aria-label="关闭抽屉" @click="close">
            <X :size="16" />
          </button>
        </header>
        <div class="ki-drawer__body">
          <slot />
        </div>
        <footer v-if="$slots.footer" class="ki-drawer__footer">
          <slot name="footer" />
        </footer>
      </aside>
    </div>
  </Teleport>
</template>

<style scoped>
.ki-drawer-backdrop {
  position: fixed;
  inset: 0;
  z-index: var(--ki-z-drawer);
  background-color: var(--ki-overlay);
  display: flex;
  justify-content: flex-end;
}

.ki-drawer {
  display: flex;
  flex-direction: column;
  height: 100%;
  max-width: 96vw;
  background-color: var(--ki-bg-elevated);
  border-left: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-modal);
}

.ki-drawer--sm {
  width: var(--ki-drawer-w-sm);
}

.ki-drawer--md {
  width: var(--ki-drawer-w-md);
}

.ki-drawer--lg {
  width: var(--ki-drawer-w-lg);
}

.ki-drawer__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-4) var(--ki-space-5);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-drawer__titles {
  min-width: 0;
}

.ki-drawer__title {
  font-size: var(--ki-fs-section);
  line-height: var(--ki-lh-section);
  color: var(--ki-text-primary);
}

.ki-drawer__sub {
  margin: var(--ki-space-1) 0 0;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
}

.ki-drawer__close {
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

.ki-drawer__close:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.ki-drawer__body {
  flex: 1;
  overflow-y: auto;
  padding: var(--ki-space-5);
}

.ki-drawer__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--ki-space-2);
  padding: var(--ki-space-3) var(--ki-space-5);
  border-top: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-surface);
}
</style>
