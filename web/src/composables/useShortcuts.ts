import { onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'

export interface ShortcutHandlers {
  onCommandPalette: () => void
  onHelpModal: () => void
  onFocusSearch?: () => void
}

export interface ShortcutDescriptor {
  keys: string
  description: string
  category: string
}

export const GLOBAL_SHORTCUTS: ShortcutDescriptor[] = [
  { keys: '⌘K / Ctrl+K', description: '打开全局命令面板（跳转主机/实例/页面）', category: '导航' },
  { keys: '/', description: '聚焦当前页搜索框', category: '导航' },
  { keys: 'g d', description: '跳转到总览驾驶舱 (Dashboard)', category: '快速跳转' },
  { keys: 'g h', description: '跳转到 CMDB 主机列表 (Hosts)', category: '快速跳转' },
  { keys: 'g i', description: '跳转到实例与插件 (Instances)', category: '快速跳转' },
  { keys: 'g s', description: '跳转到 Stack DAG 编排 (Stacks)', category: '快速跳转' },
  { keys: 'g o', description: '跳转到执行中心 (Operations)', category: '快速跳转' },
  { keys: 'g a', description: '跳转到四眼审批台 (Approvals)', category: '快速跳转' },
  { keys: '?', description: '打开快捷键速查表', category: '帮助' },
  { keys: 'Esc', description: '关闭当前弹窗 / 抽屉 / 命令面板', category: '帮助' },
]

function isEditableElement(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName.toLowerCase()
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return true
  if (el.isContentEditable) return true
  return Boolean(el.closest('.cm-editor, .xterm'))
}

/**
 * DESIGN.md §10.7 keyboard navigation & chord shortcuts (automatically disabled while typing in form/editor/terminal).
 */
export function useShortcuts(handlers: ShortcutHandlers) {
  const router = useRouter()
  let pendingG = false
  let chordTimer: number | null = null

  const clearChord = () => {
    pendingG = false
    if (chordTimer !== null) {
      window.clearTimeout(chordTimer)
      chordTimer = null
    }
  }

  const onKeydown = (e: KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault()
      clearChord()
      handlers.onCommandPalette()
      return
    }

    if (e.metaKey || e.ctrlKey || e.altKey || isEditableElement(e.target)) {
      return
    }

    const key = e.key
    if (pendingG) {
      clearChord()
      const routes: Record<string, string> = {
        d: '/dashboard',
        h: '/hosts',
        i: '/instances',
        s: '/stacks',
        o: '/operations',
        a: '/operations/approvals',
      }
      const target = routes[key.toLowerCase()]
      if (target) {
        e.preventDefault()
        void router.push(target)
      }
      return
    }

    if (key === 'g') {
      pendingG = true
      chordTimer = window.setTimeout(clearChord, 900)
      return
    }

    if (key === '?') {
      e.preventDefault()
      handlers.onHelpModal()
      return
    }

    if (key === '/' && handlers.onFocusSearch) {
      e.preventDefault()
      handlers.onFocusSearch()
    }
  }

  onMounted(() => window.addEventListener('keydown', onKeydown))
  onUnmounted(() => {
    clearChord()
    window.removeEventListener('keydown', onKeydown)
  })
}
