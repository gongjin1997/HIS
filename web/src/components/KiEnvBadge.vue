<script setup lang="ts">
import KiEnvBadge from './ki/KiEnvBadge.vue'

defineProps<{ env: string }>()
</script>

<template>
  <KiEnvBadge :env="env" />
</template>
