import {
  LayoutDashboard,
  Server,
  Box,
  GitBranch,
  Network,
  Terminal,
  ClipboardCheck,
  HardDrive,
  ShieldCheck,
  Settings,
  Palette,
} from 'lucide-vue-next'
import { markRaw, type Component } from 'vue'

export interface MenuItem {
  path: string
  key: string
  title: string
  icon: Component
  badge?: 'approvals'
  workspace?: 'ops' | 'governance'
}

export interface MenuGroup {
  title: string
  items: MenuItem[]
}

export interface WorkspaceTab {
  path: string
  label: string
}

export interface WorkspaceGroup {
  id: 'ops' | 'governance'
  title: string
  items: WorkspaceTab[]
}

export const MENU_GROUPS: MenuGroup[] = [
  {
    title: '交付总览',
    items: [
      { path: '/dashboard', key: 'nav.dashboard', title: '总览驾驶舱', icon: markRaw(LayoutDashboard) },
    ],
  },
  {
    title: '基础设施与交付',
    items: [
      { path: '/hosts', key: 'nav.hosts', title: 'CMDB 主机', icon: markRaw(Server) },
      { path: '/instances', key: 'nav.instances', title: '实例与插件', icon: markRaw(Box) },
      { path: '/stacks', key: 'nav.stacks', title: 'Stack DAG 编排', icon: markRaw(GitBranch) },
      { path: '/clusters', key: 'nav.clusters', title: '多集群管理', icon: markRaw(Network) },
    ],
  },
  {
    title: '执行与审批',
    items: [
      { path: '/operations', key: 'nav.operations', title: '执行中心', icon: markRaw(Terminal), workspace: 'ops' },
      { path: '/operations/approvals', key: 'nav.approvals', title: '四眼审批台', icon: markRaw(ClipboardCheck), badge: 'approvals', workspace: 'ops' },
    ],
  },
  {
    title: '安全与平台',
    items: [
      { path: '/artifacts/media-policies', key: 'nav.mediaPolicies', title: '介质源策略', icon: markRaw(HardDrive), workspace: 'governance' },
      { path: '/security/audit', key: 'nav.audit', title: '审计与合规', icon: markRaw(ShieldCheck), workspace: 'governance' },
      { path: '/settings', key: 'nav.settings', title: '平台设置', icon: markRaw(Settings) },
      { path: '/__design', key: 'nav.design', title: '设计系统基准', icon: markRaw(Palette) },
    ],
  },
]

export const ALL_MENU_ITEMS: MenuItem[] = MENU_GROUPS.flatMap((g) => g.items)

export const GROUP_OF_PATH: Record<string, string> = Object.fromEntries(
  MENU_GROUPS.flatMap((g) => g.items.map((it) => [it.path, g.title]))
)

export const TITLE_OF_PATH: Record<string, string> = {
  ...Object.fromEntries(ALL_MENU_ITEMS.map((it) => [it.path, it.title])),
  '/security/compliance': '等保密评中心',
}

export const WORKSPACES: WorkspaceGroup[] = [
  {
    id: 'ops',
    title: '执行与审批工作区',
    items: [
      { path: '/operations', label: '执行中心' },
      { path: '/operations/approvals', label: '四眼审批台' },
    ],
  },
  {
    id: 'governance',
    title: '供应链与合规治理',
    items: [
      { path: '/security/audit', label: '审计哈希链' },
      { path: '/security/compliance', label: '等保密评中心' },
      { path: '/artifacts/media-policies', label: '介质源策略' },
    ],
  },
]

export function workspaceForPath(path: string): WorkspaceGroup | null {
  return WORKSPACES.find((w) => w.items.some((it) => it.path === path)) || null
}
