import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import type { ITheme } from '@xterm/xterm'

export type ThemePreference = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'ki.theme'

function readInitialPreference(): ThemePreference {
  if (typeof window === 'undefined') return 'light'
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  return 'light'
}

const preference = ref<ThemePreference>(readInitialPreference())
const systemPrefersDark = ref<boolean>(
  typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : false
)

const resolvedTheme = computed<ResolvedTheme>(() => {
  if (preference.value === 'system') {
    return systemPrefersDark.value ? 'dark' : 'light'
  }
  return preference.value
})

function applyThemeToDOM(resolved: ResolvedTheme) {
  if (typeof document === 'undefined') return
  const html = document.documentElement
  html.setAttribute('data-theme', resolved)
  html.classList.toggle('dark', resolved === 'dark')
}

watch(
  [preference, resolvedTheme],
  ([pref, resolved]) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem(STORAGE_KEY, pref)
    }
    applyThemeToDOM(resolved)
  },
  { immediate: true }
)

export function getXtermTheme(): ITheme {
  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function') {
    return {}
  }
  const s = getComputedStyle(document.documentElement)
  const v = (name: string) => s.getPropertyValue(name).trim()
  return {
    background: v('--ki-bg-terminal'),
    foreground: v('--ki-term-fg'),
    cursor: v('--ki-term-cursor'),
    selectionBackground: v('--ki-term-selection'),
    black: v('--ki-ansi-0'),
    red: v('--ki-ansi-1'),
    green: v('--ki-ansi-2'),
    yellow: v('--ki-ansi-3'),
    blue: v('--ki-ansi-4'),
    magenta: v('--ki-ansi-5'),
    cyan: v('--ki-ansi-6'),
    white: v('--ki-ansi-7'),
    brightBlack: v('--ki-ansi-8'),
    brightRed: v('--ki-ansi-9'),
    brightGreen: v('--ki-ansi-10'),
    brightYellow: v('--ki-ansi-11'),
    brightBlue: v('--ki-ansi-12'),
    brightMagenta: v('--ki-ansi-13'),
    brightCyan: v('--ki-ansi-14'),
    brightWhite: v('--ki-ansi-15'),
  }
}

export function useTheme() {
  let mq: MediaQueryList | null = null
  const onChange = (e: MediaQueryListEvent) => {
    systemPrefersDark.value = e.matches
  }

  onMounted(() => {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      mq = window.matchMedia('(prefers-color-scheme: dark)')
      systemPrefersDark.value = mq.matches
      mq.addEventListener('change', onChange)
    }
  })

  onUnmounted(() => {
    mq?.removeEventListener('change', onChange)
  })

  function setTheme(next: ThemePreference) {
    preference.value = next
  }

  function cycleTheme() {
    const order: ThemePreference[] = ['light', 'dark', 'system']
    const idx = order.indexOf(preference.value)
    preference.value = order[(idx + 1) % order.length]
  }

  function toggleTheme() {
    preference.value = resolvedTheme.value === 'light' ? 'dark' : 'light'
  }

  return {
    preference,
    resolvedTheme,
    setTheme,
    cycleTheme,
    toggleTheme,
    getXtermTheme,
  }
}
