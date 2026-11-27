<script setup lang="ts">
import { computed } from 'vue'
import { Background, Controls, MarkerType, VueFlow, type Connection, type NodeMouseEvent } from '@xyflow/vue'
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

const emit = defineEmits<{
  (e: 'connect-edge', sourceId: string, targetId: string): void
  (e: 'select-node', nodeId: string): void
}>()

const ws = useWorkspaceStore()

const COL_WIDTH = 360
const ROW_HEIGHT = 248
const PAD_X = 48
const PAD_Y = 36

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

  const colTotals = new Map<number, number>()
  for (const it of props.instances) {
    const col = depthMap.get(it.id) || 0
    colTotals.set(col, (colTotals.get(col) || 0) + 1)
  }
  const maxRows = Math.max(1, ...Array.from(colTotals.values()))

  const colRowCursor = new Map<number, number>()
  return props.instances.map((it) => {
    const col = depthMap.get(it.id) || 0
    const row = colRowCursor.get(col) || 0
    colRowCursor.set(col, row + 1)
    const colSize = colTotals.get(col) || 1
    const yOffset = ((maxRows - colSize) * ROW_HEIGHT) / 2
    return {
      id: it.id,
      type: 'kiStackNode',
      position: { x: PAD_X + col * COL_WIDTH, y: PAD_Y + yOffset + row * ROW_HEIGHT },
      data: {
        id: it.id,
        product: it.product,
        version: it.version,
        target: it.target,
        status: it.status || 'draft',
        dependsOnCount: (it.dependsOn || []).length,
        outputs: it.outputs || [{ name: 'endpoint' }, { name: 'rootPassword', secret: true }],
      },
    }
  })
})

const edges = computed(() => {
  const out: Array<{
    id: string
    source: string
    target: string
    type: string
    animated: boolean
    markerEnd: MarkerType
  }> = []
  for (const it of props.instances) {
    for (const dep of it.dependsOn || []) {
      out.push({
        id: `${dep}->${it.id}`,
        source: dep,
        target: it.id,
        type: 'smoothstep',
        animated: it.status === 'running',
        markerEnd: MarkerType.ArrowClosed,
      })
    }
  }
  return out
})

const flowKey = computed(() => props.instances.map((it) => `${it.id}:${(it.dependsOn || []).join(',')}`).join('|'))

function onConnect(conn: Connection) {
  if (conn.source && conn.target && conn.source !== conn.target) {
    emit('connect-edge', conn.source, conn.target)
  }
}

function onNodeClick(ev: NodeMouseEvent) {
  if (ev.node?.id) {
    emit('select-node', ev.node.id)
  }
}
</script>

<template>
  <div class="ki-dag-wrap">
    <KiBanner
      v-if="cycleInfo.hasCycle"
      variant="danger"
      :text="`检测到应用栈循环依赖：${cycleInfo.cyclePath.join(' → ')}，已自动阻断部署计划`"
      class="ki-mb-3"
    />
    <div class="ki-dag-canvas ki-surface">
      <VueFlow
        :key="flowKey"
        :nodes="nodes"
        :edges="edges"
        :color-mode="ws.theme"
        :min-zoom="0.45"
        :max-zoom="1.5"
        :fit-view-options="{ padding: 0.24, maxZoom: 0.9 }"
        fit-view-on-init
        @connect="onConnect"
        @node-click="onNodeClick"
      >
        <Background variant="dots" :gap="20" :size="1.2" />
        <Controls position="bottom-right" :show-interactive="false" />
        <template #node-kiStackNode="nodeProps">
          <KiStackDagNode :data="(nodeProps.data as StackDagNodeData)" />
        </template>
      </VueFlow>
    </div>
  </div>
</template>

<style scoped>
.ki-dag-canvas {
  height: 460px;
  width: 100%;
  overflow: hidden;
  background-color: var(--ki-canvas-bg);
}
</style>
