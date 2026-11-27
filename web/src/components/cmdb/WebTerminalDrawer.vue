<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { RefreshCw, Trash2, Terminal as TerminalIcon } from 'lucide-vue-next'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

const props = defineProps<{
  modelValue: boolean
  projectId: string
  hostId: string
  hostName: string
  hostAddress: string
}>()

const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const termWrap = ref<HTMLElement | null>(null)
const connState = ref<'connecting' | 'open' | 'closed' | 'error'>('connecting')

let term: Terminal | null = null
let fitAddon: FitAddon | null = null
let ws: WebSocket | null = null
let resizeObs: ResizeObserver | null = null

const quickSnippets = [
  { label: '系统资源 (uptime + free)', cmd: 'uptime && free -h\n' },
  { label: '磁盘空间 (df -h)', cmd: 'df -hT -x tmpfs -x devtmpfs\n' },
  { label: '监听端口 (ss -lntup)', cmd: 'ss -lntup | head -n 40\n' },
  { label: 'Docker 容器 (docker ps)', cmd: 'docker ps -a\n' },
  { label: '异常服务 (systemctl --failed)', cmd: 'systemctl --failed --no-pager\n' },
]

function cleanup() {
  if (resizeObs) {
    resizeObs.disconnect()
    resizeObs = null
  }
  if (ws) {
    ws.close()
    ws = null
  }
  if (term) {
    term.dispose()
    term = null
  }
  fitAddon = null
}

function sendInput(data: string) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'input', data }))
  }
}

function sendResize() {
  if (!fitAddon || !term) return
  fitAddon.fit()
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
  }
}

function connect() {
  cleanup()
  if (!termWrap.value || !props.projectId || !props.hostId) return

  connState.value = 'connecting'
  term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: 'JetBrains Mono, Menlo, Consolas, monospace',
    scrollback: 5000,
    theme: {
      background: '#0F172A',
      foreground: '#E2E8F0',
      cursor: '#38BDF8',
    },
  })
  fitAddon = new FitAddon()
  term.loadAddon(fitAddon)
  term.open(termWrap.value)
  fitAddon.fit()

  term.writeln(`\x1b[36m[kinstall WebSSH]\x1b[0m 正在连接 ${props.hostName} (${props.hostAddress}) …`)

  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const url = `${proto}//${window.location.host}/api/v1/projects/${props.projectId}/hosts/${props.hostId}/terminal?cols=${term.cols}&rows=${term.rows}`
  ws = new WebSocket(url)

  ws.onopen = () => {
    connState.value = 'open'
    sendResize()
    term?.focus()
  }
  ws.onmessage = (ev) => {
    term?.write(typeof ev.data === 'string' ? ev.data : '')
  }
  ws.onerror = () => {
    connState.value = 'error'
    term?.writeln('\r\n\x1b[31m[WebSSH] 连接发生错误，请检查主机 SSH 连通性与凭据配置。\x1b[0m')
  }
  ws.onclose = () => {
    if (connState.value !== 'error') connState.value = 'closed'
    term?.writeln('\r\n\x1b[33m[WebSSH] 会话已断开。\x1b[0m')
  }

  term.onData((data) => sendInput(data))

  resizeObs = new ResizeObserver(() => sendResize())
  resizeObs.observe(termWrap.value)
}

watch(
  () => [props.modelValue, props.hostId],
  async ([open]) => {
    if (open) {
      await nextTick()
      connect()
    } else {
      cleanup()
    }
  }
)

onBeforeUnmount(cleanup)
</script>

<template>
  <el-drawer
    :model-value="modelValue"
    size="78%"
    direction="rtl"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <template #header>
      <div class="term-hdr">
        <div class="ki-flex-row">
          <TerminalIcon :size="16" class="ki-text-brand" />
          <strong>Web SSH 交互终端 · {{ hostName }}</strong>
          <span class="ki-mono ki-muted">{{ hostAddress }}</span>
          <KiStatusBadge
            :status="connState === 'open' ? 'ok' : connState === 'connecting' ? 'running' : connState === 'error' ? 'failed' : 'cancelled'"
            :label="connState === 'open' ? '已连接' : connState === 'connecting' ? '连接中' : connState === 'error' ? '连接失败' : '已断开'"
          />
        </div>
        <div class="ki-flex-row">
          <el-dropdown trigger="click" @command="(c: string) => sendInput(c)">
            <el-button size="small">快捷运维命令</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-for="s in quickSnippets" :key="s.label" :command="s.cmd">
                  {{ s.label }}
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button size="small" @click="term?.clear()">
            <Trash2 :size="12" class="btn-ico" /> 清屏
          </el-button>
          <el-button size="small" type="primary" plain @click="connect">
            <RefreshCw :size="12" class="btn-ico" /> 重连
          </el-button>
        </div>
      </div>
    </template>

    <div class="term-body">
      <div ref="termWrap" class="term-canvas" />
      <div class="term-foot ki-muted">
        所有交互会话的开启与关闭时间均自动记入 SHA-256 + 国密 SM3 防篡改审计链。支持 Ctrl+C、Tab 补全与窗口自适应缩放。
      </div>
    </div>
  </el-drawer>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.term-hdr {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  padding-right: 12px;
}

.term-body {
  display: flex;
  flex-direction: column;
  height: calc(100vh - 110px);
  gap: 10px;
}

.term-canvas {
  flex: 1;
  min-height: 0;
  border-radius: var(--ki-radius-md);
  overflow: hidden;
  padding: 8px;
  background-color: var(--ki-bg-terminal);
  border: 1px solid var(--ki-border-default);
}

.term-foot {
  font-size: var(--ki-fs-xs);
}
</style>
