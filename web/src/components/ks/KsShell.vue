<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  Boxes,
  Building2,
  ChevronDown,
  Compass,
  FileCheck2,
  FolderKanban,
  Gauge,
  GitFork,
  KeyRound,
  Layers,
  LayoutGrid,
  LogOut,
  Moon,
  Network,
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
const megaEl = ref<HTMLElement | null>(null)

const iconMap: Record<string, Component> = {
  Activity,
  Boxes,
  Building2,
  Compass,
  FileCheck2,
  FolderKanban,
  Gauge,
  GitFork,
  KeyRound,
  Layers,
  LayoutGrid,
  Network,
  Server,
  ShieldCheck,
  Workflow,
}

const activeTier = computed<ConsoleTier>(() => {
  if (route.path.startsWith('/workspace')) return 'workspace'
  if (route.path.startsWith('/project')) return 'project'
  return 'platform'
})

const currentGroups = computed(() => TIER_MENUS[activeTier.value])
const tierInfo = computed(() => TIER_META[activeTier.value])

function navigateTier(tier: ConsoleTier) {
  megaOpen.value = false
  router.push(TIER_META[tier].defaultPath)
}

async function selectTenantAndGo(tenantId: string) {
  megaOpen.value = false
  await scope.switchTenant(tenantId)
  router.push('/workspace/overview')
}

async function selectProjectAndGo(projectId: string) {
  megaOpen.value = false
  await scope.switchProject(projectId)
  router.push('/project/overview')
}

async function onTenantChange(id: string) {
  await scope.switchTenant(id)
}

async function onAppChange(id: string) {
  await scope.switchApp(id)
}

async function onProjectChange(id: string) {
  await scope.switchProject(id)
}

async function handleLogout() {
  await scope.logout()
  router.push('/login')
}

function onDocClick(ev: MouseEvent) {
  if (megaOpen.value && megaEl.value && !megaEl.value.contains(ev.target as Node)) {
    megaOpen.value = false
  }
}

onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div class="ks-layout">
    <!-- Authentic KubeSphere 64px Global Top Bar -->
    <header class="ks-topbar">
      <div class="ks-topbar__left">
        <!-- Iconic KubeSphere Platform ▾ Mega-Menu Switcher -->
        <div ref="megaEl" class="ks-mega-anchor">
          <button
            type="button"
            class="ks-platform-trigger"
            data-testid="ks-platform-switcher"
            :aria-expanded="megaOpen"
            @click.stop="megaOpen = !megaOpen"
          >
            <span class="ks-brand-mark">
              <span class="ks-brand-dot" />
            </span>
            <span class="ks-platform-trigger__label">{{ tierInfo.title }}</span>
            <ChevronDown :size="15" class="ks-platform-trigger__caret" :class="{ 'is-open': megaOpen }" />
          </button>

          <!-- Dark Slate 3-Column KubeSphere Mega-Menu -->
          <div v-if="megaOpen" class="ks-mega-panel" data-testid="ks-mega-menu">
            <div class="ks-mega-panel__col">
              <div class="ks-mega-panel__heading">控制台层级 (Console Scopes)</div>
              <button
                v-for="tierKey in (['platform', 'workspace', 'project'] as ConsoleTier[])"
                :key="tierKey"
                type="button"
                class="ks-mega-item"
                :class="{ 'is-active': activeTier === tierKey }"
                :data-testid="`ks-tier-${tierKey}`"
                @click="navigateTier(tierKey)"
              >
                <component :is="iconMap[TIER_META[tierKey].icon]" :size="17" />
                <div class="ks-mega-item__body">
                  <div class="ks-mega-item__title">{{ TIER_META[tierKey].title }}</div>
                  <div class="ks-mega-item__sub">{{ TIER_META[tierKey].subtitle }}</div>
                </div>
              </button>
            </div>

            <div class="ks-mega-panel__col">
              <div class="ks-mega-panel__heading">企业空间 / 租户 ({{ scope.tenants.length }})</div>
              <button
                v-for="t in scope.tenants"
                :key="t.tenant_id"
                type="button"
                class="ks-mega-chip"
                :class="{ 'is-active': t.tenant_id === scope.tenantId }"
                @click="selectTenantAndGo(t.tenant_id)"
              >
                <Building2 :size="15" />
                <span>{{ t.name }}</span>
                <span class="ks-mega-code">{{ t.code }}</span>
              </button>
            </div>

            <div class="ks-mega-panel__col">
              <div class="ks-mega-panel__heading">当前应用项目 ({{ scope.projects.length }})</div>
              <button
                v-for="p in scope.projects"
                :key="p.project_id"
                type="button"
                class="ks-mega-chip"
                :class="{ 'is-active': p.project_id === scope.projectId }"
                @click="selectProjectAndGo(p.project_id)"
              >
                <FolderKanban :size="15" />
                <span>{{ p.name }}</span>
                <span class="ks-mega-code">{{ p.code }}</span>
              </button>
            </div>
          </div>
        </div>

        <!-- Live Multi-Tenant Scope Breadcrumb Pills -->
        <div class="ks-scope-bar">
          <div class="ks-scope-select">
            <span class="ks-scope-select__k">租户</span>
            <select
              class="ks-native-select"
              :value="scope.tenantId"
              aria-label="切换企业空间租户"
              @change="onTenantChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="t in scope.tenants" :key="t.tenant_id" :value="t.tenant_id">
                {{ t.name }}
              </option>
            </select>
          </div>
          <span class="ks-scope-sep">/</span>
          <div class="ks-scope-select">
            <span class="ks-scope-select__k">应用</span>
            <select
              class="ks-native-select"
              :value="scope.appId"
              aria-label="切换应用"
              @change="onAppChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="a in scope.apps" :key="a.app_id" :value="a.app_id">
                {{ a.name }}
              </option>
            </select>
          </div>
          <span v-if="scope.projects.length > 0" class="ks-scope-sep">/</span>
          <div v-if="scope.projects.length > 0" class="ks-scope-select">
            <span class="ks-scope-select__k">项目</span>
            <select
              class="ks-native-select"
              :value="scope.projectId"
              aria-label="切换项目"
              @change="onProjectChange(($event.target as HTMLSelectElement).value)"
            >
              <option v-for="p in scope.projects" :key="p.project_id" :value="p.project_id">
                {{ p.name }}
              </option>
            </select>
          </div>
        </div>
      </div>

      <!-- Brand Centerpiece -->
      <div class="ks-topbar__center">
        <span class="ks-logo-wordmark">
          <span class="ks-logo-wordmark__accent">LiveOps</span>
          <span class="ks-logo-wordmark__sub">Foundation</span>
        </span>
      </div>

      <!-- Right Utility Controls -->
      <div class="ks-topbar__right">
        <div class="ks-probe-pill" title="Kubernetes /readyz & /livez 探针健康">
          <span class="ki-dot ki-dot-pulse ks-probe-dot" />
          <span>Ready</span>
        </div>

        <button
          type="button"
          class="ks-icon-btn"
          :title="scope.theme === 'light' ? '切换深色夜间模式' : '切换 KubeSphere 亮色模式'"
          @click="scope.toggleTheme()"
        >
          <Moon v-if="scope.theme === 'light'" :size="16" />
          <Sun v-else :size="16" />
        </button>

        <div class="ks-user-pill">
          <span class="ks-user-avatar">{{ (scope.context?.displayName || 'A').slice(0, 1).toUpperCase() }}</span>
          <span class="ks-user-name">{{ scope.context?.displayName || scope.context?.username || 'Admin' }}</span>
          <button type="button" class="ks-logout-btn" title="退出登录" @click="handleLogout">
            <LogOut :size="14" />
          </button>
        </div>
      </div>
    </header>

    <!-- Main Body: Left Context Sidebar + Canvas Workspace -->
    <div class="ks-body">
      <aside class="ks-sidebar">
        <!-- Context Scope Identity Card -->
        <div class="ks-scope-card">
          <div class="ks-scope-card__tier">
            <span class="ks-scope-card__dot" />
            <span>{{ tierInfo.title }}</span>
          </div>
          <div class="ks-scope-card__name">
            <template v-if="activeTier === 'platform'">LiveOps 全局底座</template>
            <template v-else-if="activeTier === 'workspace'">{{ scope.currentTenant?.name || scope.tenantId }}</template>
            <template v-else>{{ scope.currentProject?.name || scope.currentApp?.name || scope.projectId }}</template>
          </div>
          <div class="ks-scope-card__meta ki-mono">
            {{ scope.tenantId }} · {{ scope.appId }}
          </div>
        </div>

        <!-- Quick 3-Tier Switcher Tabs inside Sidebar -->
        <div class="ks-tier-pills">
          <button
            type="button"
            class="ks-tier-pill"
            :class="{ 'is-active': activeTier === 'platform' }"
            @click="navigateTier('platform')"
          >
            平台
          </button>
          <button
            type="button"
            class="ks-tier-pill"
            :class="{ 'is-active': activeTier === 'workspace' }"
            @click="navigateTier('workspace')"
          >
            空间
          </button>
          <button
            type="button"
            class="ks-tier-pill"
            :class="{ 'is-active': activeTier === 'project' }"
            @click="navigateTier('project')"
          >
            项目
          </button>
        </div>

        <!-- Navigation Groups -->
        <nav class="ks-nav" aria-label="主导航">
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
              <span class="ks-nav-item__label">{{ item.label }}</span>
            </router-link>
          </div>
        </nav>

        <div class="ks-sidebar__foot">
          <div class="ks-foot-row">
            <span>Scope Ver</span>
            <span class="ki-mono">v{{ scope.context?.scopeVersion || 1 }}</span>
          </div>
          <div class="ks-foot-row">
            <span>Isolation</span>
            <span class="ki-mono">Composite RLS</span>
          </div>
        </div>
      </aside>

      <main class="ks-main">
        <div class="ks-main__inner">
          <router-view />
        </div>
      </main>
    </div>
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
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-4);
  background-color: var(--ki-ks-topbar-bg);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ks-topbar__left {
  display: flex;
  align-items: center;
  gap: var(--ki-space-3);
  min-width: 0;
}

.ks-mega-anchor {
  position: relative;
}

.ks-platform-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 14px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-ks-nav-pill-bg);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-body);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
  box-shadow: var(--ki-shadow-surface);
  transition: border-color var(--ki-dur-fast) var(--ki-ease),
    box-shadow var(--ki-dur-fast) var(--ki-ease);
}

.ks-platform-trigger:hover {
  border-color: var(--ki-border-strong);
  box-shadow: var(--ki-shadow-hover);
}

.ks-brand-mark {
  width: 20px;
  height: 20px;
  border-radius: var(--ki-radius-xs);
  background: var(--ki-brand-logo-bg);
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.ks-brand-dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-text-on-solid);
}

.ks-platform-trigger__caret {
  color: var(--ki-text-muted);
  transition: transform var(--ki-dur-fast) var(--ki-ease);
}

.ks-platform-trigger__caret.is-open {
  transform: rotate(180deg);
}

/* Authentic Dark KubeSphere Mega-Menu */
.ks-mega-panel {
  position: absolute;
  top: calc(100% + 10px);
  left: 0;
  z-index: var(--ki-z-dropdown);
  width: 740px;
  max-width: calc(100vw - 32px);
  padding: 18px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-ks-mega-bg);
  color: var(--ki-ks-mega-text);
  box-shadow: var(--ki-shadow-modal);
  display: grid;
  grid-template-columns: 1.25fr 1fr 1fr;
  gap: 16px;
}

.ks-mega-panel__col {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ks-mega-panel__heading {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-ks-mega-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 0 6px 4px;
}

.ks-mega-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px;
  border-radius: var(--ki-radius-md);
  border: 1px solid transparent;
  background: transparent;
  color: var(--ki-ks-mega-text);
  text-align: left;
  cursor: pointer;
  transition: background-color var(--ki-dur-fast) var(--ki-ease);
}

.ks-mega-item:hover,
.ks-mega-item.is-active {
  background-color: var(--ki-ks-mega-surface);
  border-color: var(--ki-ks-green);
}

.ks-mega-item__title {
  font-size: var(--ki-fs-body);
  font-weight: var(--ki-fw-semibold);
}

.ks-mega-item__sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-ks-mega-muted);
  margin-top: 2px;
}

.ks-mega-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--ki-radius-md);
  border: 1px solid transparent;
  background-color: var(--ki-ks-mega-surface);
  color: var(--ki-ks-mega-text);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
  text-align: left;
}

.ks-mega-chip.is-active {
  border-color: var(--ki-ks-green);
}

.ks-mega-code {
  margin-left: auto;
  font-family: var(--ki-font-mono);
  font-size: var(--ki-fs-xs);
  color: var(--ki-ks-mega-muted);
}

.ks-scope-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
}

.ks-scope-select {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.ks-scope-select__k {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-native-select {
  border: none;
  background: transparent;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  font-family: var(--ki-font-sans);
  cursor: pointer;
  padding: 2px 4px;
  max-width: 148px;
}

.ks-scope-sep {
  color: var(--ki-border-strong);
  font-size: var(--ki-fs-sm);
}

.ks-topbar__center {
  display: none;
}

@media (min-width: 1280px) {
  .ks-topbar__center {
    display: flex;
    align-items: center;
  }
}

.ks-logo-wordmark {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-size: 16px;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
  color: var(--ki-text-primary);
}

.ks-logo-wordmark__accent {
  color: var(--ki-brand);
}

.ks-logo-wordmark__sub {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-muted);
}

.ks-topbar__right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
}

.ks-probe-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 10px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
}

.ks-probe-dot {
  background-color: var(--ki-ks-green);
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

.ks-user-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 8px 0 4px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
}

.ks-user-avatar {
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

.ks-user-name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-primary);
}

.ks-logout-btn {
  border: none;
  background: transparent;
  color: var(--ki-text-muted);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  padding: 4px;
  border-radius: var(--ki-radius-xs);
}

.ks-logout-btn:hover {
  color: var(--ki-status-danger-text);
  background-color: var(--ki-status-danger-subtle);
}

/* Body Shell */
.ks-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.ks-sidebar {
  width: var(--ki-sidebar-w);
  flex: none;
  padding: 16px 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background-color: var(--ki-bg-canvas);
}

.ks-scope-card {
  padding: 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-surface);
}

.ks-scope-card__tier {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-status-ok-text);
}

.ks-scope-card__dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-ks-green);
}

.ks-scope-card__name {
  margin-top: 6px;
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ks-scope-card__meta {
  margin-top: 4px;
  font-size: 11px;
  color: var(--ki-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ks-tier-pills {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
  padding: 3px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
}

.ks-tier-pill {
  height: 28px;
  border: none;
  border-radius: var(--ki-radius-xs);
  background: transparent;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ks-tier-pill.is-active {
  background-color: var(--ki-brand-solid);
  color: var(--ki-text-on-solid);
  font-weight: var(--ki-fw-semibold);
}

.ks-nav {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ks-nav-group__title {
  padding: 0 10px 6px;
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
}

.ks-nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: var(--ki-menu-item-h);
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

.ks-sidebar__foot {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-subtle);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ks-foot-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-main {
  flex: 1;
  min-width: 0;
  padding: 16px 24px 28px 10px;
}

.ks-main__inner {
  max-width: var(--ki-content-max);
  margin: 0 auto;
}
</style>
