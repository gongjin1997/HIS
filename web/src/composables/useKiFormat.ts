/**
 * DESIGN.md §9.4: Unified deterministic display formatters across all views & tables.
 * - Null / empty values always render as em-dash `—` (never `null`, `undefined`, `NaN`).
 * - Storage uses IEC (KiB / MiB / GiB / TiB); network rate uses SI (Kbps / Mbps / Gbps).
 */

export function formatOrDash(val: unknown): string {
  if (val === null || val === undefined) return '—'
  if (typeof val === 'number' && Number.isNaN(val)) return '—'
  const s = String(val).trim()
  return s === '' ? '—' : s
}

export function formatAbsTime(input: string | number | Date | null | undefined): string {
  if (!input) return '—'
  const d = input instanceof Date ? input : new Date(input)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function formatRelTime(input: string | number | Date | null | undefined, lang: 'zh-CN' | 'en-US' = 'zh-CN', nowMs = Date.now()): string {
  if (!input) return '—'
  const d = input instanceof Date ? input : new Date(input)
  if (Number.isNaN(d.getTime())) return '—'
  const diffSec = Math.round((nowMs - d.getTime()) / 1000)
  const abs = Math.abs(diffSec)
  if (abs < 5) return lang === 'zh-CN' ? '刚刚' : 'just now'
  if (abs < 60) return lang === 'zh-CN' ? `${abs} 秒前` : `${abs}s ago`
  const mins = Math.floor(abs / 60)
  if (mins < 60) return lang === 'zh-CN' ? `${mins} 分钟前` : `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return lang === 'zh-CN' ? `${hours} 小时前` : `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 7) return lang === 'zh-CN' ? `${days} 天前` : `${days}d ago`
  return formatAbsTime(d)
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || Number.isNaN(ms) || ms < 0) return '—'
  const totalSec = Math.floor(ms / 1000)
  const hours = Math.floor(totalSec / 3600)
  const mins = Math.floor((totalSec % 3600) / 60)
  const secs = totalSec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  if (hours > 0) return `${hours}h ${pad(mins)}m`
  if (mins > 0) return `${pad(mins)}:${pad(secs)}`
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `00:${pad(secs)}`
}

export function formatBytesIEC(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes) || bytes < 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let val = bytes
  let idx = 0
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024
    idx++
  }
  const decimals = idx === 0 ? 0 : val >= 100 ? 0 : val >= 10 ? 1 : 2
  return `${val.toFixed(decimals)} ${units[idx]}`
}

export function formatRateSI(bitsPerSec: number | null | undefined): string {
  if (bitsPerSec === null || bitsPerSec === undefined || Number.isNaN(bitsPerSec) || bitsPerSec < 0) return '—'
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let val = bitsPerSec
  let idx = 0
  while (val >= 1000 && idx < units.length - 1) {
    val /= 1000
    idx++
  }
  const decimals = idx === 0 ? 0 : val >= 100 ? 0 : 1
  return `${val.toFixed(decimals)} ${units[idx]}`
}

export function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined || Number.isNaN(n)) return '—'
  return new Intl.NumberFormat('en-US').format(n)
}

export function formatPercent(ratio: number | null | undefined, digits = 1): string {
  if (ratio === null || ratio === undefined || Number.isNaN(ratio)) return '—'
  const pct = ratio <= 1 && ratio >= 0 ? ratio * 100 : ratio
  return `${pct.toFixed(digits)}%`
}

export function formatBool(val: boolean | null | undefined, lang: 'zh-CN' | 'en-US' = 'zh-CN'): string {
  if (val === null || val === undefined) return '—'
  if (lang === 'en-US') return val ? 'Yes' : 'No'
  return val ? '是' : '否'
}

export function useKiFormat() {
  return {
    formatOrDash,
    formatAbsTime,
    formatRelTime,
    formatDuration,
    formatBytesIEC,
    formatRateSI,
    formatNumber,
    formatPercent,
    formatBool,
  }
}
