<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import '@xterm/xterm/css/xterm.css'
import { Search, Download, RotateCcw, ChevronUp, ChevronDown, Server, CheckCircle2, XCircle, CircleDot } from 'lucide-vue-next'
import { useOperationSSE } from '@/api/sse'
import { getXtermTheme } from '@/composables/useTheme'
import { useWorkspaceStore } from '@/stores/workspace'
import type { ProblemDetail } from '@/api/client'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiButton from '@/components/ki/KiButton.vue'
import KiProblemBanner from './KiProblemBanner.vue'

interface PhaseNode {
  key: string
  phase: string
  host: string
  state: 'running' | 'ok' | 'failed'
  lines: number
}

const props = withDefaults(
  defineProps<{
    projectId: string
    opId: string
    canResume?: boolean
    problem?: ProblemDetail | null
  }>(),
  {
    canResume: false,
    problem: null,
  }
)

const emit = defineEmits<{ (e: 'resume'): void }>()

const ws = useWorkspaceStore()
const termEl = ref<HTMLElement | null>(null)
const onlyErrors = ref(false)
const searchQuery = ref('')
const selectedHost = ref<string>('ALL')
const rawLines = ref<Array<{ host: string; phase: string; text: string; isError: boolean }>>([])
const phases = ref<Map<string, PhaseNode>>(new Map())

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let searchAddon: SearchAddon | null = null
let resizeObs: ResizeObserver | null = null

const phaseList = computed(() => Array.from(phases.value.values()))
const hostList = computed(() => {
  const s = new Set<string>()
  for (const p of phases.value.values()) s.add(p.host)
  return Array.from(s)
})

function writeFormatted(line: { text: string; isError: boolean }, eventName = '') {
  if (!term) return
  if (onlyErrors.value && !line.isError) return
  if (selectedHost.value !== 'ALL' && !line.text.includes(selectedHost.value)) return
  const color = line.isError ? '\x1b[31m' : eventName === 'operation.done' ? '\x1b[32m' : '\x1b[36m'
  term.writeln(`${color}${line.text}\x1b[0m`)
}

function rerenderTerminal() {
  if (!term) return
  term.clear()
  term.writeln(`\x1b[90m--- kinstall SSE live stream (${props.opId}) ---\x1b[0m`)
  for (const l of rawLines.value) {
    if (selectedHost.value !== 'ALL' && l.host !== selectedHost.value) continue
    if (onlyErrors.value && !l.isError) continue
    const color = l.isError ? '\x1b[31m' : '\x1b[36m'
    term.writeln(`${color}${l.text}\x1b[0m`)
  }
}

const { status, lastEventId } = useOperationSSE(props.projectId, props.opId, (frame) => {
  const host = String(frame.data.host || frame.data.hostId || 'control-plane')
  const phase = String(frame.data.phase || frame.data.step || frame.event || 'exec')
  const key = `${phase}@${host}`
  const isErr = frame.event.includes('fail') || frame.event.includes('error') || frame.data.stream === 'stderr' || frame.data.state === 'failed'
  const text =
    frame.event === 'task.log' && typeof frame.data.line === 'string'
      ? `[#${frame.id}][${phase}][${host}] ${frame.data.line}`
      : `[#${frame.id}][${phase}][${host}] ${JSON.stringify(frame.data)}`

  const prev = phases.value.get(key)
  const nextState: PhaseNode['state'] = isErr
    ? 'failed'
    : frame.event.includes('done') || frame.data.state === 'succeeded' || frame.data.state === 'skipped'
      ? 'ok'
      : 'running'
  phases.value.set(key, {
    key,
    phase,
    host,
    state: nextState,
    lines: (prev?.lines || 0) + 1,
  })
  phases.value = new Map(phases.value)

  const entry = { host, phase, text, isError: isErr }
  rawLines.value.push(entry)
  writeFormatted(entry, frame.event)
})

watch([onlyErrors, selectedHost], rerenderTerminal)
watch(
  () => ws.theme,
  () => {
    if (term) term.options.theme = getXtermTheme()
  }
)

function findNext() {
  if (searchQuery.value) searchAddon?.findNext(searchQuery.value)
}

function findPrev() {
  if (searchQuery.value) searchAddon?.findPrevious(searchQuery.value)
}

function downloadLog() {
  const blob = new Blob([rawLines.value.map((l) => l.text).join('\n')], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${props.opId}.log`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(() => {
  if (!termEl.value) return
  term = new Terminal({
    fontFamily: '"JetBrains Mono", ui-monospace, monospace',
    fontSize: 12.5,
    lineHeight: 1.48,
    cursorBlink: false,
    disableStdin: true,
    theme: getXtermTheme(),
  })
  fitAddon = new FitAddon()
  searchAddon = new SearchAddon()
  term.loadAddon(fitAddon)
  term.loadAddon(searchAddon)
  term.open(termEl.value)
  fitAddon.fit()
  term.writeln(`\x1b[90m--- kinstall SSE live stream attached to ${props.opId} ---\x1b[0m`)
  resizeObs = new ResizeObserver(() => fitAddon?.fit())
  resizeObs.observe(termEl.value)
})

onUnmounted(() => {
  resizeObs?.disconnect()
  term?.dispose()
})
</script>

<template>
  <div class="ki-term-shell ki-surface">
    <KiProblemBanner v-if="problem" :problem="problem" compact class="ki-term-problem">
      <template v-if="canResume" #actions>
        <KiButton variant="primary" size="sm" @click="emit('resume')">
          <RotateCcw :size="13" /> 从断点续跑 (--resume)
        </KiButton>
      </template>
    </KiProblemBanner>

    <div class="ki-term-bar">
      <div class="ki-flex-row">
        <strong>实时执行终端</strong>
        <KiStatusBadge :status="status === 'open' ? 'running' : status === 'closed' ? 'ok' : 'warn'" :label="`SSE ${status} #${lastEventId}`" />
      </div>

      <div class="ki-flex-row ki-flex-wrap">
        <div class="ki-term-search">
          <Search :size="13" class="ki-muted" />
          <input
            v-model="searchQuery"
            type="text"
            class="ki-term-search__input"
            placeholder="搜索日志…"
            @keyup.enter="findNext"
          />
          <button type="button" class="ki-term-search__btn" aria-label="上一处" @click="findPrev">
            <ChevronUp :size="13" />
          </button>
          <button type="button" class="ki-term-search__btn" aria-label="下一处" @click="findNext">
            <ChevronDown :size="13" />
          </button>
        </div>

        <el-select v-model="selectedHost" size="small" class="ki-term-host-sel">
          <el-option label="全部主机" value="ALL" />
          <el-option v-for="h in hostList" :key="h" :label="h" :value="h" />
        </el-select>

        <el-checkbox v-model="onlyErrors" size="small">仅看错误</el-checkbox>

        <KiButton variant="secondary" size="sm" @click="downloadLog">
          <Download :size="13" /> 下载日志
        </KiButton>
      </div>
    </div>

    <div class="ki-term-body">
      <!-- 280px Phase / Host Execution Tree -->
      <aside class="ki-term-tree">
        <div class="ki-term-tree__head">Phase / Host 执行树</div>
        <button
          type="button"
          class="ki-tree-item"
          :class="{ 'is-active': selectedHost === 'ALL' }"
          @click="selectedHost = 'ALL'"
        >
          <Server :size="13" />
          <span>全部节点日志</span>
          <em class="ki-num ki-muted">{{ rawLines.length }}</em>
        </button>
        <button
          v-for="node in phaseList"
          :key="node.key"
          type="button"
          class="ki-tree-item"
          :class="{ 'is-active': selectedHost === node.host }"
          @click="selectedHost = node.host"
        >
          <CheckCircle2 v-if="node.state === 'ok'" :size="13" class="ki-text-ok" />
          <XCircle v-else-if="node.state === 'failed'" :size="13" class="ki-text-danger" />
          <CircleDot v-else :size="13" class="ki-dot-pulse" />
          <div class="ki-tree-item__text">
            <b>{{ node.phase }}</b>
            <span class="ki-mono ki-muted">{{ node.host }}</span>
          </div>
          <em class="ki-num ki-muted">{{ node.lines }}</em>
        </button>
        <div v-if="phaseList.length === 0" class="ki-tree-empty ki-muted">等待事件流帧到达…</div>
      </aside>

      <div ref="termEl" class="ki-term-canvas" />
    </div>
  </div>
</template>

<style scoped>
.ki-term-shell {
  overflow: hidden;
}

.ki-term-problem {
  margin: var(--ki-space-3);
}

.ki-term-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  padding: var(--ki-space-2) var(--ki-space-3);
  background-color: var(--ki-bg-subtle);
  border-bottom: 1px solid var(--ki-border-subtle);
  flex-wrap: wrap;
}

.ki-term-search {
  display: inline-flex;
  align-items: center;
  gap: var(--ki-space-1);
  height: var(--ki-control-h-sm);
  padding: 0 var(--ki-space-2);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-surface);
}

.ki-term-search__input {
  width: 120px;
  border: 0;
  background-color: transparent;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-xs);
}

.ki-term-search__btn {
  display: inline-flex;
  padding: 2px;
  border: 0;
  background-color: transparent;
  color: var(--ki-text-secondary);
  cursor: pointer;
}

.ki-term-host-sel {
  width: 130px;
}

.ki-term-body {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  min-height: 380px;
}

.ki-term-tree {
  border-right: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-surface);
  overflow-y: auto;
  max-height: 420px;
  padding: var(--ki-space-2);
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-1);
}

.ki-term-tree__head {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
  padding: var(--ki-space-1) var(--ki-space-2);
}

.ki-tree-item {
  display: flex;
  align-items: center;
  gap: var(--ki-space-2);
  width: 100%;
  padding: 6px 8px;
  border: 0;
  border-radius: var(--ki-radius-sm);
  background-color: transparent;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-xs);
  text-align: left;
  cursor: pointer;
}

.ki-tree-item:hover {
  background-color: var(--ki-bg-subtle);
}

.ki-tree-item.is-active {
  background-color: var(--ki-sider-active-bg);
  color: var(--ki-brand);
}

.ki-tree-item__text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.ki-tree-item__text b,
.ki-tree-item__text span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-tree-item em {
  font-style: normal;
  font-size: 11px;
}

.ki-tree-empty {
  padding: var(--ki-space-4) var(--ki-space-2);
  font-size: var(--ki-fs-xs);
  text-align: center;
}

.ki-term-canvas {
  height: 420px;
  background-color: var(--ki-bg-terminal);
  padding: var(--ki-space-2);
}

@media (max-width: 900px) {
  .ki-term-body {
    grid-template-columns: minmax(0, 1fr);
  }

  .ki-term-tree {
    max-height: 150px;
    border-right: 0;
    border-bottom: 1px solid var(--ki-border-subtle);
  }
}
</style>
