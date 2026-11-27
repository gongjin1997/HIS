<script setup lang="ts">
import KiDomainTag from './ki/KiDomainTag.vue'

defineProps<{ target: string }>()
</script>

<template>
  <KiDomainTag kind="target" :value="target" />
</template>
