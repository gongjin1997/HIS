<script setup lang="ts">
import { computed } from 'vue'
import { ShieldAlert, Layers, Code2, FlaskConical } from 'lucide-vue-next'

const props = defineProps<{ env: string }>()

const normalized = computed(() => {
  const e = (props.env || '').toLowerCase()
  if (e === 'prod' || e === 'staging' || e === 'dev' || e === 'sandbox') return e
  return 'sandbox'
})

const label = computed(() => {
  switch (normalized.value) {
    case 'prod':
      return 'PROD 生产'
    case 'staging':
      return 'STAGING 预发'
    case 'dev':
      return 'DEV 开发'
    default:
      return 'SANDBOX 沙箱'
  }
})
</script>

<template>
  <span class="ki-badge ki-env-badge" :class="`ki-env-badge--${normalized}`">
    <ShieldAlert v-if="normalized === 'prod'" :size="13" aria-hidden="true" />
    <Layers v-else-if="normalized === 'staging'" :size="13" aria-hidden="true" />
    <Code2 v-else-if="normalized === 'dev'" :size="13" aria-hidden="true" />
    <FlaskConical v-else :size="13" aria-hidden="true" />
    <span>{{ label }}</span>
  </span>
</template>

<style scoped>
.ki-env-badge {
  border: 1px solid transparent;
  font-weight: var(--ki-fw-semibold);
}

.ki-env-badge--prod {
  background-color: var(--ki-env-prod-bg);
  color: var(--ki-env-prod-text);
  border-color: var(--ki-status-danger-border);
}

.ki-env-badge--staging {
  background-color: var(--ki-env-staging-bg);
  color: var(--ki-env-staging-text);
  border-color: var(--ki-status-warn-border);
}

.ki-env-badge--dev {
  background-color: var(--ki-env-dev-bg);
  color: var(--ki-env-dev-text);
  border-color: var(--ki-status-ok-border);
}

.ki-env-badge--sandbox {
  background-color: var(--ki-env-sandbox-bg);
  color: var(--ki-env-sandbox-text);
  border-color: var(--ki-status-neutral-border);
}
</style>
