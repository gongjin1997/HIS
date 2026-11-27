<script setup lang="ts">
import { computed } from 'vue'
import { VueFlow } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { useWorkspaceStore } from '@/stores/workspace'
import { detectDagCycle } from '@/utils/dag'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiStackDagNode, { type StackDagNodeData } from '@/components/domain/KiStackDagNode.vue'

export interface StackNodeInput {
  id: string
  product: string
  version: string
  target: string
  dependsOn: string[]
  status?: string
  outputs?: Array<{ name: string; secret?: boolean }>
}

const props = defineProps<{
  instances: StackNodeInput[]
}>()

const ws = useWorkspaceStore()

const cycleInfo = computed(() =>
  detectDagCycle(props.instances.map((it) => ({ id: it.id, dependsOn: it.dependsOn || [] })))
)

const nodes = computed(() => {
  const depthMap = new Map<string, number>()
  const computeDepth = (id: string, visiting = new Set<string>()): number => {
    if (depthMap.has(id)) return depthMap.get(id)!
    if (visiting.has(id)) return 0
    visiting.add(id)
    const item = props.instances.find((x) => x.id === id)
    if (!item || !item.dependsOn.length) {
      depthMap.set(id, 0)
      return 0
    }
    const d = 1 + Math.max(...item.dependsOn.map((dep) => computeDepth(dep, visiting)))
    depthMap.set(id, d)
    return d
  }
  props.instances.forEach((it) => computeDepth(it.id))

  const colCount = new Map<number, number>()
  return props.instances.map((it) => {
    const col = depthMap.get(it.id) || 0
    const row = colCount.get(col) || 0
    colCount.set(col, row + 1)
    return {
      id: it.id,
      type: 'kiStackNode',
      position: { x: 36 + col * 300, y: 36 + row * 148 },
      data: {
        id: it.id,
        product: it.product,
        version: it.version,
        target: it.target,
        status: it.status || 'ready',
        dependsOnCount: (it.dependsOn || []).length,
        outputs: it.outputs || [{ name: 'endpoint' }, { name: 'rootPassword', secret: true }],
      },
    }
  })
})

const edges = computed(() => {
  const out: Array<{ id: string; source: string; target: string; animated: boolean }> = []
  for (const it of props.instances) {
    for (const dep of it.dependsOn || []) {
      out.push({
        id: `${dep}->${it.id}`,
        source: dep,
        target: it.id,
        animated: it.status === 'running',
      })
    }
  }
  return out
})
</script>

<template>
  <div class="ki-dag-wrap">
    <KiBanner
      v-if="cycleInfo.hasCycle"
      variant="danger"
      :text="`检测到 Stack DAG 循环依赖：${cycleInfo.cyclePath.join(' → ')}，已阻断部署计划生成`"
      class="ki-mb-3"
    />
    <div class="ki-dag-canvas ki-surface">
      <VueFlow
        :nodes="nodes"
        :edges="edges"
        :color-mode="ws.theme"
        fit-view-on-init
      >
        <template #node-kiStackNode="nodeProps">
          <KiStackDagNode :data="(nodeProps.data as StackDagNodeData)" />
        </template>
      </VueFlow>
    </div>
  </div>
</template>

<style scoped>
.ki-dag-canvas {
  height: 440px;
  width: 100%;
  overflow: hidden;
  background-color: var(--ki-bg-canvas);
}
</style>
