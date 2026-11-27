<script setup lang="ts">
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

defineProps<{ data: StackDagNodeData }>()
</script>

<template>
  <div class="ki-dag-node ki-surface">
    <Handle type="target" :position="Position.Left" class="ki-dag-handle" />

    <header class="ki-dag-node__head">
      <div class="ki-flex-row ki-truncate">
        <Box :size="13" class="ki-dag-node__icon" aria-hidden="true" />
        <strong class="ki-mono ki-truncate">{{ data.id }}</strong>
      </div>
      <KiStatusBadge :status="data.status || 'ready'" />
    </header>

    <div class="ki-dag-node__body">
      <div class="ki-flex-between">
        <span class="ki-dag-node__prod">{{ data.product }} {{ data.version }}</span>
        <KiDomainTag kind="target" :value="data.target" />
      </div>

      <div v-if="data.outputs && data.outputs.length" class="ki-dag-ports">
        <div v-for="out in data.outputs" :key="out.name" class="ki-dag-port">
          <Lock v-if="out.secret" :size="11" class="ki-text-warn" aria-hidden="true" />
          <span class="ki-mono">{{ out.name }}</span>
        </div>
      </div>
    </div>

    <footer class="ki-dag-node__foot ki-muted">
      <span>上游依赖 {{ data.dependsOnCount ?? 0 }}</span>
      <span>Output {{ data.outputs?.length ?? 0 }}</span>
    </footer>

    <Handle type="source" :position="Position.Right" class="ki-dag-handle" />
  </div>
</template>

<style scoped>
.ki-dag-node {
  width: 248px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  overflow: hidden;
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
  padding: var(--ki-space-2) var(--ki-space-3);
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-2);
}

.ki-dag-node__prod {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-secondary);
}

.ki-dag-ports {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ki-space-1);
}

.ki-dag-port {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  font-size: 11px;
  color: var(--ki-text-secondary);
}

.ki-dag-node__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 26px;
  padding: 0 var(--ki-space-3);
  border-top: 1px solid var(--ki-border-subtle);
  font-size: 11px;
}

.ki-dag-handle {
  width: 9px;
  height: 9px;
  background-color: var(--ki-brand);
  border: 2px solid var(--ki-bg-surface);
}
</style>
