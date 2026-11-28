<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useScopeStore } from '@/stores/scope'

const scope = useScopeStore()
const { locale } = useI18n()

watch(
  () => scope.lang,
  (v) => {
    locale.value = v
  },
  { immediate: true }
)
</script>

<template>
  <router-view />
</template>
