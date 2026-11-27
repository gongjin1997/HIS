<script setup lang="ts">
import KiDomainTag from './ki/KiDomainTag.vue'

defineProps<{ arch?: string; pageSize?: number }>()
</script>

<template>
  <KiDomainTag kind="arch" :value="arch || 'amd64'" :page-size="pageSize" />
</template>
