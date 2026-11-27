<script setup lang="ts">
import KiStatusBadge from './ki/KiStatusBadge.vue'

defineProps<{ status: string; label?: string }>()
</script>

<template>
  <KiStatusBadge :status="status" :label="label" />
</template>
