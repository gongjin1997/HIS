import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { apiRequest } from '@/api/client'
import { useTheme } from '@/composables/useTheme'

export interface WorkspaceItem {
  id: string
  code: string
  name: string
  description: string
}

export type EnvCode = 'prod' | 'beta' | 'uat' | 'sit' | 'dev'

export const useWorkspaceStore = defineStore('workspace', () => {
  const themeCtl = useTheme()

  const workspaces = ref<WorkspaceItem[]>([])
  const currentWorkspaceId = ref<string>(localStorage.getItem('kinstall.workspaceId') || '')
  const currentEnv = ref<EnvCode>((localStorage.getItem('kinstall.env') as EnvCode) || 'prod')
  const density = ref<'comfortable' | 'compact'>(
    ((localStorage.getItem('ki.density') as 'comfortable' | 'compact') || 'comfortable')
  )
  const lang = ref<'zh-CN' | 'en-US'>('zh-CN')
  const sidebarCollapsed = ref<boolean>(false)

  function applyDOM() {
    if (typeof document === 'undefined') return
    document.documentElement.setAttribute('data-density', density.value)
    document.title = `[${currentEnv.value.toUpperCase()}] Kinstall — 统一 CMDB 与交付控制台`
  }

  watch(
    [density, currentEnv],
    () => {
      localStorage.setItem('ki.density', density.value)
      localStorage.setItem('kinstall.env', currentEnv.value)
      applyDOM()
    },
    { immediate: true }
  )

  watch(currentWorkspaceId, (v) => {
    if (v) localStorage.setItem('kinstall.workspaceId', v)
  })

  async function loadWorkspaces() {
    const { data } = await apiRequest<{ items: WorkspaceItem[] }>('GET', '/api/v1/workspaces')
    workspaces.value = data.items || []
    if (workspaces.value.length > 0 && !workspaces.value.some((w) => w.id === currentWorkspaceId.value)) {
      currentWorkspaceId.value = workspaces.value[0].id
    }
  }

  function toggleDensity() {
    density.value = density.value === 'comfortable' ? 'compact' : 'comfortable'
  }

  return {
    workspaces,
    currentWorkspaceId,
    currentEnv,
    theme: themeCtl.resolvedTheme,
    themePreference: themeCtl.preference,
    density,
    lang,
    sidebarCollapsed,
    loadWorkspaces,
    toggleTheme: themeCtl.toggleTheme,
    setTheme: themeCtl.setTheme,
    toggleDensity,
  }
})
