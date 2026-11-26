<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'

const props = defineProps<{ arch?: string; pageSize?: number }>()

const is64K = computed(() => (props.pageSize || 4096) >= 65536)
const label = computed(() => {
  const a = props.arch || 'amd64'
  return is64K.value ? `${a}-64K` : `${a}-4K`
})
</script>

<template>
  <span
    class="ki-badge ki-mono"
    :style="is64K
      ? { background: 'var(--ki-badge-arm64k-bg)', color: 'var(--ki-badge-arm64k-text)' }
      : { background: 'var(--ki-badge-arch-bg)', color: 'var(--ki-badge-arch-text)' }"
  >
    <AlertTriangle v-if="is64K" :size="12" />
    {{ label }}
  </span>
</template>
