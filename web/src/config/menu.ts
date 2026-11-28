import type { ConsoleTier } from '@/stores/scope'

export interface KsNavItem {
  path: string
  label: string
  icon: string
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
    subtitle: '节点基础设施、企业空间、访问控制与操作审计',
    defaultPath: '/platform/overview',
    icon: 'LayoutGrid',
  },
  workspace: {
    title: '企业空间',
    subtitle: '应用中心、密钥凭据与项目团队治理',
    defaultPath: '/workspace/overview',
    icon: 'Building2',
  },
  project: {
    title: '项目工作台',
    subtitle: 'CMDB 资产拓扑、配置中心与发布编排',
    defaultPath: '/project/overview',
    icon: 'Layers',
  },
}

export const TIER_MENUS: Record<ConsoleTier, KsNavGroup[]> = {
  platform: [
    {
      title: '概览',
      items: [{ path: '/platform/overview', label: '运行总览', icon: 'Gauge' }],
    },
    {
      title: '多租户与权限',
      items: [
        { path: '/platform/tenants', label: '企业空间', icon: 'Building2' },
        { path: '/platform/access', label: '访问控制', icon: 'ShieldCheck' },
      ],
    },
    {
      title: '基础设施',
      items: [
        { path: '/platform/nodes', label: '节点管理', icon: 'Server' },
        { path: '/platform/gateway', label: '网关与制品', icon: 'Network' },
      ],
    },
    {
      title: '安全与合规',
      items: [{ path: '/platform/audit', label: '操作审计', icon: 'FileCheck2' }],
    },
  ],
  workspace: [
    {
      title: '空间概览',
      items: [{ path: '/workspace/overview', label: '基本概况', icon: 'Compass' }],
    },
    {
      title: '应用与项目',
      items: [
        { path: '/workspace/apps', label: '应用管理', icon: 'Boxes' },
        { path: '/workspace/projects', label: '项目管理', icon: 'FolderKanban' },
      ],
    },
  ],
  project: [
    {
      title: '项目概览',
      items: [{ path: '/project/overview', label: '应用概况', icon: 'Activity' }],
    },
    {
      title: '运维与交付',
      items: [
        { path: '/project/assets', label: '资产库 (CMDB)', icon: 'GitFork' },
        { path: '/project/configs', label: '配置中心', icon: 'KeyRound' },
        { path: '/project/jobs', label: '发布编排', icon: 'Workflow' },
      ],
    },
  ],
}
