<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ChevronDown,
  FolderKanban,
  Keyboard,
  LogOut,
  Menu,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  SlidersHorizontal,
  Sun,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore, type EnvCode } from '@/stores/workspace'
import { useShortcuts } from '@/composables/useShortcuts'
import { ALL_MENU_ITEMS, GROUP_OF_PATH, MENU_GROUPS, TITLE_OF_PATH } from '@/config/menu'
import KiBrandLogo from '@/components/ki/KiBrandLogo.vue'
import KiCommandPalette, { type CommandItem } from '@/components/ki/KiCommandPalette.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const showCmdK = ref(false)
const showHelp = ref(false)

const currentGroup = computed(() => GROUP_OF_PATH[route.path] || '全景总览')
const currentTitle = computed(() => TITLE_OF_PATH[route.path] || (route.meta.title as string) || '资产与运维全景')
const currentWorkspaceName = computed(
  () => ws.workspaces.find((w) => w.id === ws.currentWorkspaceId)?.name || '核心运维空间'
)
const userInitial = computed(() => (auth.user?.displayName || auth.user?.username || 'A').slice(0, 1).toUpperCase())

const envLabelMap: Record<EnvCode, string> = {
  prod: 'PROD 生产',
  beta: 'BETA 灰度',
  uat: 'UAT 验收',
  sit: 'SIT 集成',
  dev: 'DEV 研发',
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

function onWorkspaceSelect(id: string) {
  ws.currentWorkspaceId = id
}

function onEnvSelect(env: EnvCode) {
  ws.currentEnv = env
}

function onUserCommand(cmd: string) {
  if (cmd === 'governance') void router.push('/governance')
  else if (cmd === 'density') ws.toggleDensity()
  else if (cmd === 'shortcuts') showHelp.value = true
  else if (cmd === 'logout') {
    auth.logout()
    void router.push('/login')
  }
}

onMounted(() => {
  void ws.loadWorkspaces()
})
</script>

<template>
  <el-container class="ki-layout" :class="{ 'is-collapsed': ws.sidebarCollapsed }">
    <el-aside
      class="ki-sider ki-shell-sidebar"
      :width="ws.sidebarCollapsed ? 'var(--ki-sidebar-collapsed-w)' : 'var(--ki-sidebar-w)'"
    >
      <div class="ki-logo" @click="router.push('/dashboard')">
        <KiBrandLogo :size="30" variant="badge" />
        <div v-show="!ws.sidebarCollapsed" class="ki-brand">
          <b>kinstall</b>
          <span>CMDB 与声明式交付中枢</span>
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
              </router-link>
            </el-tooltip>
          </template>
        </nav>
      </el-scrollbar>

      <div class="ki-sider-foot">
        <button
          type="button"
          class="ki-collapse-btn"
          :aria-label="ws.sidebarCollapsed ? '展开导航栏' : '折叠导航栏'"
          @click="ws.sidebarCollapsed = !ws.sidebarCollapsed"
        >
          <PanelLeftOpen v-if="ws.sidebarCollapsed" :size="16" />
          <PanelLeftClose v-else :size="16" />
          <span v-show="!ws.sidebarCollapsed">收起导航</span>
        </button>
      </div>
    </el-aside>

    <el-container direction="vertical" class="ki-main-wrap">
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
                <span class="ki-crumb-title">{{ currentTitle }}</span>
              </el-breadcrumb-item>
            </el-breadcrumb>
          </div>

          <button type="button" class="ki-search-trigger" @click="showCmdK = true">
            <Search :size="14" class="ki-muted" aria-hidden="true" />
            <span class="ki-search-placeholder">快速跳转资产、插件或发布…</span>
            <kbd class="ki-kbd ki-mono">/</kbd>
          </button>

          <div class="ki-toolbar-right">
            <el-dropdown trigger="click" @command="onWorkspaceSelect">
              <div class="ki-scope-pill">
                <FolderKanban :size="13" aria-hidden="true" />
                <span class="ki-scope-name">{{ currentWorkspaceName }}</span>
                <ChevronDown :size="12" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-dd-header">切换运维空间</div>
                  <el-dropdown-item
                    v-for="w in ws.workspaces"
                    :key="w.id"
                    :command="w.id"
                    :class="{ 'is-selected': w.id === ws.currentWorkspaceId }"
                  >
                    {{ w.name }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>

            <el-dropdown trigger="click" @command="onEnvSelect">
              <div class="ki-env-pill" :class="ws.currentEnv === 'prod' ? 'ki-env-pill--prod' : ws.currentEnv === 'dev' ? 'ki-env-pill--dev' : 'ki-env-pill--staging'">
                <i class="ki-scope-dot" />
                <span>{{ envLabelMap[ws.currentEnv] }}</span>
                <ChevronDown :size="12" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-dd-header">五环境隔离上下文</div>
                  <el-dropdown-item command="prod">PROD 生产环境</el-dropdown-item>
                  <el-dropdown-item command="beta">BETA 灰度环境</el-dropdown-item>
                  <el-dropdown-item command="uat">UAT 验收环境</el-dropdown-item>
                  <el-dropdown-item command="sit">SIT 集成环境</el-dropdown-item>
                  <el-dropdown-item command="dev">DEV 研发环境</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>

            <span class="ki-toolbar-sep" aria-hidden="true" />

            <el-tooltip :content="ws.theme === 'light' ? '切换暗色主题' : '切换浅色主题'" placement="bottom">
              <el-button text circle aria-label="切换明暗主题" @click="ws.toggleTheme">
                <Moon v-if="ws.theme === 'light'" :size="16" />
                <Sun v-else :size="16" />
              </el-button>
            </el-tooltip>

            <el-dropdown trigger="click" @command="onUserCommand">
              <div class="ki-user-trigger" :aria-label="`用户菜单：${auth.user?.username || 'admin'}`">
                <span class="ki-avatar">{{ userInitial }}</span>
                <div class="ki-user-who">
                  <b>{{ auth.user?.displayName || auth.user?.username || 'admin' }}</b>
                  <span>{{ auth.primaryRole }}</span>
                </div>
                <ChevronDown :size="13" class="ki-muted" aria-hidden="true" />
              </div>
              <template #dropdown>
                <el-dropdown-menu>
                  <div class="ki-user-card">
                    <b>{{ auth.user?.displayName || auth.user?.username || 'admin' }}</b>
                    <span>账号：{{ auth.user?.username }} · 角色：{{ auth.primaryRole }}</span>
                    <span>版本：Kinstall v{{ auth.version }}</span>
                  </div>
                  <el-dropdown-item command="density" divided>
                    <SlidersHorizontal :size="14" class="ki-dd-icon" />
                    {{ ws.density === 'comfortable' ? '切换到紧凑行密度' : '切换到舒适行密度' }}
                  </el-dropdown-item>
                  <el-dropdown-item command="shortcuts">
                    <Keyboard :size="14" class="ki-dd-icon" /> 快捷键速查 (?)
                  </el-dropdown-item>
                  <el-dropdown-item command="governance">
                    <Settings :size="14" class="ki-dd-icon" /> 权限审计与设置
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
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" :key="`${ws.currentWorkspaceId}:${route.path}`" />
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

.ki-brand {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
  white-space: nowrap;
}

.ki-brand b {
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-page);
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-snug);
}

.ki-brand span {
  color: var(--ki-text-muted);
  font-size: 11px;
}

.ki-menu-scroll {
  flex: 1;
}

.ki-menu {
  padding: 8px 10px 14px;
}

.ki-group-title {
  padding: 14px 10px 5px;
  font-size: 11px;
  font-weight: var(--ki-fw-semibold);
  letter-spacing: 0.03em;
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

.ki-sider-foot {
  flex: none;
  padding: 8px 10px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ki-collapse-btn {
  width: 100%;
  height: 34px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid transparent;
  border-radius: var(--ki-radius-md);
  background-color: transparent;
  color: var(--ki-text-muted);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
}

.ki-collapse-btn:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.ki-layout.is-collapsed .ki-logo,
.ki-layout.is-collapsed .ki-menu-item,
.ki-layout.is-collapsed .ki-collapse-btn {
  justify-content: center;
  padding: 0;
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
  width: 232px;
  padding: 0 10px;
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-md);
  color: var(--ki-text-muted);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
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
  gap: 8px;
}

.ki-toolbar-sep {
  width: 1px;
  height: 18px;
  background-color: var(--ki-border-default);
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
  white-space: nowrap;
}

.ki-scope-pill {
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
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
  border: 1px solid var(--ki-status-danger-border);
}

.ki-env-pill--staging {
  color: var(--ki-env-staging-text);
  background-color: var(--ki-env-staging-bg);
  border: 1px solid var(--ki-status-warn-border);
}

.ki-env-pill--dev {
  color: var(--ki-env-dev-text);
  background-color: var(--ki-env-dev-bg);
  border: 1px solid var(--ki-status-ok-border);
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
  min-width: 220px;
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
  padding: 20px 24px 32px;
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
  .ki-toolbar-sep,
  .ki-user-who {
    display: none;
  }

  .ki-content {
    padding: 14px 12px 24px;
  }
}
</style>
