<script setup lang="ts">
import { computed, onMounted, provide, ref, useTemplateRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Bell,
  ChevronDown,
  FolderKanban,
  Keyboard,
  Languages,
  LogOut,
  Maximize2,
  Menu,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  Sun,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useShortcuts } from '@/composables/useShortcuts'
import { ALL_MENU_ITEMS, GROUP_OF_PATH, MENU_GROUPS, TITLE_OF_PATH } from '@/config/menu'
import { pageHeaderTargetKey } from '@/components/PageHeader.vue'
import WorkspaceNav from '@/components/WorkspaceNav.vue'
import KiCommandPalette, { type CommandItem } from '@/components/ki/KiCommandPalette.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const showCmdK = ref(false)
const showHelp = ref(false)
const pageHeaderTarget = useTemplateRef<HTMLElement>('pageHeaderTarget')
provide(pageHeaderTargetKey, pageHeaderTarget)

const currentGroup = computed(() => GROUP_OF_PATH[route.path] || '工作台')
const currentTitle = computed(() => TITLE_OF_PATH[route.path] || (route.meta.title as string) || '总览驾驶舱')
const currentProjectName = computed(
  () => ws.projects.find((p) => p.id === ws.currentProjectId)?.name || '默认项目'
)
const userInitial = computed(() => (auth.user?.displayName || auth.user?.username || 'A').slice(0, 1).toUpperCase())
const envLabelMap: Record<string, string> = {
  prod: 'PROD 生产',
  staging: 'STAGING 预发',
  dev: 'DEV 开发',
  sandbox: 'SANDBOX 沙箱',
}

const commandItems = computed<CommandItem[]>(() =>
  ALL_MENU_ITEMS.map((m) => ({
    id: m.path,
    label: m.title,
    path: m.path,
    icon: m.icon,
    keywords: m.key,
  }))
)

useShortcuts({
  onCommandPalette: () => {
    showCmdK.value = !showCmdK.value
  },
  onHelpModal: () => {
    showHelp.value = !showHelp.value
  },
  onFocusSearch: () => {
    showCmdK.value = true
  },
})

function onCommandSelect(item: CommandItem) {
  if (item.path) void router.push(item.path)
}

function onProjectSelect(id: string) {
  ws.currentProjectId = id
  void ws.refreshApprovalBadge()
}

function onEnvSelect(env: 'prod' | 'staging' | 'dev' | 'sandbox') {
  ws.currentEnv = env
}

function onUserCommand(cmd: string) {
  if (cmd === 'settings') void router.push('/settings')
  else if (cmd === 'shortcuts') showHelp.value = true
  else if (cmd === 'logout') {
    auth.logout()
    void router.push('/login')
  }
}

onMounted(async () => {
  await ws.loadProjects()
  await ws.refreshApprovalBadge()
})
</script>

<template>
  <el-container class="ki-layout" :class="{ 'is-collapsed': ws.sidebarCollapsed }">
    <!-- 左侧浅色分组导航栏（对标 HCS BasicLayout：220px / 64px） -->
    <el-aside
      class="ki-sider ki-shell-sidebar"
      :width="ws.sidebarCollapsed ? 'var(--ki-sidebar-collapsed-w)' : 'var(--ki-sidebar-w)'"
    >
      <div class="ki-logo" @click="router.push('/dashboard')">
        <span class="ki-logo-mark" aria-hidden="true">
          <svg viewBox="0 0 32 32" width="30" height="30">
            <rect width="32" height="32" rx="8" fill="currentColor" />
            <path d="M16 6l8 3v6c0 5-3.4 9.3-8 11-4.6-1.7-8-6-8-11V9z" fill="none" stroke="var(--ki-text-on-solid)" stroke-width="2" />
            <path d="M12 16l3 3 5-6" fill="none" stroke="var(--ki-text-on-solid)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </span>
        <div v-show="!ws.sidebarCollapsed" class="ki-brand">
          <b>kinstall 交付平台</b>
          <span>Enterprise Offline Ops</span>
        </div>
      </div>

      <el-scrollbar class="ki-menu-scroll">
        <nav class="ki-menu" aria-label="主导航">
          <template v-for="group in MENU_GROUPS" :key="group.title">
            <div v-show="!ws.sidebarCollapsed" class="ki-group-title">{{ group.title }}</div>
            <div v-show="ws.sidebarCollapsed" class="ki-group-sep" />
            <el-tooltip
              v-for="item in group.items"
              :key="item.path"
              :content="item.title"
              placement="right"
              :disabled="!ws.sidebarCollapsed"
            >
              <router-link
                :to="item.path"
                class="ki-menu-item"
                :class="{ 'is-active': route.path === item.path }"
                :aria-current="route.path === item.path ? 'page' : undefined"
              >
                <component :is="item.icon" :size="17" class="ki-menu-icon" aria-hidden="true" />
                <span v-show="!ws.sidebarCollapsed" class="ki-menu-label">{{ item.title }}</span>
                <em
                  v-if="item.badge === 'approvals' && ws.pendingApprovalCount > 0"
                  class="ki-menu-badge ki-num"
                >
                  {{ ws.pendingApprovalCount > 99 ? '99+' : ws.pendingApprovalCount }}
                </em>
              </router-link>
            </el-tooltip>
          </template>
        </nav>
      </el-scrollbar>

      <div class="ki-sider-foot">
        <el-button
          text
          circle
          :aria-label="ws.sidebarCollapsed ? '展开导航栏' : '折叠导航栏'"
          @click="ws.sidebarCollapsed = !ws.sidebarCollapsed"
        >
          <PanelLeftOpen v-if="ws.sidebarCollapsed" :size="16" />
          <PanelLeftClose v-else :size="16" />
        </el-button>
      </div>
    </el-aside>

    <!-- 右侧主区域 -->
    <el-container direction="vertical" class="ki-main-wrap">
      <!-- 顶部全局工具栏 56px -->
      <el-header class="ki-header ki-shell-topbar" height="auto">
        <div class="ki-toolbar">
          <div class="ki-toolbar-left">
            <el-dropdown class="ki-mobile-nav" trigger="click" @command="(p: string) => router.push(p)">
              <el-button text circle aria-label="打开导航菜单">
                <Menu :size="18" />
              </el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-for="m in ALL_MENU_ITEMS" :key="m.path" :command="m.path">
                    {{ m.title }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>

            <el-breadcrumb separator="/">
              <el-breadcrumb-item>{{ currentGroup }}</el-breadcrumb-item>
              <el-breadcrumb-item>
                <span class="ki-crumb-title" role="heading" aria-level="1">{{ currentTitle }}</span>
              </el-breadcrumb-item>
            </el-breadcrumb>
          </div>

          <button type="button" class="ki-search-trigger" @click="showCmdK = true">
            <Search :size="14" class="ki-muted" aria-hidden="true" />
            <span class="ki-search-placeholder">搜索模块 / 主机 / 实例…</span>
            <kbd class="ki-kbd ki-mono">/</kbd>
          </button>

          <div class="ki-toolbar-right">
            <div ref="pageHeaderTarget" class="ki-refresh-host" />

            <!-- 项目 + 环境双维度胶囊（对标 HCS tenant-switch） -->
            <el-dropdown trigger="click" @command="onProjectSelect">
              <div class="ki-scope-pill" :class="`ki-scope-pill--${ws.currentEnv}`">
                <i class="ki-scope-dot" />
                <FolderKanban :size="13" aria-hidden="true" />
                <span class="ki-scope-name">{{ currentProjectName }}</span>
                <ChevronDown :size="12" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-dd-header">切换交付项目</div>
                  <el-dropdown-item
                    v-for="p in ws.projects"
                    :key="p.id"
                    :command="p.id"
                    :class="{ 'is-selected': p.id === ws.currentProjectId }"
                  >
                    {{ p.name }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>

            <el-dropdown trigger="click" @command="onEnvSelect">
              <div class="ki-env-pill" :class="`ki-env-pill--${ws.currentEnv}`">
                <i class="ki-scope-dot" />
                <span>{{ envLabelMap[ws.currentEnv] }}</span>
                <ChevronDown :size="12" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-dd-header">目标环境上下文</div>
                  <el-dropdown-item command="prod">PROD 生产环境</el-dropdown-item>
                  <el-dropdown-item command="staging">STAGING 预发环境</el-dropdown-item>
                  <el-dropdown-item command="dev">DEV 开发环境</el-dropdown-item>
                  <el-dropdown-item command="sandbox">SANDBOX 沙箱</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>

            <el-tooltip content="物理离线严格模式：零外网 CDN，介质源锁定内网" placement="bottom">
              <span class="ki-live-pill">
                <i class="ki-live-dot ki-dot-pulse" />
                离线就绪
              </span>
            </el-tooltip>

            <el-tooltip :content="ws.theme === 'light' ? '切换到暗色主题' : '切换到浅色主题'" placement="bottom">
              <el-button text circle aria-label="切换明暗主题" @click="ws.toggleTheme">
                <Moon v-if="ws.theme === 'light'" :size="16" />
                <Sun v-else :size="16" />
              </el-button>
            </el-tooltip>

            <el-tooltip :content="ws.density === 'comfortable' ? '切换到紧凑行密度' : '切换到舒适行密度'" placement="bottom">
              <el-button text circle aria-label="切换界面密度" @click="ws.toggleDensity">
                <Maximize2 :size="15" />
              </el-button>
            </el-tooltip>

            <el-tooltip content="切换界面语言 (中文 / EN)" placement="bottom">
              <el-button text circle aria-label="切换语言" @click="ws.toggleLang">
                <Languages :size="15" />
              </el-button>
            </el-tooltip>

            <el-badge :value="ws.pendingApprovalCount" :hidden="ws.pendingApprovalCount === 0" :max="99" class="ki-bell">
              <el-button text circle aria-label="查看待审批工单" @click="router.push('/operations/approvals')">
                <Bell :size="16" />
              </el-button>
            </el-badge>

            <el-dropdown trigger="click" @command="onUserCommand">
              <div class="ki-user-trigger" :aria-label="`用户菜单：${auth.user?.username || 'ops'}`">
                <span class="ki-avatar">{{ userInitial }}</span>
                <div class="ki-user-who">
                  <b>{{ auth.user?.displayName || auth.user?.username || 'ops' }}</b>
                  <span>{{ auth.primaryRole }}</span>
                </div>
                <ChevronDown :size="13" class="ki-muted" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-user-card">
                    <b>{{ auth.user?.displayName || auth.user?.username || 'admin' }}</b>
                    <span>账号：{{ auth.user?.username || 'admin' }} · 角色：{{ auth.primaryRole }}</span>
                    <span>当前项目：{{ currentProjectName }} ({{ ws.currentEnv.toUpperCase() }})</span>
                  </div>
                  <el-dropdown-item command="shortcuts" divided>
                    <Keyboard :size="14" class="ki-dd-icon" /> 快捷键速查 (?)
                  </el-dropdown-item>
                  <el-dropdown-item command="settings">
                    <Settings :size="14" class="ki-dd-icon" /> 平台设置
                  </el-dropdown-item>
                  <el-dropdown-item command="logout" divided>
                    <LogOut :size="14" class="ki-dd-icon" /> 退出登录
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </el-header>

      <el-main class="ki-content">
        <div class="ki-content-inner">
          <WorkspaceNav />
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" :key="`${ws.currentProjectId}:${route.path}`" />
            </transition>
          </router-view>
        </div>
      </el-main>
    </el-container>

    <KiCommandPalette
      v-model="showCmdK"
      v-model:help-open="showHelp"
      :items="commandItems"
      @select="onCommandSelect"
    />
  </el-container>
</template>

<style scoped>
.ki-layout {
  height: 100vh;
}

.ki-sider {
  display: flex;
  flex-direction: column;
  background-color: var(--ki-bg-surface);
  border-right: 1px solid var(--ki-border-default);
  transition: width var(--ki-dur-base) var(--ki-ease);
  overflow: hidden;
}

.ki-logo {
  height: var(--ki-topbar-h);
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 16px;
  cursor: pointer;
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-logo-mark {
  display: inline-flex;
  color: var(--ki-brand);
  flex: none;
}

.ki-brand {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
  white-space: nowrap;
}

.ki-brand b {
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-bold);
}

.ki-brand span {
  color: var(--ki-text-muted);
  font-size: 11px;
}

.ki-menu-scroll {
  flex: 1;
}

.ki-menu {
  padding: 6px 10px 14px;
}

.ki-group-title {
  padding: 14px 10px 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-group-sep {
  height: 1px;
  margin: 10px 6px;
  background-color: var(--ki-border-subtle);
}

.ki-menu-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  height: var(--ki-menu-item-h);
  padding: 0 12px;
  margin: 2px 0;
  border-radius: var(--ki-radius-md);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-sub);
  white-space: nowrap;
  text-decoration: none;
  transition: background-color var(--ki-dur-fast) var(--ki-ease), color var(--ki-dur-fast) var(--ki-ease);
}

.ki-menu-item:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  text-decoration: none;
}

.ki-menu-item.is-active {
  background-color: var(--ki-sider-active-bg);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.ki-menu-item.is-active::before {
  content: "";
  position: absolute;
  left: -10px;
  top: 9px;
  bottom: 9px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background-color: var(--ki-brand);
}

.ki-menu-icon {
  flex: none;
}

.ki-menu-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ki-menu-badge {
  min-width: 20px;
  height: 18px;
  padding: 0 6px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-status-danger);
  color: var(--ki-text-on-solid);
  font-size: 11px;
  font-style: normal;
  display: grid;
  place-items: center;
  font-weight: var(--ki-fw-semibold);
}

.ki-sider-foot {
  height: 48px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 0 12px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ki-layout.is-collapsed .ki-menu-item {
  justify-content: center;
  padding: 0;
}

.ki-layout.is-collapsed .ki-sider-foot {
  justify-content: center;
}

.ki-main-wrap {
  min-width: 0;
  min-height: 0;
}

.ki-header {
  flex: none;
  background-color: var(--ki-bg-surface);
  border-bottom: 1px solid var(--ki-border-default);
  padding: 10px 20px;
}

.ki-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  min-height: 36px;
}

.ki-toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.ki-mobile-nav {
  display: none;
}

.ki-crumb-title {
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ki-search-trigger {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  width: 230px;
  padding: 0 10px;
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-md);
  color: var(--ki-text-muted);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.ki-search-trigger:hover {
  border-color: var(--ki-border-strong);
  background-color: var(--ki-bg-surface);
}

.ki-search-placeholder {
  flex: 1;
  text-align: left;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-kbd {
  font-size: 11px;
  color: var(--ki-text-muted);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-xs);
  padding: 0 5px;
  line-height: 16px;
}

.ki-toolbar-right {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ki-refresh-host {
  display: inline-flex;
  align-items: center;
}

.ki-scope-pill,
.ki-env-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 10px;
  border-radius: var(--ki-radius-pill);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
  border: 1px solid var(--ki-brand-border);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.ki-scope-name {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-scope-dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: currentcolor;
  flex: none;
}

.ki-env-pill--prod {
  color: var(--ki-env-prod-text);
  background-color: var(--ki-env-prod-bg);
  border-color: var(--ki-status-danger-border);
}

.ki-env-pill--prod .ki-scope-dot {
  box-shadow: var(--ki-dot-halo-prod);
}

.ki-env-pill--staging {
  color: var(--ki-env-staging-text);
  background-color: var(--ki-env-staging-bg);
  border-color: var(--ki-status-warn-border);
}

.ki-env-pill--staging .ki-scope-dot {
  box-shadow: var(--ki-dot-halo-staging);
}

.ki-env-pill--dev {
  color: var(--ki-env-dev-text);
  background-color: var(--ki-env-dev-bg);
  border-color: var(--ki-status-ok-border);
}

.ki-env-pill--dev .ki-scope-dot {
  box-shadow: var(--ki-dot-halo-dev);
}

.ki-env-pill--sandbox {
  color: var(--ki-env-sandbox-text);
  background-color: var(--ki-env-sandbox-bg);
  border-color: var(--ki-status-neutral-border);
}

.ki-live-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-status-ok-text);
  margin: 0 4px;
  white-space: nowrap;
}

.ki-live-dot {
  width: 8px;
  height: 8px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-status-ok);
  box-shadow: var(--ki-live-halo);
}

.ki-bell {
  margin-right: 2px;
}

.ki-user-trigger {
  display: flex;
  align-items: center;
  gap: 7px;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: var(--ki-radius-md);
}

.ki-user-trigger:hover {
  background-color: var(--ki-bg-subtle);
}

.ki-avatar {
  flex: none;
  width: 28px;
  height: 28px;
  border-radius: var(--ki-radius-pill);
  display: grid;
  place-items: center;
  color: var(--ki-text-on-solid);
  font-weight: var(--ki-fw-semibold);
  font-size: var(--ki-fs-sm);
  background: var(--ki-avatar-bg);
}

.ki-user-who {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}

.ki-user-who b {
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-primary);
}

.ki-user-who span {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.ki-dd-header {
  padding: 6px 14px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  border-bottom: 1px solid var(--ki-border-subtle);
}

.ki-user-card {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 8px 14px 10px;
  min-width: 230px;
}

.ki-user-card b {
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.ki-user-card span {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-dd-icon {
  margin-right: 6px;
}

.ki-content {
  padding: 18px 22px 28px;
  background-color: var(--ki-bg-canvas);
}

.ki-content-inner {
  max-width: var(--ki-content-max);
  margin: 0 auto;
}

@media (max-width: 767px) {
  .ki-sider {
    display: none;
  }

  .ki-mobile-nav {
    display: inline-flex;
  }

  .ki-search-trigger,
  .ki-live-pill,
  .ki-user-who {
    display: none;
  }

  .ki-content {
    padding: 14px 12px 24px;
  }
}
</style>
