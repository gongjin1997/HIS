import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppShell from '@/components/AppShell.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true, title: '登录' } },
    {
      path: '/',
      component: AppShell,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: 'CMDB 资产大盘' } },
        // Enterprise Dynamic Meta-Model CMDB Core (bk-cmdb / VeOps / NetBox)
        { path: 'cmdb/models', name: 'cmdb-models', component: () => import('@/views/CmdbModelsView.vue'), meta: { title: '元模型与画布' } },
        { path: 'cmdb/topology', name: 'cmdb-topology', component: () => import('@/views/CmdbTopologyView.vue'), meta: { title: '业务与资源拓扑' } },
        { path: 'cmdb/resources', name: 'cmdb-resources', component: () => import('@/views/CmdbResourcesView.vue'), meta: { title: 'CI 资源库与图谱' } },
        { path: 'cmdb/dcim', name: 'cmdb-dcim', component: () => import('@/views/CmdbDcimView.vue'), meta: { title: '机柜与 IPAM' } },
        { path: 'hosts', name: 'hosts', component: () => import('@/views/HostsView.vue'), meta: { title: '主机资产与采集' } },
        // Delivery, Versioned Config & Extensible Declarative Plugins
        { path: 'config-center', name: 'config-center', component: () => import('@/views/ConfigCenterView.vue'), meta: { title: '配置与密钥中心' } },
        { path: 'plugins', name: 'plugins', component: () => import('@/views/PluginsView.vue'), meta: { title: '插件与模板市场' } },
        { path: 'delivery', name: 'delivery', component: () => import('@/views/DeliveryView.vue'), meta: { title: '分批交付发布' } },
        { path: 'instances', name: 'instances', component: () => import('@/views/InstancesView.vue'), meta: { title: '托管组件实例' } },
        { path: 'stacks', name: 'stacks', component: () => import('@/views/StacksView.vue'), meta: { title: '应用栈 DAG' } },
        // Dual-Mode Execution Plane (SSH + Outbound Go Agent Fleet)
        { path: 'fleet', name: 'fleet', component: () => import('@/views/FleetView.vue'), meta: { title: 'Agent 执行舰队' } },
        { path: 'jobs', name: 'jobs', component: () => import('@/views/JobsView.vue'), meta: { title: '批量作业中心' } },
        { path: 'operations', name: 'operations', component: () => import('@/views/OperationsView.vue'), meta: { title: '执行中心' } },
        { path: 'operations/approvals', name: 'approvals', component: () => import('@/views/ApprovalsView.vue'), meta: { title: '变更审批' } },
        // Artifacts, Multi-Cluster & Governance
        { path: 'artifacts/bundles', name: 'artifacts-bundles', component: () => import('@/views/ArtifactsView.vue'), meta: { title: 'CAS 制品与 OCI' } },
        { path: 'artifacts/media-policies', name: 'media-policies', component: () => import('@/views/MediaPoliciesView.vue'), meta: { title: '介质源策略' } },
        { path: 'clusters', name: 'clusters', component: () => import('@/views/ClustersView.vue'), meta: { title: '多集群管理' } },
        { path: 'security/audit', name: 'audit', component: () => import('@/views/AuditComplianceView.vue'), meta: { title: '审计与合规' } },
        { path: 'security/compliance', name: 'compliance', component: () => import('@/views/AuditComplianceView.vue'), meta: { title: '等保密评中心' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue'), meta: { title: '平台设置' } },
        { path: '__design', name: 'design-showcase', component: () => import('@/views/DesignShowcaseView.vue'), meta: { title: '设计系统基准' } },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const auth = useAuthStore()
  if (!auth.initialized) {
    const ok = await auth.fetchMe()
    if (!ok) return '/login'
  } else if (!auth.isAuthenticated) {
    return '/login'
  }
  return true
})

export default router
