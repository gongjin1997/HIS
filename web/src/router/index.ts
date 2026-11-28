import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true, title: '登录' },
  },
  {
    path: '/',
    component: () => import('@/components/AppShell.vue'),
    redirect: '/dashboard',
    children: [
      { path: 'dashboard', name: 'dashboard', component: () => import('@/views/DashboardView.vue'), meta: { title: '资产与运维全景' } },
      { path: 'cmdb/models', name: 'cmdb-models', component: () => import('@/views/CmdbModelsView.vue'), meta: { title: '元模型与关系' } },
      { path: 'cmdb/topology', name: 'cmdb-topology', component: () => import('@/views/CmdbTopologyView.vue'), meta: { title: '双拓扑与模板' } },
      { path: 'cmdb/resources', name: 'cmdb-resources', component: () => import('@/views/CmdbResourcesView.vue'), meta: { title: '统一资产台账' } },
      { path: 'cmdb/dcim', name: 'cmdb-dcim', component: () => import('@/views/CmdbDcimView.vue'), meta: { title: '机柜与 IPAM' } },
      { path: 'config', name: 'config', component: () => import('@/views/ConfigCenterView.vue'), meta: { title: '多环境配置中心' } },
      { path: 'plugins', name: 'plugins', component: () => import('@/views/PluginsView.vue'), meta: { title: '声明式插件库' } },
      { path: 'delivery', name: 'delivery', component: () => import('@/views/DeliveryView.vue'), meta: { title: '编排与发布流水线' } },
      { path: 'ops', name: 'ops', component: () => import('@/views/OpsHubView.vue'), meta: { title: '执行舰队与制品' } },
      { path: 'governance', name: 'governance', component: () => import('@/views/GovernanceView.vue'), meta: { title: '权限审计与设置' } },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.meta.public) {
    if (auth.token && !auth.initialized) {
      await auth.fetchMe()
    }
    if (auth.user) return '/dashboard'
    return true
  }
  if (!auth.token) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (!auth.initialized) {
    const ok = await auth.fetchMe()
    if (!ok) return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})
