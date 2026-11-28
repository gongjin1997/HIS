import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, configureApiClient } from '@/api/client'

export type ConsoleTier = 'platform' | 'workspace' | 'project'

export interface TrustedContext {
  tenantId: string
  appId: string
  projectId: string
  subjectId: string
  username: string
  displayName: string
  roles: string[]
  permissions: string[]
  scopeVersion: number
  authKind: string
}

export interface TenantItem {
  tenant_id: string
  code: string
  name: string
  description: string
  status: string
  created_at: string
}

export interface AppItem {
  tenant_id: string
  app_id: string
  code: string
  name: string
  description: string
  status: string
  created_at: string
}

export interface ProjectItem {
  tenant_id: string
  app_id: string
  project_id: string
  code: string
  name: string
  description: string
  permission_version: number
  status: string
  created_at: string
}

export const useScopeStore = defineStore('scope', () => {
  const token = ref<string>(localStorage.getItem('liveops.token') || '')
  const context = ref<TrustedContext | null>(null)
  const tenants = ref<TenantItem[]>([])
  const apps = ref<AppItem[]>([])
  const projects = ref<ProjectItem[]>([])

  const tenantId = ref<string>(localStorage.getItem('liveops.tenantId') || 'default-tenant')
  const appId = ref<string>(localStorage.getItem('liveops.appId') || 'his-core')
  const projectId = ref<string>(localStorage.getItem('liveops.projectId') || 'prod-clinical')

  const theme = ref<'light' | 'dark'>((localStorage.getItem('ki.theme') as 'light' | 'dark') || 'light')
  const density = ref<'comfortable' | 'compact'>(
    (localStorage.getItem('ki.density') as 'comfortable' | 'compact') || 'comfortable'
  )
  const lang = ref<string>(localStorage.getItem('ki.lang') || 'zh-CN')
  const initialized = ref(false)
  const probesHealthy = ref(true)

  const isAuthenticated = computed(() => Boolean(token.value && context.value))
  const currentTenant = computed(() => tenants.value.find((t) => t.tenant_id === tenantId.value) || tenants.value[0])
  const currentApp = computed(() => apps.value.find((a) => a.app_id === appId.value) || apps.value[0])
  const currentProject = computed(() => projects.value.find((p) => p.project_id === projectId.value) || projects.value[0])

  function applyVisualPrefs() {
    document.documentElement.setAttribute('data-theme', theme.value)
    document.documentElement.setAttribute('data-density', density.value)
    document.documentElement.classList.toggle('dark', theme.value === 'dark')
    localStorage.setItem('ki.theme', theme.value)
    localStorage.setItem('ki.density', density.value)
    localStorage.setItem('ki.lang', lang.value)
  }

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
    applyVisualPrefs()
  }

  function toggleDensity() {
    density.value = density.value === 'comfortable' ? 'compact' : 'comfortable'
    applyVisualPrefs()
  }

  const currentEnv = computed(() => projectId.value || 'prod')

  function persistScope() {
    localStorage.setItem('liveops.tenantId', tenantId.value)
    localStorage.setItem('liveops.appId', appId.value)
    localStorage.setItem('liveops.projectId', projectId.value)
  }

  function hasPerm(perm: string): boolean {
    const perms = context.value?.permissions || []
    return perms.includes('*') || perms.includes(perm)
  }

  function clearSession() {
    token.value = ''
    context.value = null
    localStorage.removeItem('liveops.token')
  }

  configureApiClient({
    getScope: () => ({ tenantId: tenantId.value, appId: appId.value, projectId: projectId.value }),
    getToken: () => token.value,
    onUnauthorized: () => {
      clearSession()
      if (window.location.pathname !== '/login') {
        window.location.assign('/login')
      }
    },
  })

  async function login(username: string, password: string) {
    const { data } = await api.post<{ token: string; context: TrustedContext }>('/api/v1/iam/auth/login', {
      username,
      password,
    })
    token.value = data.token
    localStorage.setItem('liveops.token', data.token)
    context.value = data.context
    tenantId.value = data.context.tenantId || 'default-tenant'
    appId.value = data.context.appId || 'his-core'
    projectId.value = data.context.projectId || 'prod-clinical'
    persistScope()
    await refreshMe()
  }

  async function logout() {
    try {
      await api.post('/api/v1/iam/auth/logout')
    } catch {
      // ignore network error on logout
    }
    clearSession()
  }

  async function refreshMe() {
    if (!token.value) {
      initialized.value = true
      return
    }
    try {
      const { data } = await api.get<{
        context: TrustedContext
        tenants: TenantItem[]
        apps: AppItem[]
        projects: ProjectItem[]
      }>('/api/v1/iam/auth/me')
      context.value = data.context
      tenants.value = data.tenants || []
      apps.value = data.apps || []
      projects.value = data.projects || []
      tenantId.value = data.context.tenantId
      appId.value = data.context.appId
      projectId.value = data.context.projectId
      persistScope()
      probesHealthy.value = true
    } catch {
      clearSession()
    } finally {
      initialized.value = true
    }
  }

  async function switchTenant(nextTenantId: string) {
    tenantId.value = nextTenantId
    appId.value = ''
    projectId.value = ''
    persistScope()
    await refreshMe()
  }

  async function switchApp(nextAppId: string) {
    appId.value = nextAppId
    projectId.value = ''
    persistScope()
    await refreshMe()
  }

  async function switchProject(nextProjectId: string) {
    projectId.value = nextProjectId
    persistScope()
    await refreshMe()
  }

  applyVisualPrefs()

  return {
    token,
    context,
    tenants,
    apps,
    projects,
    tenantId,
    appId,
    projectId,
    theme,
    density,
    lang,
    initialized,
    probesHealthy,
    isAuthenticated,
    currentTenant,
    currentApp,
    currentProject,
    hasPerm,
    toggleTheme,
    toggleDensity,
    currentEnv,
    login,
    logout,
    refreshMe,
    switchTenant,
    switchApp,
    switchProject,
  }
})
