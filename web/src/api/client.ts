export interface ErrorBody {
  code?: string
  message?: string
  detail?: string
  traceId?: string
  title?: string
}

export class APIError extends Error {
  status: number
  code: string
  traceId: string
  detail: string

  constructor(status: number, body: ErrorBody) {
    super(body.message || body.title || body.detail || `HTTP ${status}`)
    this.status = status
    this.code = body.code || 'API_ERROR'
    this.traceId = body.traceId || ''
    this.detail = body.detail || ''
  }
}

export interface SSELogLine {
  seq: number
  timestamp: string
  stream: string
  level: string
  message: string
  done?: boolean
  status?: string
}

function baseHeaders(extra?: Record<string, string>): Record<string, string> {
  const token = localStorage.getItem('kinstall.token') || ''
  const wsId = localStorage.getItem('kinstall.workspaceId') || ''
  const headers: Record<string, string> = {
    ...(extra || {}),
  }
  if (token) headers.Authorization = `Bearer ${token}`
  if (wsId) headers['X-Workspace-Id'] = wsId
  return headers
}

export async function apiRequest<T>(
  method: string,
  path: string,
  opts?: { body?: unknown; headers?: Record<string, string> }
): Promise<{ data: T }> {
  const headers = baseHeaders({ Accept: 'application/json', ...(opts?.headers || {}) })
  if (opts?.body !== undefined) headers['Content-Type'] = 'application/json'

  const resp = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: opts?.body !== undefined ? JSON.stringify(opts.body) : undefined,
  })

  if (resp.status === 204) {
    return { data: undefined as unknown as T }
  }
  const text = await resp.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = { message: text }
  }
  if (!resp.ok) {
    throw new APIError(resp.status, (parsed as ErrorBody) || { message: resp.statusText })
  }
  return { data: parsed as T }
}

export async function apiUpload<T>(path: string, formData: FormData): Promise<T> {
  const resp = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: baseHeaders({ Accept: 'application/json' }),
    body: formData,
  })
  const text = await resp.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = { message: text }
  }
  if (!resp.ok) {
    throw new APIError(resp.status, (parsed as ErrorBody) || { message: resp.statusText })
  }
  return parsed as T
}

export async function apiDownload(path: string, defaultFilename: string): Promise<void> {
  const resp = await fetch(path, {
    method: 'GET',
    credentials: 'same-origin',
    headers: baseHeaders(),
  })
  if (!resp.ok) {
    const text = await resp.text()
    let parsed: ErrorBody = { message: resp.statusText, detail: text }
    try {
      parsed = JSON.parse(text) as ErrorBody
    } catch {
      // keep raw text
    }
    throw new APIError(resp.status, parsed)
  }
  const blob = await resp.blob()
  const cd = resp.headers.get('Content-Disposition') || ''
  const m = cd.match(/filename="?([^";]+)"?/i)
  const filename = m?.[1] || defaultFilename
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function openSSEStream(
  topic: string,
  onLine: (line: SSELogLine) => void,
  onEnd?: () => void
): () => void {
  const token = localStorage.getItem('kinstall.token') || ''
  const url = `/api/v1/stream/${encodeURIComponent(topic)}?token=${encodeURIComponent(token)}`
  const es = new EventSource(url)
  es.addEventListener('log', (ev) => {
    try {
      const parsed = JSON.parse((ev as MessageEvent).data) as SSELogLine
      onLine(parsed)
      if (parsed.done) {
        es.close()
        onEnd?.()
      }
    } catch {
      // ignore malformed frame
    }
  })
  es.onerror = () => {
    es.close()
    onEnd?.()
  }
  return () => es.close()
}
