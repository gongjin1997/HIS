<script lang="ts">
import type { InjectionKey, ShallowRef } from 'vue'

export const pageHeaderTargetKey: InjectionKey<Readonly<ShallowRef<HTMLElement | null>>> = Symbol(
  'kinstall-page-refresh-target'
)
</script>

<script setup lang="ts">
import { ref } from 'vue'
import { RefreshCw } from 'lucide-vue-next'

const props = defineProps<{
  title?: string
  subtitle?: string
  refresh?: () => unknown | Promise<unknown>
  refreshing?: boolean
}>()

const running = ref(false)

async function runRefresh() {
  if (running.value || props.refreshing || !props.refresh) return
  running.value = true
  try {
    await props.refresh()
  } finally {
    running.value = false
  }
}
</script>

<template>
  <header class="ki-page-bar">
    <div class="ki-page-bar__left">
      <div class="ki-page-bar__title-row">
        <h1 v-if="title" class="ki-page-bar__title">{{ title }}</h1>
        <slot name="meta" />
      </div>
      <p v-if="subtitle" class="ki-page-bar__sub">{{ subtitle }}</p>
    </div>
    <div class="ki-page-bar__actions">
      <slot name="actions" />
      <el-button
        v-if="refresh"
        :loading="refreshing || running"
        aria-label="刷新当前页面"
        @click="runRefresh"
      >
        <RefreshCw v-if="!(refreshing || running)" :size="14" class="ki-btn-ico" />
        刷新
      </el-button>
    </div>
  </header>
</template>

<style scoped>
.ki-btn-ico {
  margin-right: 4px;
}
</style>
