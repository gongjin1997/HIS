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

describe('RBAC evaluatePermission (5 roles, T-M3-16)', () => {
  const admin: UserActor = {
    id: 'u1',
    username: 'admin',
    roleBindings: [{ projectId: '*', role: 'admin' }],
  }
  const projectAdmin: UserActor = {
    id: 'u2',
    username: 'pa',
    roleBindings: [{ projectId: 'prj_a', role: 'project-admin' }],
  }
  const viewer: UserActor = {
    id: 'u3',
    username: 'ro',
    roleBindings: [{ projectId: '*', role: 'viewer' }],
  }
  const auditor: UserActor = {
    id: 'u4',
    username: 'aud',
    roleBindings: [{ projectId: '*', role: 'auditor' }],
  }

  it('grants wildcard permissions to platform admin', () => {
    expect(evaluatePermission(admin, 'instance:destroy', 'prj_x')).toBe(true)
    expect(evaluatePermission(admin, 'secrets:rewrap', 'prj_x')).toBe(true)
  })

  it('scopes project-admin to assigned project only', () => {
    expect(evaluatePermission(projectAdmin, 'host:write', 'prj_a')).toBe(true)
    expect(evaluatePermission(projectAdmin, 'host:write', 'prj_b')).toBe(false)
  })

  it('blocks write actions for viewer and auditor while permitting audit verify for auditor', () => {
    expect(evaluatePermission(viewer, 'host:read', 'prj_a')).toBe(true)
    expect(evaluatePermission(viewer, 'host:write', 'prj_a')).toBe(false)
    expect(evaluatePermission(viewer, 'audit:verify', 'prj_a')).toBe(false)
    expect(evaluatePermission(auditor, 'audit:verify', 'prj_a')).toBe(true)
    expect(evaluatePermission(auditor, 'instance:apply', 'prj_a')).toBe(false)
  })
})

describe('Stack DAG cycle detector (KI-STACK-0001)', () => {
  it('accepts valid acyclic diamond topology', () => {
    const res = detectDagCycle([
      { id: 's3', dependsOn: [] },
      { id: 'kafka', dependsOn: [] },
      { id: 'opensearch', dependsOn: ['s3'] },
      { id: 'dataprepper', dependsOn: ['kafka', 'opensearch', 's3'] },
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
    expect(res.cyclePath[0]).toBe(res.cyclePath[res.cyclePath.length - 1])
  })
})

describe('SSE lossless parser & monotonic deduplication (G1 §3.2)', () => {
  it('deduplicates stale event IDs and cleanly terminates on id-less operation.done', () => {
    const first = parseSSEBlock('id: 10\nevent: step.log\ndata: {"msg":"ok"}', 0)
    expect(first.frame?.id).toBe(10)
    expect(first.nextLastEventId).toBe(10)
    expect(first.terminal).toBe(false)

    // Duplicate/stale frame must be dropped
    const dup = parseSSEBlock('id: 10\nevent: step.log\ndata: {"msg":"dup"}', first.nextLastEventId)
    expect(dup.frame).toBeNull()
    expect(dup.nextLastEventId).toBe(10)

    // Terminal operation.done WITHOUT id: line must NOT be dropped and MUST mark terminal
    const done = parseSSEBlock('event: operation.done\ndata: {"state":"succeeded"}', first.nextLastEventId)
    expect(done.frame?.event).toBe('operation.done')
    expect(done.terminal).toBe(true)
    expect(done.nextLastEventId).toBe(10)
  })
})
