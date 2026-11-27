<script setup lang="ts">
import type { ProblemDetail } from '@/api/client'
import KiOperationTerminal from './domain/KiOperationTerminal.vue'

defineProps<{
  projectId: string
  opId: string
  canResume?: boolean
  problem?: ProblemDetail | null
}>()

defineEmits<{ (e: 'resume'): void }>()
</script>

<template>
  <KiOperationTerminal
    :project-id="projectId"
    :op-id="opId"
    :can-resume="canResume"
    :problem="problem"
    @resume="$emit('resume')"
  />
</template>
