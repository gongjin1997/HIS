import { describe, it, expect } from 'vitest'
import {
  formatBytesIEC,
  formatRateSI,
  formatDuration,
  formatOrDash,
  formatBool,
  formatPercent,
} from '@/composables/useKiFormat'
import { evaluatePermission, type UserActor } from '@/stores/auth'
import { detectDagCycle } from '@/utils/dag'
import { parseSSEBlock } from '@/api/sse'

describe('useKiFormat (DESIGN.md v3.0 data formatting)', () => {
  it('formats null/undefined/NaN/empty as em-dash', () => {
    expect(formatOrDash(null)).toBe('—')
    expect(formatOrDash(undefined)).toBe('—')
    expect(formatOrDash(Number.NaN)).toBe('—')
    expect(formatOrDash('   ')).toBe('—')
    expect(formatOrDash('ok')).toBe('ok')
  })

  it('formats IEC storage bytes and SI network rates accurately', () => {
    expect(formatBytesIEC(512)).toBe('512 B')
    expect(formatBytesIEC(1024 * 1024)).toBe('1.00 MiB')
    expect(formatBytesIEC(16 * 1024 * 1024 * 1024)).toBe('16.0 GiB')
    expect(formatRateSI(1_000_000_000)).toBe('1.0 Gbps')
    expect(formatRateSI(250_000_000)).toBe('250 Mbps')
  })

  it('formats durations, percentages, and booleans', () => {
    expect(formatDuration(450)).toBe('450ms')
    expect(formatDuration(65_000)).toBe('01:05')
    expect(formatDuration(3_720_000)).toBe('1h 02m')
    expect(formatPercent(0.953)).toBe('95.3%')
    expect(formatBool(true, 'zh-CN')).toBe('是')
    expect(formatBool(false, 'zh-CN')).toBe('否')
    expect(formatBool(true, 'en-US')).toBe('Yes')
  })
})

describe('RBAC evaluatePermission (v2 default-deny roles)', () => {
  const admin: UserActor = { id: 'u1', username: 'admin', role: 'admin' }
  const operator: UserActor = { id: 'u2', username: 'ops', role: 'operator' }
  const viewer: UserActor = { id: 'u3', username: 'ro', role: 'viewer' }
  const auditor: UserActor = { id: 'u4', username: 'aud', role: 'auditor' }

  it('grants wildcard permissions to admin', () => {
    expect(evaluatePermission(admin, 'iam:write')).toBe(true)
    expect(evaluatePermission(admin, 'delivery:execute')).toBe(true)
  })

  it('permits operator to manage CMDB/Config/Delivery but blocks IAM write', () => {
    expect(evaluatePermission(operator, 'cmdb:write')).toBe(true)
    expect(evaluatePermission(operator, 'config:reveal')).toBe(true)
    expect(evaluatePermission(operator, 'iam:write')).toBe(false)
  })

  it('enforces read-only for viewer and audit:verify for auditor', () => {
    expect(evaluatePermission(viewer, 'cmdb:read')).toBe(true)
    expect(evaluatePermission(viewer, 'cmdb:write')).toBe(false)
    expect(evaluatePermission(viewer, 'audit:verify')).toBe(false)
    expect(evaluatePermission(auditor, 'audit:verify')).toBe(true)
    expect(evaluatePermission(auditor, 'delivery:execute')).toBe(false)
  })
})

describe('Stack DAG cycle detector', () => {
  it('accepts valid acyclic diamond topology', () => {
    const res = detectDagCycle([
      { id: 's3', dependsOn: [] },
      { id: 'kafka', dependsOn: [] },
      { id: 'opensearch', dependsOn: ['s3'] },
      { id: 'app', dependsOn: ['kafka', 'opensearch', 's3'] },
    ])
    expect(res.hasCycle).toBe(false)
    expect(res.cyclePath).toEqual([])
  })

  it('detects directed cycle and returns offending cycle path', () => {
    const res = detectDagCycle([
      { id: 'a', dependsOn: ['b'] },
      { id: 'b', dependsOn: ['c'] },
      { id: 'c', dependsOn: ['a'] },
    ])
    expect(res.hasCycle).toBe(true)
    expect(res.cyclePath.length).toBeGreaterThanOrEqual(3)
  })
})

describe('SSE parser', () => {
  it('deduplicates stale event IDs and terminates on done', () => {
    const first = parseSSEBlock('id: 10\nevent: step.log\ndata: {"msg":"ok"}', 0)
    expect(first.frame?.id).toBe(10)
    const dup = parseSSEBlock('id: 10\nevent: step.log\ndata: {"msg":"dup"}', first.nextLastEventId)
    expect(dup.frame).toBeNull()
    const done = parseSSEBlock('event: operation.done\ndata: {"state":"succeeded"}', first.nextLastEventId)
    expect(done.terminal).toBe(true)
  })
})
