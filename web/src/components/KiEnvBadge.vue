<script setup lang="ts">
import { computed } from 'vue'
import { ShieldAlert, Layers } from 'lucide-vue-next'

const props = defineProps<{ env: string }>()

const styleMap = computed(() => {
  switch ((props.env || '').toLowerCase()) {
    case 'prod':
      return { bg: 'var(--ki-env-prod-bg)', color: 'var(--ki-env-prod-text)', label: 'PROD 生产', prod: true }
    case 'staging':
      return { bg: 'var(--ki-env-staging-bg)', color: 'var(--ki-env-staging-text)', label: 'STAGING 预发', prod: false }
    case 'dev':
      return { bg: 'var(--ki-env-dev-bg)', color: 'var(--ki-env-dev-text)', label: 'DEV 开发', prod: false }
    default:
      return { bg: 'var(--ki-env-sandbox-bg)', color: 'var(--ki-env-sandbox-text)', label: 'SANDBOX 沙箱', prod: false }
  }
})
</script>

<template>
  <span class="ki-badge" :style="{ background: styleMap.bg, color: styleMap.color }">
    <ShieldAlert v-if="styleMap.prod" :size="13" />
    <Layers v-else :size="13" />
    {{ styleMap.label }}
  </span>
</template>
