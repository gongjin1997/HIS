<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ target: string }>()

const style = computed(() => {
  switch (props.target) {
    case 'host-container':
      return { bg: 'var(--ki-badge-container-bg)', color: 'var(--ki-badge-container-text)' }
    case 'kubernetes':
      return { bg: 'var(--ki-badge-k8s-bg)', color: 'var(--ki-badge-k8s-text)' }
    default:
      return { bg: 'var(--ki-badge-binary-bg)', color: 'var(--ki-badge-binary-text)' }
  }
})
</script>

<template>
  <span class="ki-badge ki-mono" :style="{ background: style.bg, color: style.color }">
    {{ target }}
  </span>
</template>
