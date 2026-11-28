import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { apiRequest } from '@/api/client'

export type RoleName = 'admin' | 'operator' | 'auditor' | 'viewer'

export interface UserActor {
  id?: string
  userId?: string
  username: string
  displayName?: string
  email?: string
  role: string
  workspaceId?: string
}

const ROLE_PERMS: Record<RoleName, ReadonlySet<string>> = {
  admin: new Set(['*']),
  operator: new Set([
    'cmdb:read', 'cmdb:write',
    'config:read', 'config:write', 'config:reveal',
    'delivery:read', 'delivery:write', 'delivery:execute',
    'ops:read', 'ops:execute',
    'catalog:read', 'catalog:write',
    'iam:read', 'audit:read',
  ]),
  auditor: new Set([
    'cmdb:read', 'config:read', 'delivery:read', 'ops:read', 'catalog:read',
    'iam:read', 'audit:read', 'audit:verify',
  ]),
  viewer: new Set(['cmdb:read', 'config:read', 'delivery:read', 'ops:read', 'catalog:read']),
}

export function evaluatePermission(actor: UserActor | null, perm: string): boolean {
  if (!actor) return false
  const role = (actor.role || '').toLowerCase() as RoleName
  const set = ROLE_PERMS[role]
  if (!set) return false
  return set.has('*') || set.has(perm)
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem('kinstall.token') || '')
  const user = ref<UserActor | null>(null)
  const initialized = ref(false)
  const version = ref('2.0.0')

  const isAuthenticated = computed(() => Boolean(token.value && (user.value || !initialized.value)))
  const primaryRole = computed<string>(() => user.value?.role || 'viewer')

  function can(perm: string): boolean {
    return evaluatePermission(user.value, perm)
  }

  async function fetchMe(): Promise<boolean> {
    if (!token.value) {
      user.value = null
      initialized.value = true
      return false
    }
    try {
      const { data } = await apiRequest<{ user: UserActor; version?: string }>('GET', '/api/v1/me')
      user.value = data.user
      if (data.version) version.value = data.version
      initialized.value = true
      return true
    } catch {
      token.value = ''
      user.value = null
      localStorage.removeItem('kinstall.token')
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
    initialized.value = true
    localStorage.setItem('kinstall.token', data.token)
    return data.user
  }

  function logout() {
    token.value = ''
    user.value = null
    initialized.value = true
    localStorage.removeItem('kinstall.token')
  }

  return { token, user, version, initialized, isAuthenticated, primaryRole, can, fetchMe, loginLocal, logout }
})
