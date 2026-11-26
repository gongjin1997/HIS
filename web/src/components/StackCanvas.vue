<script setup lang="ts">
import { ref, computed } from 'vue'
import { VueFlow } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { useWorkspaceStore } from '@/stores/workspace'

const ws = useWorkspaceStore()

export interface StackNodeInput {
  id: string
  product: string
  version: string
  target: string
  dependsOn: string[]
  status?: string
}

const props = defineProps<{
  instances: StackNodeInput[]
}>()

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
      position: { x: 40 + col * 270, y: 40 + row * 120 },
      data: { label: `${it.id}\n${it.product} ${it.version} [${it.target}]` },
      style: {
        background: 'var(--ki-bg-elevated)',
        color: 'var(--ki-text-primary)',
        border: '1px solid var(--ki-brand-border)',
        borderRadius: '6px',
        padding: '10px 14px',
        fontFamily: 'var(--ki-font-mono)',
        fontSize: '12px',
        whiteSpace: 'pre-line',
        width: '220px',
      },
    }
  })
})

const edges = computed(() => {
  const out: Array<{ id: string; source: string; target: string; animated: boolean; style: Record<string, string> }> = []
  for (const it of props.instances) {
    for (const dep of it.dependsOn) {
      out.push({
        id: `${dep}->${it.id}`,
        source: dep,
        target: it.id,
        animated: it.status === 'running',
        style: { stroke: 'var(--ki-brand)', strokeWidth: '1.8' },
      })
    }
  }
  return out
})

const flowKey = ref(0)
</script>

<template>
  <div class="ki-surface" style="height: 420px; width: 100%; overflow: hidden">
    <VueFlow
      :key="flowKey"
      :nodes="nodes"
      :edges="edges"
      :color-mode="ws.theme"
      fit-view-on-init
    />
  </div>
</template>
