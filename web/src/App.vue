<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceStore } from '@/stores/workspace'

const ws = useWorkspaceStore()
const { locale } = useI18n()

watch(
  () => ws.lang,
  (v) => {
    locale.value = v
  },
  { immediate: true }
)
</script>

<template>
  <router-view />
</template>
