import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { apiRequest } from '@/api/client'

export type RoleName = 'admin' | 'project-admin' | 'operator' | 'auditor' | 'viewer'

export interface RoleBinding {
  projectId: string
  env?: string
  role: string
}

export interface UserActor {
  id: string
  username: string
  displayName?: string
  type?: string
  roleBindings?: RoleBinding[]
}

/**
 * Action-to-role matrix aligned with internal/authz & DESIGN.md §9.6 (T-M3-16).
 */
const ROLE_PERMISSIONS: Record<RoleName, ReadonlySet<string>> = {
  admin: new Set(['*']),
  'project-admin': new Set([
    'project:read',
    'host:read', 'host:write', 'host:probe',
    'instance:read', 'instance:write', 'instance:plan', 'instance:apply', 'instance:destroy',
    'stack:read', 'stack:write', 'stack:apply',
    'operation:read', 'operation:resume', 'operation:approve', 'operation:report',
    'media:read', 'media:write', 'media:test',
    'credential:read', 'credential:write', 'secrets:rewrap',
    'cluster:read', 'cluster:kubeconfig',
    'audit:read', 'audit:verify', 'compliance:read', 'compliance:report',
  ]),
  operator: new Set([
    'project:read',
    'host:read', 'host:write', 'host:probe',
    'instance:read', 'instance:write', 'instance:plan', 'instance:apply',
    'stack:read', 'stack:write', 'stack:apply',
    'operation:read', 'operation:resume', 'operation:approve', 'operation:report',
    'media:read', 'media:test',
    'credential:read', 'credential:write',
    'cluster:read', 'cluster:kubeconfig',
    'audit:read',
  ]),
  auditor: new Set([
    'project:read',
    'host:read',
    'instance:read',
    'stack:read',
    'operation:read', 'operation:report',
    'media:read',
    'cluster:read',
    'audit:read', 'audit:verify', 'compliance:read', 'compliance:report',
  ]),
  viewer: new Set([
    'project:read',
    'host:read',
    'instance:read',
    'stack:read',
    'operation:read',
    'media:read',
    'cluster:read',
  ]),
}

export function evaluatePermission(
  actor: UserActor | null,
  action: string,
  projectId?: string
): boolean {
  if (!actor) return false
  const bindings = actor.roleBindings || []
  for (const b of bindings) {
    const scopeMatches =
      b.projectId === '*' ||
      !projectId ||
      b.projectId === projectId
    if (!scopeMatches) continue
    const role = (b.role || '').toLowerCase() as RoleName
    const perms = ROLE_PERMISSIONS[role]
    if (!perms) continue
    if (perms.has('*') || perms.has(action)) return true
  }
  return false
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem('kinstall.token') || '')
  const user = ref<UserActor | null>(null)
  const initialized = ref(false)

  const isAuthenticated = computed(() => Boolean(user.value || token.value))

  const primaryRole = computed<string>(() => {
    const roles = (user.value?.roleBindings || []).map((b) => b.role)
    if (roles.includes('admin')) return 'admin'
    if (roles.includes('project-admin')) return 'project-admin'
    if (roles.includes('operator')) return 'operator'
    if (roles.includes('auditor')) return 'auditor'
    return roles[0] || 'viewer'
  })

  function can(action: string, projectId?: string): boolean {
    return evaluatePermission(user.value, action, projectId)
  }

  async function fetchMe() {
    try {
      const { data } = await apiRequest<UserActor>('GET', '/api/v1/me')
      user.value = data
      initialized.value = true
      return true
    } catch {
      user.value = null
      initialized.value = true
      return false
    }
  }

  async function loginLocal(username: string, password: string) {
    const { data } = await apiRequest<{ token: string; user: UserActor }>('POST', '/api/v1/auth/login', {
      body: { username, password },
    })
    token.value = data.token
    user.value = data.user
    localStorage.setItem('kinstall.token', data.token)
    return data.user
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem('kinstall.token')
  }

  return { token, user, initialized, isAuthenticated, primaryRole, can, fetchMe, loginLocal, logout }
})
