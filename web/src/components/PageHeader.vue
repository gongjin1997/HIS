<script lang="ts">
import type { InjectionKey, ShallowRef } from 'vue'

export const pageHeaderTargetKey: InjectionKey<Readonly<ShallowRef<HTMLElement | null>>> = Symbol(
  'kinstall-page-refresh-target'
)
</script>

<script setup lang="ts">
import { inject, ref, shallowRef } from 'vue'
import { RefreshCw } from 'lucide-vue-next'

const props = defineProps<{
  title?: string
  refresh?: () => unknown | Promise<unknown>
  refreshing?: boolean
}>()

const target = inject(pageHeaderTargetKey, shallowRef(null))
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
  <Teleport v-if="target && refresh" :to="target">
    <el-tooltip content="刷新当前页面数据" placement="bottom">
      <el-button
        text
        circle
        aria-label="刷新当前页面"
        :loading="refreshing || running"
        @click="runRefresh"
      >
        <RefreshCw v-if="!(refreshing || running)" :size="15" />
      </el-button>
    </el-tooltip>
  </Teleport>
</template>
