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
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: '总览驾驶舱' } },
        { path: 'hosts', name: 'hosts', component: () => import('@/views/HostsView.vue'), meta: { title: 'CMDB 主机' } },
        { path: 'instances', name: 'instances', component: () => import('@/views/InstancesView.vue'), meta: { title: '实例与插件' } },
        { path: 'stacks', name: 'stacks', component: () => import('@/views/StacksView.vue'), meta: { title: 'Stack DAG 编排' } },
        { path: 'clusters', name: 'clusters', component: () => import('@/views/ClustersView.vue'), meta: { title: '多集群管理' } },
        { path: 'operations', name: 'operations', component: () => import('@/views/OperationsView.vue'), meta: { title: '执行中心' } },
        { path: 'operations/approvals', name: 'approvals', component: () => import('@/views/ApprovalsView.vue'), meta: { title: '四眼审批台' } },
        { path: 'artifacts/media-policies', name: 'media-policies', component: () => import('@/views/MediaPoliciesView.vue'), meta: { title: '介质源策略' } },
        { path: 'security/audit', name: 'audit', component: () => import('@/views/AuditComplianceView.vue'), meta: { title: '审计哈希链' } },
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
