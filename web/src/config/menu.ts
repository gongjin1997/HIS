import {
  LayoutDashboard,
  Server,
  Box,
  Boxes,
  Database,
  GitBranch,
  GitFork,
  Network,
  Terminal,
  ClipboardCheck,
  HardDrive,
  Package,
  PlaySquare,
  ShieldCheck,
  Settings,
  SlidersHorizontal,
  Puzzle,
  Rocket,
  Radio,
  Landmark,
} from 'lucide-vue-next'
import { markRaw, type Component } from 'vue'

export interface MenuItem {
  path: string
  key: string
  title: string
  icon: Component
  badge?: 'approvals'
  workspace?: 'cmdb' | 'delivery' | 'ops' | 'governance'
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
  id: 'cmdb' | 'delivery' | 'ops' | 'governance'
  title: string
  items: WorkspaceTab[]
}

export const MENU_GROUPS: MenuGroup[] = [
  {
    title: '资产总览',
    items: [
      { path: '/dashboard', key: 'nav.dashboard', title: 'CMDB 资产大盘', icon: markRaw(LayoutDashboard) },
    ],
  },
  {
    title: 'CMDB 资产核心',
    items: [
      { path: '/cmdb/models', key: 'nav.cmdbModels', title: '元模型与画布', icon: markRaw(Boxes), workspace: 'cmdb' },
      { path: '/cmdb/topology', key: 'nav.cmdbTopology', title: '业务与资源拓扑', icon: markRaw(GitFork), workspace: 'cmdb' },
      { path: '/cmdb/resources', key: 'nav.cmdbResources', title: 'CI 资源库与图谱', icon: markRaw(Database), workspace: 'cmdb' },
      { path: '/hosts', key: 'nav.hosts', title: '主机资产与采集', icon: markRaw(Server), workspace: 'cmdb' },
      { path: '/cmdb/dcim', key: 'nav.cmdbDcim', title: '机柜与 IPAM', icon: markRaw(Landmark), workspace: 'cmdb' },
    ],
  },
  {
    title: '交付与配置编排',
    items: [
      { path: '/config-center', key: 'nav.configCenter', title: '配置与密钥中心', icon: markRaw(SlidersHorizontal), workspace: 'delivery' },
      { path: '/plugins', key: 'nav.plugins', title: '插件与模板市场', icon: markRaw(Puzzle), workspace: 'delivery' },
      { path: '/delivery', key: 'nav.delivery', title: '分批交付发布', icon: markRaw(Rocket), workspace: 'delivery' },
      { path: '/instances', key: 'nav.instances', title: '托管组件实例', icon: markRaw(Box), workspace: 'delivery' },
      { path: '/stacks', key: 'nav.stacks', title: '应用栈 DAG', icon: markRaw(GitBranch), workspace: 'delivery' },
    ],
  },
  {
    title: '执行舰队与作业',
    items: [
      { path: '/fleet', key: 'nav.fleet', title: 'Agent 执行舰队', icon: markRaw(Radio), workspace: 'ops' },
      { path: '/jobs', key: 'nav.jobs', title: '批量作业中心', icon: markRaw(PlaySquare), workspace: 'ops' },
      { path: '/operations', key: 'nav.operations', title: '执行中心', icon: markRaw(Terminal), workspace: 'ops' },
      { path: '/operations/approvals', key: 'nav.approvals', title: '变更审批', icon: markRaw(ClipboardCheck), badge: 'approvals', workspace: 'ops' },
    ],
  },
  {
    title: '制品与治理',
    items: [
      { path: '/artifacts/bundles', key: 'nav.artifacts', title: 'CAS 制品与 OCI', icon: markRaw(Package), workspace: 'governance' },
      { path: '/artifacts/media-policies', key: 'nav.mediaPolicies', title: '介质源策略', icon: markRaw(HardDrive), workspace: 'governance' },
      { path: '/clusters', key: 'nav.clusters', title: '多集群管理', icon: markRaw(Network), workspace: 'governance' },
      { path: '/security/audit', key: 'nav.audit', title: '审计与合规', icon: markRaw(ShieldCheck), workspace: 'governance' },
      { path: '/settings', key: 'nav.settings', title: '平台设置', icon: markRaw(Settings) },
    ],
  },
]

export const ALL_MENU_ITEMS: MenuItem[] = MENU_GROUPS.flatMap((g) => g.items)

export const GROUP_OF_PATH: Record<string, string> = {
  ...Object.fromEntries(MENU_GROUPS.flatMap((g) => g.items.map((it) => [it.path, g.title]))),
  '/security/compliance': '制品与治理',
  '/__design': '设计系统',
}

export const TITLE_OF_PATH: Record<string, string> = {
  ...Object.fromEntries(ALL_MENU_ITEMS.map((it) => [it.path, it.title])),
  '/security/compliance': '等保密评中心',
  '/__design': '设计系统基准',
}

export const WORKSPACES: WorkspaceGroup[] = [
  {
    id: 'cmdb',
    title: 'CMDB 资产核心',
    items: [
      { path: '/cmdb/models', label: '元模型与画布' },
      { path: '/cmdb/topology', label: '双拓扑树' },
      { path: '/cmdb/resources', label: 'CI 资源库' },
      { path: '/hosts', label: '主机资产' },
      { path: '/cmdb/dcim', label: '机柜与 IPAM' },
    ],
  },
  {
    id: 'delivery',
    title: '交付与配置编排',
    items: [
      { path: '/config-center', label: '配置与密钥' },
      { path: '/plugins', label: '声明式插件' },
      { path: '/delivery', label: '分批交付' },
      { path: '/instances', label: '组件实例' },
      { path: '/stacks', label: '应用栈 DAG' },
    ],
  },
  {
    id: 'ops',
    title: '执行舰队与作业',
    items: [
      { path: '/fleet', label: 'Agent 舰队' },
      { path: '/jobs', label: '批量作业' },
      { path: '/operations', label: '执行中心' },
      { path: '/operations/approvals', label: '变更审批' },
    ],
  },
  {
    id: 'governance',
    title: '制品与合规治理',
    items: [
      { path: '/artifacts/bundles', label: 'CAS 制品与 OCI' },
      { path: '/artifacts/media-policies', label: '介质源策略' },
      { path: '/clusters', label: '多集群管理' },
      { path: '/security/audit', label: '审计哈希链' },
      { path: '/security/compliance', label: '等保密评' },
    ],
  },
]

export function workspaceForPath(path: string): WorkspaceGroup | null {
  return WORKSPACES.find((w) => w.items.some((it) => it.path === path)) || null
}
