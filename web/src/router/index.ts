import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppShell from '@/components/AppShell.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: AppShell,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
        { path: 'hosts', name: 'hosts', component: () => import('@/views/HostsView.vue') },
        { path: 'instances', name: 'instances', component: () => import('@/views/InstancesView.vue') },
        { path: 'stacks', name: 'stacks', component: () => import('@/views/StacksView.vue') },
        { path: 'clusters', name: 'clusters', component: () => import('@/views/ClustersView.vue') },
        { path: 'operations', name: 'operations', component: () => import('@/views/OperationsView.vue') },
        { path: 'operations/approvals', name: 'approvals', component: () => import('@/views/ApprovalsView.vue') },
        { path: 'artifacts/media-policies', name: 'media-policies', component: () => import('@/views/MediaPoliciesView.vue') },
        { path: 'security/audit', name: 'audit', component: () => import('@/views/AuditComplianceView.vue') },
        { path: 'security/compliance', name: 'compliance', component: () => import('@/views/AuditComplianceView.vue') },
        { path: 'settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
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
