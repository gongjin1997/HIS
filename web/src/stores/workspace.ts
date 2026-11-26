import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { apiRequest } from '@/api/client'

export interface ProjectItem {
  id: string
  name: string
  description: string
}

export const useWorkspaceStore = defineStore('workspace', () => {
  const projects = ref<ProjectItem[]>([])
  const currentProjectId = ref<string>(localStorage.getItem('kinstall.projectId') || '')
  const currentEnv = ref<'prod' | 'staging' | 'dev' | 'sandbox'>(
    (localStorage.getItem('kinstall.env') as 'prod' | 'staging' | 'dev' | 'sandbox') || 'prod'
  )
  const theme = ref<'dark' | 'light'>((localStorage.getItem('kinstall.theme') as 'dark' | 'light') || 'dark')
  const density = ref<'comfortable' | 'compact'>(
    (localStorage.getItem('kinstall.density') as 'comfortable' | 'compact') || 'comfortable'
  )
  const lang = ref<'zh-CN' | 'en-US'>((localStorage.getItem('kinstall.lang') as 'zh-CN' | 'en-US') || 'zh-CN')
  const pendingApprovalCount = ref<number>(0)
  const sidebarCollapsed = ref<boolean>(false)

  function applyDOMAttributes() {
    const html = document.documentElement
    html.setAttribute('data-theme', theme.value)
    html.classList.toggle('dark', theme.value === 'dark')
    html.setAttribute('data-density', density.value)
    html.setAttribute('lang', lang.value)
    document.title = currentEnv.value === 'prod'
      ? '[PROD] kinstall — 基础设施交付控制台'
      : 'kinstall — 基础设施交付控制台'
  }

  watch([theme, density, lang, currentEnv], () => {
    localStorage.setItem('kinstall.theme', theme.value)
    localStorage.setItem('kinstall.density', density.value)
    localStorage.setItem('kinstall.lang', lang.value)
    localStorage.setItem('kinstall.env', currentEnv.value)
    applyDOMAttributes()
  }, { immediate: true })

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
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
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
    theme,
    density,
    lang,
    pendingApprovalCount,
    sidebarCollapsed,
    loadProjects,
    refreshApprovalBadge,
    toggleTheme,
    toggleDensity,
    toggleLang,
  }
})
