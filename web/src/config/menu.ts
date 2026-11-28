import type { ConsoleTier } from '@/stores/scope'

export interface KsNavItem {
  path: string
  label: string
  icon: string
  badge?: string
  perm?: string
}

export interface KsNavGroup {
  title: string
  items: KsNavItem[]
}

export const TIER_META: Record<
  ConsoleTier,
  { title: string; subtitle: string; defaultPath: string; icon: string }
> = {
  platform: {
    title: '平台管理',
    subtitle: '多租户底座、零信任节点、动态网关与审计链',
    defaultPath: '/platform/overview',
    icon: 'LayoutGrid',
  },
  workspace: {
    title: '企业空间',
    subtitle: '应用中心、双轨 AppKey 与多项目治理',
    defaultPath: '/workspace/overview',
    icon: 'Building2',
  },
  project: {
    title: '项目工作台',
    subtitle: 'CMDB 双拓扑、KMS 配置环境与 DAG 发布编排',
    defaultPath: '/project/overview',
    icon: 'Layers',
  },
}

export const TIER_MENUS: Record<ConsoleTier, KsNavGroup[]> = {
  platform: [
    {
      title: '平台概览',
      items: [{ path: '/platform/overview', label: '平台运行总览', icon: 'Gauge' }],
    },
    {
      title: '多租户与访问控制',
      items: [
        { path: '/platform/tenants', label: '企业空间 / 租户', icon: 'Building2' },
        { path: '/platform/access', label: '账号、角色与策略', icon: 'ShieldCheck' },
      ],
    },
    {
      title: '基础设施底座',
      items: [
        { path: '/platform/nodes', label: '零信任节点池', icon: 'Server' },
        { path: '/platform/gateway', label: '动态网关与制品', icon: 'Network' },
      ],
    },
    {
      title: '合规与可观测',
      items: [{ path: '/platform/audit', label: '防篡改审计链', icon: 'FileCheck2' }],
    },
  ],
  workspace: [
    {
      title: '空间总览',
      items: [{ path: '/workspace/overview', label: '企业空间概览', icon: 'Compass' }],
    },
    {
      title: '应用与项目',
      items: [
        { path: '/workspace/apps', label: '应用中心与 AppKey', icon: 'Boxes' },
        { path: '/workspace/projects', label: '项目治理与授权', icon: 'FolderKanban' },
      ],
    },
  ],
  project: [
    {
      title: '工作台概览',
      items: [{ path: '/project/overview', label: '项目应用概览', icon: 'Activity' }],
    },
    {
      title: 'LiveOps 核心底座',
      items: [
        { path: '/project/assets', label: 'CMDB 资产与双拓扑', icon: 'GitFork' },
        { path: '/project/configs', label: '配置中心与环境合集', icon: 'KeyRound' },
        { path: '/project/jobs', label: 'DAG 部署与 SSE 流', icon: 'Workflow' },
      ],
    },
  ],
}
