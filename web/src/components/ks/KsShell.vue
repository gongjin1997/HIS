<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  Boxes,
  Building2,
  ChevronDown,
  Compass,
  FileCheck2,
  FolderKanban,
  Gauge,
  GitFork,
  Home,
  KeyRound,
  Layers,
  LayoutGrid,
  LogOut,
  Moon,
  Network,
  Search,
  Server,
  ShieldCheck,
  Sun,
  Workflow,
} from 'lucide-vue-next'
import type { Component } from 'vue'
import { TIER_MENUS, TIER_META } from '@/config/menu'
import { useScopeStore, type ConsoleTier } from '@/stores/scope'

const route = useRoute()
const router = useRouter()
const scope = useScopeStore()

const megaOpen = ref(false)
const userOpen = ref(false)
const cmdOpen = ref(false)
const cmdQuery = ref('')
const megaEl = ref<HTMLElement | null>(null)
const userEl = ref<HTMLElement | null>(null)

const iconMap: Record<string, Component> = {
  Activity,
  Boxes,
  Building2,
  Compass,
  FileCheck2,
  FolderKanban,
  Gauge,
  GitFork,
  Home,
  KeyRound,
  Layers,
  LayoutGrid,
  Network,
  Server,
  ShieldCheck,
  Workflow,
}

const isPortalHome = computed(() => route.path === '/home')

const activeTier = computed<ConsoleTier>(() => {
  if (route.path.startsWith('/workspace')) return 'workspace'
  if (route.path.startsWith('/project')) return 'project'
  return 'platform'
})

const currentGroups = computed(() => TIER_MENUS[activeTier.value])
const tierInfo = computed(() => TIER_META[activeTier.value])

const allCommands = computed(() => {
  const list: Array<{ label: string; group: string; path: string; icon: string }> = [
    { label: '控制台首页', group: '全局导航', path: '/home', icon: 'Home' },
  ]
  for (const tier of ['platform', 'workspace', 'project'] as ConsoleTier[]) {
    for (const g of TIER_MENUS[tier]) {
      for (const item of g.items) {
        list.push({ label: item.label, group: TIER_META[tier].title, path: item.path, icon: item.icon })
      }
    }
  }
  const q = cmdQuery.value.trim().toLowerCase()
  if (!q) return list
  return list.filter((c) => c.label.toLowerCase().includes(q) || c.group.toLowerCase().includes(q))
})

function go(path: string) {
  megaOpen.value = false
  cmdOpen.value = false
  userOpen.value = false
  router.push(path)
}

async function selectWorkspace(tenantId: string) {
  megaOpen.value = false
  await scope.switchTenant(tenantId)
  router.push('/workspace/overview')
}

async function selectProject(projectId: string) {
  megaOpen.value = false
  await scope.switchProject(projectId)
  router.push('/project/overview')
}

async function onSidebarContextChange(val: string) {
  if (activeTier.value === 'workspace') {
    await scope.switchTenant(val)
  } else if (activeTier.value === 'project') {
    await scope.switchProject(val)
  }
}

async function onSidebarAppChange(val: string) {
  await scope.switchApp(val)
}

async function handleLogout() {
  userOpen.value = false
  await scope.logout()
  router.push('/login')
}

function onDocClick(ev: MouseEvent) {
  const target = ev.target as Node
  if (megaOpen.value && megaEl.value && !megaEl.value.contains(target)) megaOpen.value = false
  if (userOpen.value && userEl.value && !userEl.value.contains(target)) userOpen.value = false
}

function onKeyDown(ev: KeyboardEvent) {
  if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === 'k') {
    ev.preventDefault()
    cmdOpen.value = !cmdOpen.value
    cmdQuery.value = ''
  } else if (ev.key === 'Escape') {
    megaOpen.value = false
    userOpen.value = false
    cmdOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKeyDown)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKeyDown)
})
</script>

<template>
  <div class="ks-layout">
    <!-- Authentic KubeSphere 4.x 64px Top Header -->
    <header class="ks-topbar">
      <div class="ks-topbar__left">
        <div ref="megaEl" class="ks-mega-anchor">
          <button
            type="button"
            class="ks-platform-btn"
            data-testid="ks-platform-switcher"
            :aria-expanded="megaOpen"
            @click.stop="megaOpen = !megaOpen"
          >
            <LayoutGrid :size="16" class="ks-platform-btn__icon" />
            <span>{{ isPortalHome ? '平台管理' : tierInfo.title }}</span>
            <ChevronDown :size="14" class="ks-caret" :class="{ 'is-open': megaOpen }" />
          </button>

          <!-- Clean KubeSphere Global Navigation Popover -->
          <div v-if="megaOpen" class="ks-mega-panel" data-testid="ks-mega-menu">
            <div class="ks-mega-col">
              <div class="ks-mega-heading">平台导航</div>
              <button type="button" class="ks-mega-link" :class="{ 'is-active': isPortalHome }" @click="go('/home')">
                <Home :size="16" />
                <div>
                  <div class="ks-mega-link__title">控制台首页</div>
                  <div class="ks-mega-link__desc">企业空间入口与平台运行概况</div>
                </div>
              </button>
              <button
                type="button"
                class="ks-mega-link"
                :class="{ 'is-active': !isPortalHome && activeTier === 'platform' }"
                data-testid="ks-tier-platform"
                @click="go('/platform/overview')"
              >
                <LayoutGrid :size="16" />
                <div>
                  <div class="ks-mega-link__title">平台管理</div>
                  <div class="ks-mega-link__desc">节点池、访问控制、动态网关与审计</div>
                </div>
              </button>
              <button
                type="button"
                class="ks-mega-link"
                :class="{ 'is-active': activeTier === 'workspace' }"
                data-testid="ks-tier-workspace"
                @click="go('/workspace/overview')"
              >
                <Building2 :size="16" />
                <div>
                  <div class="ks-mega-link__title">企业空间</div>
                  <div class="ks-mega-link__desc">管理空间内的应用与项目团队</div>
                </div>
              </button>
              <button
                type="button"
                class="ks-mega-link"
                :class="{ 'is-active': activeTier === 'project' }"
                data-testid="ks-tier-project"
                @click="go('/project/overview')"
              >
                <Layers :size="16" />
                <div>
                  <div class="ks-mega-link__title">项目工作台</div>
                  <div class="ks-mega-link__desc">资产库、配置中心与发布编排</div>
                </div>
              </button>
            </div>

            <div class="ks-mega-col">
              <div class="ks-mega-heading">企业空间 ({{ scope.tenants.length }})</div>
              <button
                v-for="t in scope.tenants"
                :key="t.tenant_id"
                type="button"
                class="ks-mega-item"
                :class="{ 'is-active': t.tenant_id === scope.tenantId }"
                @click="selectWorkspace(t.tenant_id)"
              >
                <Building2 :size="15" />
                <span class="ks-mega-item__name">{{ t.name }}</span>
                <span class="ks-mega-item__tag">{{ t.code }}</span>
              </button>
            </div>

            <div class="ks-mega-col">
              <div class="ks-mega-heading">项目快速直达 ({{ scope.projects.length }})</div>
              <button
                v-for="p in scope.projects"
                :key="p.project_id"
                type="button"
                class="ks-mega-item"
                :class="{ 'is-active': p.project_id === scope.projectId }"
                @click="selectProject(p.project_id)"
              >
                <FolderKanban :size="15" />
                <span class="ks-mega-item__name">{{ p.name }}</span>
                <span class="ks-mega-item__tag">{{ p.code }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- Subtle Contextual Breadcrumb (only when nested inside Workspace / Project) -->
        <nav v-if="!isPortalHome" class="ks-crumb" aria-label="当前位置">
          <button type="button" class="ks-crumb__btn" @click="go('/home')">首页</button>
          <span class="ks-crumb__sep">/</span>
          <template v-if="activeTier === 'platform'">
            <span class="ks-crumb__cur">平台管理</span>
          </template>
          <template v-else-if="activeTier === 'workspace'">
            <span class="ks-crumb__cur">{{ scope.currentTenant?.name || scope.tenantId }}</span>
          </template>
          <template v-else>
            <button type="button" class="ks-crumb__btn" @click="go('/workspace/overview')">
              {{ scope.currentTenant?.name || scope.tenantId }}
            </button>
            <span class="ks-crumb__sep">/</span>
            <span class="ks-crumb__cur">{{ scope.currentProject?.name || scope.currentApp?.name }}</span>
          </template>
        </nav>
      </div>

      <!-- Authentic KubeSphere Centered Brand Wordmark -->
      <button type="button" class="ks-brand" @click="go('/home')">
        <span class="ks-brand__cube" />
        <span class="ks-brand__title">LiveOps</span>
        <span class="ks-brand__edition">Console</span>
      </button>

      <!-- Clean Right Utilities -->
      <div class="ks-topbar__right">
        <button type="button" class="ks-search-trigger" @click="cmdOpen = true">
          <Search :size="14" />
          <span>快速跳转</span>
          <kbd class="ks-kbd">⌘K</kbd>
        </button>

        <button
          type="button"
          class="ks-icon-btn"
          :title="scope.theme === 'light' ? '切换深色模式' : '切换亮色模式'"
          @click="scope.toggleTheme()"
        >
          <Moon v-if="scope.theme === 'light'" :size="16" />
          <Sun v-else :size="16" />
        </button>

        <div ref="userEl" class="ks-user-anchor">
          <button type="button" class="ks-user-btn" @click.stop="userOpen = !userOpen">
            <span class="ks-avatar">{{ (scope.context?.displayName || 'A').slice(0, 1).toUpperCase() }}</span>
            <span class="ks-user-btn__name">{{ scope.context?.displayName || scope.context?.username }}</span>
            <ChevronDown :size="13" class="ks-caret" />
          </button>

          <div v-if="userOpen" class="ks-user-menu">
            <div class="ks-user-menu__head">
              <div class="ks-user-menu__name">{{ scope.context?.displayName }}</div>
              <div class="ks-user-menu__role">{{ (scope.context?.roles || []).join(' · ') || '平台成员' }}</div>
            </div>
            <button type="button" class="ks-user-menu__item" @click="go('/platform/access')">
              <ShieldCheck :size="14" /><span>访问控制与角色</span>
            </button>
            <button type="button" class="ks-user-menu__item ks-user-menu__item--danger" @click="handleLogout">
              <LogOut :size="14" /><span>退出登录</span>
            </button>
          </div>
        </div>
      </div>
    </header>

    <!-- Main Body -->
    <div class="ks-body">
      <!-- Clean Context-Aware Left Sidebar (Hidden on Portal Home like real KubeSphere!) -->
      <aside v-if="!isPortalHome" class="ks-sidebar">
        <div class="ks-sidebar-head">
          <button
            type="button"
            class="ks-back-link"
            @click="go(activeTier === 'project' ? '/workspace/overview' : '/home')"
          >
            <ArrowLeft :size="13" />
            <span>{{ activeTier === 'project' ? '返回企业空间' : '返回控制台首页' }}</span>
          </button>

          <div v-if="activeTier === 'platform'" class="ks-ctx-box">
            <div class="ks-ctx-box__label">全局管理视图</div>
            <div class="ks-ctx-box__title">平台管理中心</div>
          </div>

          <div v-else-if="activeTier === 'workspace'" class="ks-ctx-box">
            <label class="ks-ctx-box__label" for="ks-ws-sel">当前企业空间</label>
            <select
              id="ks-ws-sel"
              class="ks-ctx-select"
              :value="scope.tenantId"
              @change="onSidebarContextChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="t in scope.tenants" :key="t.tenant_id" :value="t.tenant_id">{{ t.name }}</option>
            </select>
          </div>

          <div v-else class="ks-ctx-box">
            <label class="ks-ctx-box__label" for="ks-app-sel">应用集群</label>
            <select
              id="ks-app-sel"
              class="ks-ctx-select ks-ctx-select--sub"
              :value="scope.appId"
              @change="onSidebarAppChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="a in scope.apps" :key="a.app_id" :value="a.app_id">{{ a.name }}</option>
            </select>
            <label v-if="scope.projects.length > 0" class="ks-ctx-box__label ks-mt-sm" for="ks-prj-sel">当前项目</label>
            <select
              v-if="scope.projects.length > 0"
              id="ks-prj-sel"
              class="ks-ctx-select"
              :value="scope.projectId"
              @change="onSidebarContextChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="p in scope.projects" :key="p.project_id" :value="p.project_id">{{ p.name }}</option>
            </select>
          </div>
        </div>

        <nav class="ks-nav" aria-label="功能菜单">
          <div v-for="group in currentGroups" :key="group.title" class="ks-nav-group">
            <div class="ks-nav-group__title">{{ group.title }}</div>
            <router-link
              v-for="item in group.items"
              :key="item.path"
              :to="item.path"
              class="ks-nav-item"
              :class="{ 'is-active': route.path === item.path }"
            >
              <component :is="iconMap[item.icon]" :size="16" class="ks-nav-item__icon" />
              <span>{{ item.label }}</span>
            </router-link>
          </div>
        </nav>
      </aside>

      <main class="ks-main" :class="{ 'ks-main--portal': isPortalHome }">
        <div class="ks-main__inner" :class="{ 'ks-main__inner--portal': isPortalHome }">
          <router-view />
        </div>
      </main>
    </div>

    <!-- Command Palette (⌘K) -->
    <el-dialog v-model="cmdOpen" title="快速导航" width="480px" :show-close="true">
      <el-input v-model="cmdQuery" placeholder="搜索页面名称，如 资产库、配置中心、节点管理..." clearable autofocus />
      <div class="ks-cmd-list">
        <button
          v-for="c in allCommands"
          :key="c.path"
          type="button"
          class="ks-cmd-row"
          @click="go(c.path)"
        >
          <component :is="iconMap[c.icon]" :size="15" class="ks-cmd-icon" />
          <span class="ks-cmd-label">{{ c.label }}</span>
          <span class="ks-cmd-group">{{ c.group }}</span>
        </button>
      </div>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-layout {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  background-color: var(--ki-bg-canvas);
}

.ks-topbar {
  position: sticky;
  top: 0;
  z-index: var(--ki-z-shell);
  height: var(--ki-topbar-h);
  padding: 0 24px;
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  gap: 16px;
  background-color: var(--ki-ks-topbar-bg);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ks-topbar__left {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
}

.ks-mega-anchor,
.ks-user-anchor {
  position: relative;
}

.ks-platform-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 14px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
  box-shadow: var(--ki-shadow-surface);
  transition: border-color var(--ki-dur-fast) var(--ki-ease),
    box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.ks-platform-btn:hover {
  border-color: var(--ki-border-strong);
  box-shadow: var(--ki-shadow-hover);
}

.ks-platform-btn__icon {
  color: var(--ki-ks-green);
}

.ks-caret {
  color: var(--ki-text-muted);
  transition: transform var(--ki-dur-fast) var(--ki-ease);
}

.ks-caret.is-open {
  transform: rotate(180deg);
}

/* KubeSphere Dark Slate Global Mega-Menu */
.ks-mega-panel {
  position: absolute;
  top: calc(100% + 10px);
  left: 0;
  z-index: var(--ki-z-dropdown);
  width: 760px;
  max-width: calc(100vw - 32px);
  padding: 20px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-ks-mega-bg);
  color: var(--ki-ks-mega-text);
  box-shadow: var(--ki-shadow-modal);
  display: grid;
  grid-template-columns: 1.25fr 1fr 1fr;
  gap: 18px;
}

.ks-mega-col {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ks-mega-heading {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-ks-mega-muted);
  padding: 0 8px 6px;
}

.ks-mega-link {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid transparent;
  background: transparent;
  color: var(--ki-ks-mega-text);
  text-align: left;
  cursor: pointer;
}

.ks-mega-link:hover,
.ks-mega-link.is-active {
  background-color: var(--ki-ks-mega-surface);
  border-color: var(--ki-ks-green);
}

.ks-mega-link__title {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
}

.ks-mega-link__desc {
  font-size: var(--ki-fs-xs);
  color: var(--ki-ks-mega-muted);
  margin-top: 2px;
}

.ks-mega-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 10px;
  border-radius: var(--ki-radius-md);
  border: 1px solid transparent;
  background-color: var(--ki-ks-mega-surface);
  color: var(--ki-ks-mega-text);
  font-size: var(--ki-fs-sm);
  text-align: left;
  cursor: pointer;
}

.ks-mega-item:hover,
.ks-mega-item.is-active {
  border-color: var(--ki-ks-green);
}

.ks-mega-item__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ks-mega-item__tag {
  font-family: var(--ki-font-mono);
  font-size: 11px;
  color: var(--ki-ks-mega-muted);
}

.ks-crumb {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-sm);
  min-width: 0;
}

.ks-crumb__btn {
  border: none;
  background: transparent;
  color: var(--ki-text-muted);
  cursor: pointer;
  padding: 2px 4px;
  font-size: var(--ki-fs-sm);
}

.ks-crumb__btn:hover {
  color: var(--ki-text-primary);
}

.ks-crumb__sep {
  color: var(--ki-border-strong);
}

.ks-crumb__cur {
  color: var(--ki-text-primary);
  font-weight: var(--ki-fw-semibold);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Center Brand */
.ks-brand {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  border: none;
  background: transparent;
  cursor: pointer;
  padding: 4px 10px;
  border-radius: var(--ki-radius-pill);
}

.ks-brand__cube {
  width: 24px;
  height: 24px;
  border-radius: var(--ki-radius-md);
  background: var(--ki-brand-logo-bg);
  box-shadow: var(--ki-brand-logo-shadow);
}

.ks-brand__title {
  font-size: 17px;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
  color: var(--ki-text-primary);
}

.ks-brand__edition {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-muted);
}

/* Right Controls */
.ks-topbar__right {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.ks-search-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 12px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-muted);
  font-size: var(--ki-fs-xs);
  cursor: pointer;
}

.ks-search-trigger:hover {
  border-color: var(--ki-border-strong);
  color: var(--ki-text-primary);
}

.ks-kbd {
  padding: 1px 6px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-canvas);
  border: 1px solid var(--ki-border-default);
  font-family: var(--ki-font-mono);
  font-size: 11px;
  color: var(--ki-text-secondary);
}

.ks-icon-btn {
  width: 34px;
  height: 34px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.ks-icon-btn:hover {
  color: var(--ki-text-primary);
  border-color: var(--ki-border-strong);
}

.ks-user-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 10px 0 4px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  cursor: pointer;
}

.ks-avatar {
  width: 26px;
  height: 26px;
  border-radius: var(--ki-radius-pill);
  background: var(--ki-avatar-bg);
  color: var(--ki-text-on-solid);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-bold);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.ks-user-btn__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
}

.ks-user-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: var(--ki-z-dropdown);
  width: 210px;
  padding: 8px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-bg-elevated);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-popover);
}

.ks-user-menu__head {
  padding: 8px 10px 10px;
  border-bottom: 1px solid var(--ki-border-subtle);
  margin-bottom: 6px;
}

.ks-user-menu__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-user-menu__role {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-user-menu__item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border: none;
  border-radius: var(--ki-radius-md);
  background: transparent;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
  text-align: left;
}

.ks-user-menu__item:hover {
  background-color: var(--ki-bg-hover);
}

.ks-user-menu__item--danger:hover {
  background-color: var(--ki-status-danger-subtle);
  color: var(--ki-status-danger-text);
}

/* Body & Pure Left Sidebar */
.ks-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.ks-sidebar {
  width: 224px;
  flex: none;
  padding: 16px 14px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ks-sidebar-head {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ks-back-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: transparent;
  color: var(--ki-text-muted);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  padding: 2px 6px;
  cursor: pointer;
  align-self: flex-start;
}

.ks-back-link:hover {
  color: var(--ki-text-primary);
}

.ks-ctx-box {
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-surface);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ks-ctx-box__label {
  font-size: 11px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
}

.ks-ctx-box__title {
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-ctx-select {
  width: 100%;
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  padding: 5px 8px;
  cursor: pointer;
}

.ks-ctx-select--sub {
  font-weight: var(--ki-fw-medium);
  font-size: var(--ki-fs-xs);
}

.ks-mt-sm {
  margin-top: 6px;
}

.ks-nav {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ks-nav-group__title {
  padding: 0 10px 6px;
  font-size: 11px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
}

.ks-nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 38px;
  padding: 0 12px;
  border-radius: var(--ki-radius-md);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-body);
  font-weight: var(--ki-fw-medium);
  text-decoration: none;
  transition: background-color var(--ki-dur-fast) var(--ki-ease),
    color var(--ki-dur-fast) var(--ki-ease);
}

.ks-nav-item:hover {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  text-decoration: none;
}

.ks-nav-item.is-active {
  background-color: var(--ki-sider-active-bg);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.ks-nav-item.is-active .ks-nav-item__icon {
  color: var(--ki-ks-green);
}

.ks-main {
  flex: 1;
  min-width: 0;
  padding: 16px 24px 32px 10px;
}

.ks-main--portal {
  padding: 24px 32px 40px;
}

.ks-main__inner {
  max-width: var(--ki-content-max);
  margin: 0 auto;
}

.ks-main__inner--portal {
  max-width: 1240px;
}

/* Command Palette */
.ks-cmd-list {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 320px;
  overflow-y: auto;
}

.ks-cmd-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border: none;
  border-radius: var(--ki-radius-md);
  background: transparent;
  color: var(--ki-text-primary);
  cursor: pointer;
  text-align: left;
}

.ks-cmd-row:hover {
  background-color: var(--ki-bg-hover);
}

.ks-cmd-icon {
  color: var(--ki-brand);
}

.ks-cmd-label {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  flex: 1;
}

.ks-cmd-group {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}
</style>
