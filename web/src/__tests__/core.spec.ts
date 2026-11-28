import { describe, expect, it } from 'vitest'
import { TIER_MENUS, TIER_META } from '@/config/menu'
import { newIdempotencyKey } from '@/api/client'

describe('LiveOps × KubeSphere Console Foundation', () => {
  it('defines all three KubeSphere governance tiers without plugin bloat', () => {
    expect(Object.keys(TIER_META)).toEqual(['platform', 'workspace', 'project'])
    const allPaths = Object.values(TIER_MENUS)
      .flatMap((groups) => groups.flatMap((g) => g.items.map((i) => i.path)))
    expect(allPaths.length).toBe(13)
    expect(allPaths.some((p) => p.includes('plugin'))).toBe(false)
    expect(allPaths).toContain('/platform/overview')
    expect(allPaths).toContain('/workspace/apps')
    expect(allPaths).toContain('/project/assets')
    expect(allPaths).toContain('/project/configs')
    expect(allPaths).toContain('/project/jobs')
  })

  it('generates valid ASCII Idempotency-Key tokens', () => {
    const k1 = newIdempotencyKey('dag')
    const k2 = newIdempotencyKey('dag')
    expect(k1).not.toEqual(k2)
    expect(k1.length).toBeLessThanOrEqual(128)
    expect(/^[\x21-\x7e]+$/.test(k1)).toBe(true)
  })
})
