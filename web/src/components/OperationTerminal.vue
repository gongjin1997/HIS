<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useOperationSSE } from '@/api/sse'

const props = defineProps<{
  projectId: string
  opId: string
}>()

const termEl = ref<HTMLElement | null>(null)
const onlyErrors = ref(false)
let term: Terminal | null = null
let fitAddon: FitAddon | null = null

const { status, lastEventId } = useOperationSSE(props.projectId, props.opId, (frame) => {
  if (!term) return
  const line = `[#${frame.id}][${frame.event}] ${JSON.stringify(frame.data)}`
  if (onlyErrors.value && !line.toLowerCase().includes('error') && !line.toLowerCase().includes('fail')) return
  const color = frame.event.includes('fail') || frame.event.includes('error') ? '\x1b[31m' : frame.event === 'operation.done' ? '\x1b[32m' : '\x1b[36m'
  term.writeln(`${color}${line}\x1b[0m`)
})

onMounted(() => {
  if (!termEl.value) return
  term = new Terminal({
    fontFamily: '"JetBrains Mono", ui-monospace, monospace',
    fontSize: 12.5,
    lineHeight: 1.45,
    cursorBlink: false,
    disableStdin: true,
    theme: {
      background: '#080c12',
      foreground: '#e2e8f0',
      cursor: '#60a5fa',
      red: '#f87171',
      green: '#34d399',
      yellow: '#fbbf24',
      blue: '#60a5fa',
      cyan: '#22d3ee',
    },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termEl.value)
  fitAddon.fit()
  term.writeln(`\x1b[90m--- kinstall SSE live stream attached to ${props.opId} ---\x1b[0m`)
})

onUnmounted(() => {
  term?.dispose()
})
</script>

<template>
  <div class="ki-surface" style="overflow: hidden">
    <div class="ki-flex-between" style="padding: 8px 12px; background: var(--ki-bg-subtle); border-bottom: 1px solid var(--ki-border-subtle)">
      <div class="ki-flex-row">
        <span style="font-weight: 600">实时执行终端 (@xterm/xterm)</span>
        <span class="ki-badge ki-mono" style="background: var(--ki-status-ok-subtle); color: var(--ki-status-ok-text)">
          SSE: {{ status }} #{{ lastEventId }}
        </span>
      </div>
      <el-checkbox v-model="onlyErrors">仅看错误</el-checkbox>
    </div>
    <div ref="termEl" style="height: 360px; background: var(--ki-bg-terminal); padding: 8px" />
  </div>
</template>
