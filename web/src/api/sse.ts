import { ref, onUnmounted } from 'vue'

export interface OperationEventFrame {
  id: number
  event: string
  data: Record<string, unknown>
}

/**
 * Lossless SSE client over native fetch ReadableStream (WEB_DESIGN §4.3 A, G1 §3.2):
 * - Supports Authorization Bearer header + Same-Origin Cookie
 * - Strictly monotonic Last-Event-ID deduplication & automatic backoff reconnect
 * - Handles terminal `event: operation.done` WITHOUT `id:` line cleanly (never loops/drops)
 * - 45s activity watchdog aborts half-open proxy connections
 */
export function useOperationSSE(
  projectId: string,
  opId: string,
  onFrame: (f: OperationEventFrame) => void
) {
  const status = ref<'connecting' | 'open' | 'reconnecting' | 'closed' | 'error'>('connecting')
  const lastEventId = ref<number>(0)

  let abortCtrl: AbortController | null = null
  let stopped = false
  let lastActivityAt = Date.now()
  let watchdogTimer: number | null = null
  let reconnectTimer: number | null = null

  const parseAndDispatch = (rawBlock: string) => {
    lastActivityAt = Date.now()
    if (!rawBlock.trim() || rawBlock.startsWith(':')) return
    let id = 0
    let event = 'message'
    const dataLines: string[] = []
    for (const line of rawBlock.split('\n')) {
      if (line.startsWith('id:')) id = Number(line.slice(3).trim()) || 0
      else if (line.startsWith('event:')) event = line.slice(6).trim()
      else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart())
    }
    if (event !== 'operation.done' && id > 0 && id <= lastEventId.value) return
    if (id > 0) lastEventId.value = id
    const payload = dataLines.join('\n')
    try {
      const parsed = payload ? (JSON.parse(payload) as Record<string, unknown>) : {}
      onFrame({ id, event, data: parsed })
      if (event === 'operation.done') {
        stopped = true
        status.value = 'closed'
        abortCtrl?.abort()
      }
    } catch {
      onFrame({ id, event, data: { raw: payload } })
    }
  }

  const connect = async (attempt = 0) => {
    if (stopped) return
    status.value = attempt === 0 ? 'connecting' : 'reconnecting'
    abortCtrl = new AbortController()
    lastActivityAt = Date.now()

    const headers: Record<string, string> = { Accept: 'text/event-stream' }
    const token = localStorage.getItem('kinstall.token') || ''
    if (token) headers.Authorization = `Bearer ${token}`
    if (lastEventId.value > 0) headers['Last-Event-ID'] = String(lastEventId.value)

    try {
      const url = `/api/v1/projects/${encodeURIComponent(projectId)}/operations/${encodeURIComponent(opId)}/events`
      const resp = await fetch(url, {
        method: 'GET',
        credentials: 'same-origin',
        headers,
        signal: abortCtrl.signal,
      })
      if (!resp.ok || !resp.body) throw new Error(`SSE HTTP ${resp.status}`)
      status.value = 'open'

      const reader = resp.body.getReader()
      const decoder = new TextDecoder('utf-8')
      let buf = ''
      while (!stopped) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n')
        const parts = buf.split('\n\n')
        buf = parts.pop() ?? ''
        for (const block of parts) parseAndDispatch(block)
      }
    } catch {
      // retry via backoff
    }
    if (!stopped) {
      const delayMs = Math.min(1000 * 2 ** attempt, 10000)
      reconnectTimer = window.setTimeout(() => void connect(attempt + 1), delayMs)
    }
  }

  watchdogTimer = window.setInterval(() => {
    if (!stopped && status.value === 'open' && Date.now() - lastActivityAt > 45_000) {
      abortCtrl?.abort()
    }
  }, 10_000)

  void connect(0)

  const close = () => {
    stopped = true
    status.value = 'closed'
    if (watchdogTimer) clearInterval(watchdogTimer)
    if (reconnectTimer) clearTimeout(reconnectTimer)
    abortCtrl?.abort()
  }
  onUnmounted(close)

  return { status, lastEventId, close }
}
