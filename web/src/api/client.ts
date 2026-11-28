// LiveOps Foundation v1 Typed HTTP & Strict SSE Client
export interface ApiResponse<T> {
  data: T
  etag: string
  status: number
  replayed?: boolean
}

export class ApiError extends Error {
  status: number
  code: string
  details?: Record<string, unknown>
  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message || code)
    this.status = status
    this.code = code
    this.details = details
  }
}

export interface ScopeHeaders {
  tenantId?: string
  appId?: string
  projectId?: string
}

let scopeProvider: () => ScopeHeaders = () => ({})
let tokenProvider: () => string = () => localStorage.getItem('liveops.token') || ''
let unauthorizedHandler: () => void = () => {}

export function configureApiClient(opts: {
  getScope?: () => ScopeHeaders
  getToken?: () => string
  onUnauthorized?: () => void
}) {
  if (opts.getScope) scopeProvider = opts.getScope
  if (opts.getToken) tokenProvider = opts.getToken
  if (opts.onUnauthorized) unauthorizedHandler = opts.onUnauthorized
}

export function newIdempotencyKey(prefix = 'ui'): string {
  const rand = Math.random().toString(36).slice(2, 10)
  return `${prefix}-${Date.now()}-${rand}`
}

export async function apiRequest<T>(
  method: string,
  path: string,
  opts: {
    body?: unknown
    ifMatch?: string | number
    idempotencyKey?: string
    headers?: Record<string, string>
    rawBody?: BodyInit
  } = {}
): Promise<ApiResponse<T>> {
  const headers: Record<string, string> = { ...(opts.headers || {}) }
  const tok = tokenProvider()
  if (tok && !headers.Authorization) {
    headers.Authorization = `Bearer ${tok}`
  }
  const sc = scopeProvider()
  if (sc.tenantId && !headers['X-Tenant-Id']) headers['X-Tenant-Id'] = sc.tenantId
  if (sc.appId && !headers['X-App-Id']) headers['X-App-Id'] = sc.appId
  if (sc.projectId && !headers['X-Project-Id']) headers['X-Project-Id'] = sc.projectId

  if (opts.ifMatch !== undefined && opts.ifMatch !== '') {
    const raw = String(opts.ifMatch).trim()
    headers['If-Match'] = raw.startsWith('"') ? raw : `"${raw}"`
  }
  if (opts.idempotencyKey) {
    headers['Idempotency-Key'] = opts.idempotencyKey
  }

  let body: BodyInit | undefined = opts.rawBody
  if (opts.body !== undefined && !body) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }

  const res = await fetch(path, {
    method,
    headers,
    body,
    credentials: 'same-origin',
  })

  if (res.status === 401 && !path.includes('/auth/login')) {
    unauthorizedHandler()
  }

  const etag = res.headers.get('ETag') || ''
  const replayed = res.headers.get('X-Idempotency-Replayed') === 'true'
  const text = await res.text()
  let parsed: unknown = {}
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = { message: text }
    }
  }

  if (!res.ok) {
    const errObj = parsed as { code?: string; message?: string; details?: Record<string, unknown> }
    throw new ApiError(res.status, errObj.code || `HTTP_${res.status}`, errObj.message || res.statusText, errObj.details)
  }

  return { data: parsed as T, etag, status: res.status, replayed }
}

export const api = {
  get: <T>(path: string, headers?: Record<string, string>) => apiRequest<T>('GET', path, { headers }),
  post: <T>(path: string, body?: unknown, opts?: { ifMatch?: string | number; idempotencyKey?: string }) =>
    apiRequest<T>('POST', path, { body, ...opts }),
  put: <T>(path: string, body?: unknown, opts?: { ifMatch?: string | number }) =>
    apiRequest<T>('PUT', path, { body, ...opts }),
  del: <T>(path: string, opts?: { ifMatch?: string | number }) => apiRequest<T>('DELETE', path, opts),
}

// Strict LiveOps U01-U09 SSE Timeline Streamer (always requires initial lastSeq from snapshot!)
export function subscribeJobTimeline(
  jobId: string,
  lastSeq: number,
  callbacks: {
    onEvent: (seq: number, eventType: string, data: Record<string, unknown>) => void
    onResetRequired?: () => void
    onError?: (err: Error) => void
  }
): () => void {
  const ctrl = new AbortController()
  const sc = scopeProvider()
  const tok = tokenProvider()
  const headers: Record<string, string> = {
    Accept: 'text/event-stream',
    'Last-Event-ID': String(Math.max(0, lastSeq)),
  }
  if (tok) headers.Authorization = `Bearer ${tok}`
  if (sc.tenantId) headers['X-Tenant-Id'] = sc.tenantId
  if (sc.appId) headers['X-App-Id'] = sc.appId

  ;(async () => {
    try {
      const res = await fetch(`/api/v1/deploy/jobs/${encodeURIComponent(jobId)}/events`, {
        headers,
        signal: ctrl.signal,
        credentials: 'same-origin',
      })
      if (res.status === 410 || res.status === 409) {
        callbacks.onResetRequired?.()
        return
      }
      if (!res.ok || !res.body) {
        callbacks.onError?.(new Error(`SSE HTTP ${res.status}`))
        return
      }
      const reader = res.body.getReader()
      const dec = new TextDecoder()
      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        const frames = buf.split('\n\n')
        buf = frames.pop() || ''
        for (const frame of frames) {
          if (!frame.trim() || frame.trim().startsWith(':')) continue
          let seq = 0
          let evType = 'message'
          let dataStr = ''
          for (const line of frame.split('\n')) {
            if (line.startsWith('id:')) seq = Number(line.slice(3).trim()) || 0
            else if (line.startsWith('event:')) evType = line.slice(6).trim()
            else if (line.startsWith('data:')) dataStr += line.slice(5).trim()
          }
          if (dataStr) {
            try {
              callbacks.onEvent(seq, evType, JSON.parse(dataStr))
            } catch {
              // ignore non-JSON frames
            }
          }
        }
      }
    } catch (e) {
      if (!ctrl.signal.aborted) {
        callbacks.onError?.(e instanceof Error ? e : new Error(String(e)))
      }
    }
  })()

  return () => ctrl.abort()
}
