import type { Component } from 'vue'
import {
  Blocks,
  GitFork,
  HardDrive,
  LayoutDashboard,
  Network,
  Rocket,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
} from 'lucide-vue-next'

export interface MenuItem {
  key: string
  path: string
  title: string
  icon: Component
}

export interface MenuGroup {
  title: string
  items: MenuItem[]
}

export const MENU_GROUPS: MenuGroup[] = [
  {
    title: '全景总览',
    items: [
      { key: 'dashboard', path: '/dashboard', title: '资产与运维全景', icon: LayoutDashboard },
    ],
  },
  {
    title: 'CMDB 资产中枢',
    items: [
      { key: 'cmdb-models', path: '/cmdb/models', title: '元模型与关系', icon: Network },
      { key: 'cmdb-topology', path: '/cmdb/topology', title: '双拓扑与模板', icon: GitFork },
      { key: 'cmdb-resources', path: '/cmdb/resources', title: '统一资产台账', icon: Server },
      { key: 'cmdb-dcim', path: '/cmdb/dcim', title: '机柜与 IPAM', icon: HardDrive },
    ],
  },
  {
    title: '配置与声明式交付',
    items: [
      { key: 'config', path: '/config', title: '多环境配置中心', icon: SlidersHorizontal },
      { key: 'plugins', path: '/plugins', title: '声明式插件库', icon: Blocks },
      { key: 'delivery', path: '/delivery', title: '编排与发布流水线', icon: Rocket },
    ],
  },
  {
    title: '执行面与安全治理',
    items: [
      { key: 'ops', path: '/ops', title: '执行舰队与制品', icon: Terminal },
      { key: 'governance', path: '/governance', title: '权限审计与设置', icon: ShieldCheck },
    ],
  },
]

export const ALL_MENU_ITEMS: MenuItem[] = MENU_GROUPS.flatMap((g) => g.items)

export const GROUP_OF_PATH: Record<string, string> = Object.fromEntries(
  MENU_GROUPS.flatMap((g) => g.items.map((item) => [item.path, g.title]))
)

export const TITLE_OF_PATH: Record<string, string> = Object.fromEntries(
  ALL_MENU_ITEMS.map((item) => [item.path, item.title])
)
