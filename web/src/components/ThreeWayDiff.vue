<script setup lang="ts">
import KiThreeWayDiffViewer from './domain/KiThreeWayDiffViewer.vue'

defineProps<{
  instanceName: string
  path: string
  baseText: string
  liveText: string
  desiredText: string
  hasDrift: boolean
}>()

defineEmits<{ (e: 'decision', mode: 'abort' | 'adopt' | 'force-override'): void }>()
</script>

<template>
  <KiThreeWayDiffViewer
    :instance-name="instanceName"
    :path="path"
    :base-text="baseText"
    :live-text="liveText"
    :desired-text="desiredText"
    :has-drift="hasDrift"
    @decision="$emit('decision', $event)"
  />
</template>
