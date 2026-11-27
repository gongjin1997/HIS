<script setup lang="ts">
import { computed, ref } from 'vue'
import { Copy, Check } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    value: string
    head?: number
    tail?: number
    copyable?: boolean
    ellipsis?: boolean
  }>(),
  {
    head: 14,
    tail: 6,
    copyable: true,
    ellipsis: true,
  }
)

const copied = ref(false)

const rendered = computed(() => {
  const s = props.value || ''
  if (!s) return '—'
  if (!props.ellipsis || s.length <= props.head + props.tail + 2) return s
  return `${s.slice(0, props.head)}…${s.slice(-props.tail)}`
})

async function copyText() {
  if (!props.value) return
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    window.setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    // clipboard unavailable in restricted context
  }
}
</script>

<template>
  <span class="ki-mono-wrap" :title="value">
    <code class="ki-mono ki-mono-val">{{ rendered }}</code>
    <button
      v-if="copyable && value"
      type="button"
      class="ki-mono-copy"
      :class="{ 'ki-mono-copy--ok': copied }"
      :aria-label="copied ? '已复制' : '复制完整值'"
      @click.stop="copyText"
    >
      <Check v-if="copied" :size="12" />
      <Copy v-else :size="12" />
    </button>
  </span>
</template>

<style scoped>
.ki-mono-wrap {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
  max-width: 100%;
  vertical-align: middle;
}

.ki-mono-val {
  color: var(--ki-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-mono-copy {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: 0;
  border-radius: var(--ki-radius-xs);
  background-color: transparent;
  color: var(--ki-text-muted);
  cursor: pointer;
  opacity: 0.72;
  transition: opacity var(--ki-dur-fast) var(--ki-ease), color var(--ki-dur-fast) var(--ki-ease);
}

.ki-mono-copy:focus-visible,
.ki-mono-wrap:hover .ki-mono-copy {
  opacity: 1;
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.ki-mono-copy--ok {
  opacity: 1;
  color: var(--ki-status-ok-text);
}
</style>
