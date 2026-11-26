export interface ProblemDetail {
  type?: string
  title?: string
  status?: number
  detail?: string
  code?: string
  traceId?: string
  suggestion?: string
}

export class APIError extends Error {
  status: number
  code: string
  traceId: string
  suggestion: string
  problem: ProblemDetail

  constructor(status: number, problem: ProblemDetail) {
    super(problem.title || problem.detail || `HTTP ${status}`)
    this.status = status
    this.code = problem.code || 'KI-API-0001'
    this.traceId = problem.traceId || ''
    this.suggestion = problem.suggestion || ''
    this.problem = problem
  }
}

function genIdempotencyKey(): string {
  const bytes = new Uint8Array(12)
  crypto.getRandomValues(bytes)
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `idem_${Date.now().toString(36)}_${hex}`
}

export async function apiRequest<T>(
  method: string,
  path: string,
  opts?: {
    body?: unknown
    headers?: Record<string, string>
    idempotent?: boolean
    etag?: string
  }
): Promise<{ data: T; etag: string }> {
  const token = localStorage.getItem('kinstall.token') || ''
  const lang = localStorage.getItem('kinstall.lang') || 'zh-CN'
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Accept-Language': lang,
    ...(opts?.headers || {}),
  }
  if (token) headers.Authorization = `Bearer ${token}`
  if (opts?.body !== undefined) headers['Content-Type'] = 'application/json'
  if (opts?.idempotent && ['POST', 'PUT', 'PATCH'].includes(method.toUpperCase())) {
    headers['Idempotency-Key'] = genIdempotencyKey()
  }
  if (opts?.etag) headers['If-Match'] = opts.etag

  const resp = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: opts?.body !== undefined ? JSON.stringify(opts.body) : undefined,
  })

  const etag = resp.headers.get('ETag') || ''
  if (resp.status === 204) {
    return { data: undefined as unknown as T, etag }
  }
  const text = await resp.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = { detail: text }
  }
  if (!resp.ok) {
    throw new APIError(resp.status, (parsed as ProblemDetail) || { title: resp.statusText })
  }
  return { data: parsed as T, etag }
}
