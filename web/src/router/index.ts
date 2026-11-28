import { createRouter, createWebHistory } from 'vue-router'
import { useScopeStore } from '@/stores/scope'
import KsShell from '@/components/ks/KsShell.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: KsShell,
      redirect: '/home',
      children: [
        // Authentic KubeSphere Portal Entry
        { path: 'home', name: 'home', component: () => import('@/views/HomePortalView.vue') },

        // Tier 1: Platform Management
        { path: 'platform/overview', name: 'platform-overview', component: () => import('@/views/PlatformOverviewView.vue') },
        { path: 'platform/tenants', name: 'platform-tenants', component: () => import('@/views/PlatformTenantsView.vue') },
        { path: 'platform/access', name: 'platform-access', component: () => import('@/views/PlatformAccessView.vue') },
        { path: 'platform/nodes', name: 'platform-nodes', component: () => import('@/views/PlatformNodesView.vue') },
        { path: 'platform/gateway', name: 'platform-gateway', component: () => import('@/views/PlatformGatewayView.vue') },
        { path: 'platform/audit', name: 'platform-audit', component: () => import('@/views/PlatformAuditView.vue') },

        // Tier 2: Workspace
        { path: 'workspace/overview', name: 'workspace-overview', component: () => import('@/views/WorkspaceOverviewView.vue') },
        { path: 'workspace/apps', name: 'workspace-apps', component: () => import('@/views/WorkspaceAppsView.vue') },
        { path: 'workspace/projects', name: 'workspace-projects', component: () => import('@/views/WorkspaceProjectsView.vue') },

        // Tier 3: Project Workbench
        { path: 'project/overview', name: 'project-overview', component: () => import('@/views/ProjectOverviewView.vue') },
        { path: 'project/assets', name: 'project-assets', component: () => import('@/views/ProjectAssetsView.vue') },
        { path: 'project/configs', name: 'project-configs', component: () => import('@/views/ProjectConfigsView.vue') },
        { path: 'project/jobs', name: 'project-jobs', component: () => import('@/views/ProjectJobsView.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/home' },
  ],
})

router.beforeEach(async (to) => {
  const scope = useScopeStore()
  if (!scope.initialized) {
    await scope.refreshMe()
  }
  if (!to.meta.public && !scope.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && scope.isAuthenticated) {
    return { path: '/home' }
  }
  return true
})

export default router
