export interface DagNodeSpec {
  id: string
  dependsOn: string[]
}

export interface DagCycleResult {
  hasCycle: boolean
  cyclePath: string[]
}

/**
 * Detects directed cycles in a Stack DAG (Kahn / 3-color DFS) and returns the offending path if cyclic.
 */
export function detectDagCycle(nodes: DagNodeSpec[]): DagCycleResult {
  const byId = new Map<string, string[]>()
  for (const n of nodes) {
    byId.set(n.id, n.dependsOn || [])
  }
  const color = new Map<string, 0 | 1 | 2>() // 0=unvisited, 1=visiting, 2=done
  const stack: string[] = []

  const dfs = (id: string): string[] | null => {
    color.set(id, 1)
    stack.push(id)
    for (const dep of byId.get(id) || []) {
      if (!byId.has(dep)) continue
      const c = color.get(dep) || 0
      if (c === 1) {
        const start = stack.indexOf(dep)
        return [...stack.slice(start), dep]
      }
      if (c === 0) {
        const found = dfs(dep)
        if (found) return found
      }
    }
    stack.pop()
    color.set(id, 2)
    return null
  }

  for (const n of nodes) {
    if ((color.get(n.id) || 0) === 0) {
      const found = dfs(n.id)
      if (found) {
        return { hasCycle: true, cyclePath: found }
      }
    }
  }
  return { hasCycle: false, cyclePath: [] }
}
