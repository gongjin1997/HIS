import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { apiRequest } from '@/api/client'

export interface UserActor {
  id: string
  username: string
  displayName?: string
  type?: string
  roleBindings?: Array<{ projectId: string; env?: string; role: string }>
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem('kinstall.token') || '')
  const user = ref<UserActor | null>(null)
  const initialized = ref(false)

  const isAuthenticated = computed(() => Boolean(user.value || token.value))

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

  return { token, user, initialized, isAuthenticated, fetchMe, loginLocal, logout }
})
