import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { apiRequest } from '@/api/client'
import { useTheme } from '@/composables/useTheme'

export interface ProjectItem {
  id: string
  name: string
  description: string
}

export const useWorkspaceStore = defineStore('workspace', () => {
  const themeCtl = useTheme()

  const projects = ref<ProjectItem[]>([])
  const currentProjectId = ref<string>(localStorage.getItem('kinstall.projectId') || '')
  const currentEnv = ref<'prod' | 'staging' | 'dev' | 'sandbox'>(
    (localStorage.getItem('kinstall.env') as 'prod' | 'staging' | 'dev' | 'sandbox') || 'prod'
  )
  const density = ref<'comfortable' | 'compact'>(
    ((localStorage.getItem('ki.density') || localStorage.getItem('kinstall.density')) as 'comfortable' | 'compact') || 'comfortable'
  )
  const lang = ref<'zh-CN' | 'en-US'>(
    ((localStorage.getItem('ki.lang') || localStorage.getItem('kinstall.lang')) as 'zh-CN' | 'en-US') || 'zh-CN'
  )
  const pendingApprovalCount = ref<number>(0)
  const sidebarCollapsed = ref<boolean>(false)
  const offlineStrict = ref<boolean>(true)

  function applyDOMAttributes() {
    if (typeof document === 'undefined') return
    const html = document.documentElement
    html.setAttribute('data-density', density.value)
    html.setAttribute('lang', lang.value)
    document.title =
      currentEnv.value === 'prod'
        ? '[PROD] kinstall — 基础设施交付控制台'
        : 'kinstall — 基础设施交付控制台'
  }

  watch(
    [density, lang, currentEnv],
    () => {
      localStorage.setItem('ki.density', density.value)
      localStorage.setItem('kinstall.density', density.value)
      localStorage.setItem('ki.lang', lang.value)
      localStorage.setItem('kinstall.lang', lang.value)
      localStorage.setItem('kinstall.env', currentEnv.value)
      applyDOMAttributes()
    },
    { immediate: true }
  )

  watch(currentProjectId, (v) => {
    if (v) localStorage.setItem('kinstall.projectId', v)
  })

  async function loadProjects() {
    const { data } = await apiRequest<{ items: ProjectItem[] }>('GET', '/api/v1/projects')
    projects.value = data.items || []
    if (projects.value.length === 0) {
      const { data: created } = await apiRequest<ProjectItem>('POST', '/api/v1/projects', {
        body: { name: 'core-ops', description: 'Default Enterprise Infrastructure Project' },
        idempotent: true,
      })
      projects.value = [created]
    }
    if (!projects.value.some((p) => p.id === currentProjectId.value)) {
      currentProjectId.value = projects.value[0].id
    }
  }

  async function refreshApprovalBadge() {
    if (!currentProjectId.value) return
    try {
      const { data } = await apiRequest<{ items: unknown[] }>(
        'GET',
        `/api/v1/projects/${currentProjectId.value}/approvals`
      )
      pendingApprovalCount.value = (data.items || []).length
    } catch {
      pendingApprovalCount.value = 0
    }
  }

  function toggleTheme() {
    themeCtl.toggleTheme()
  }

  function cycleTheme() {
    themeCtl.cycleTheme()
  }

  function toggleDensity() {
    density.value = density.value === 'comfortable' ? 'compact' : 'comfortable'
  }

  function toggleLang() {
    lang.value = lang.value === 'zh-CN' ? 'en-US' : 'zh-CN'
  }

  return {
    projects,
    currentProjectId,
    currentEnv,
    theme: themeCtl.resolvedTheme,
    themePreference: themeCtl.preference,
    density,
    lang,
    pendingApprovalCount,
    sidebarCollapsed,
    offlineStrict,
    loadProjects,
    refreshApprovalBadge,
    toggleTheme,
    cycleTheme,
    setTheme: themeCtl.setTheme,
    toggleDensity,
    toggleLang,
  }
})
