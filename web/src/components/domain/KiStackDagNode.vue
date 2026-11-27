<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@xyflow/vue'
import { Lock, Box } from 'lucide-vue-next'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'

export interface StackDagNodeData {
  id: string
  product: string
  version: string
  target: string
  status?: string
  outputs?: Array<{ name: string; secret?: boolean }>
  dependsOnCount?: number
}

const props = defineProps<{ data: StackDagNodeData }>()

const visibleOutputs = computed(() => (props.data.outputs || []).slice(0, 4))
const extraOutputCount = computed(() => Math.max(0, (props.data.outputs || []).length - 4))
</script>

<template>
  <div class="ki-dag-node ki-surface">
    <Handle type="target" :position="Position.Left" class="ki-dag-handle" />

    <header class="ki-dag-node__head">
      <div class="ki-flex-row ki-truncate">
        <Box :size="14" class="ki-dag-node__icon" aria-hidden="true" />
        <strong class="ki-mono ki-truncate">{{ data.id }}</strong>
      </div>
      <KiStatusBadge :status="data.status || 'draft'" />
    </header>

    <div class="ki-dag-node__body">
      <div class="ki-dag-meta">
        <span class="ki-dag-node__prod ki-mono">{{ data.product }} {{ data.version }}</span>
        <KiDomainTag kind="target" :value="data.target" />
      </div>

      <div v-if="visibleOutputs.length" class="ki-dag-ports">
        <div v-for="out in visibleOutputs" :key="out.name" class="ki-dag-port">
          <Lock v-if="out.secret" :size="10" class="ki-text-warn" aria-hidden="true" />
          <span class="ki-mono">{{ out.name }}</span>
        </div>
        <span v-if="extraOutputCount > 0" class="ki-dag-port ki-dag-port--more ki-mono">+{{ extraOutputCount }}</span>
      </div>
    </div>

    <footer class="ki-dag-node__foot ki-muted">
      <span>上游依赖 {{ data.dependsOnCount ?? 0 }}</span>
      <span>输出项 {{ data.outputs?.length ?? 0 }}</span>
    </footer>

    <Handle type="source" :position="Position.Right" class="ki-dag-handle" />
  </div>
</template>

<style scoped>
.ki-dag-node {
  width: 286px;
  border-radius: var(--ki-radius-card);
  border: 1px solid var(--ki-border-default);
  border-top: 3px solid var(--ki-brand);
  background-color: var(--ki-bg-surface);
  box-shadow: var(--ki-shadow-surface);
  overflow: hidden;
  transition: box-shadow var(--ki-dur-fast) var(--ki-ease), border-color var(--ki-dur-fast) var(--ki-ease);
}

.ki-dag-node:hover {
  box-shadow: var(--ki-shadow-hover);
  border-color: var(--ki-brand-border);
  border-top-color: var(--ki-brand);
}

.ki-dag-node__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-2);
  height: 36px;
  padding: 0 var(--ki-space-3);
  background-color: var(--ki-bg-subtle);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-dag-node__icon {
  color: var(--ki-brand);
  flex: none;
}

.ki-dag-node__body {
  padding: 10px var(--ki-space-3);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ki-dag-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 6px;
}

.ki-dag-node__prod {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-secondary);
}

.ki-dag-ports {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.ki-dag-port {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  font-size: 11px;
  color: var(--ki-text-secondary);
}

.ki-dag-port--more {
  color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
  border-color: var(--ki-brand-border);
}

.ki-dag-node__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 28px;
  padding: 0 var(--ki-space-3);
  border-top: 1px solid var(--ki-border-subtle);
  font-size: 11px;
}

.ki-dag-handle {
  width: 10px;
  height: 10px;
  background-color: var(--ki-brand);
  border: 2px solid var(--ki-bg-surface);
}
</style>
