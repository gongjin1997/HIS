<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  LayoutDashboard, Server, Box, GitBranch, Network, Terminal,
  ClipboardCheck, HardDrive, ShieldCheck, Settings, Sun, Moon,
  Languages, LogOut, PanelLeftClose, PanelLeftOpen, Search, Maximize2,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import KiEnvBadge from '@/components/KiEnvBadge.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const showCmdK = ref(false)
const cmdQuery = ref('')

const navItems = [
  { path: '/dashboard', key: 'nav.dashboard', icon: LayoutDashboard },
  { path: '/hosts', key: 'nav.hosts', icon: Server },
  { path: '/instances', key: 'nav.instances', icon: Box },
  { path: '/stacks', key: 'nav.stacks', icon: GitBranch },
  { path: '/clusters', key: 'nav.clusters', icon: Network },
  { path: '/operations', key: 'nav.operations', icon: Terminal },
  { path: '/operations/approvals', key: 'nav.approvals', icon: ClipboardCheck },
  { path: '/artifacts/media-policies', key: 'nav.mediaPolicies', icon: HardDrive },
  { path: '/security/audit', key: 'nav.audit', icon: ShieldCheck },
  { path: '/settings', key: 'nav.settings', icon: Settings },
]

function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    showCmdK.value = !showCmdK.value
  }
}

function logout() {
  auth.logout()
  void router.push('/login')
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown)
  await ws.loadProjects()
  await ws.refreshApprovalBadge()
})
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <div style="display: flex; flex-direction: column; height: 100vh">
    <!-- Topbar 48px -->
    <header
      style="height: var(--ki-topbar-h); background: var(--ki-bg-surface); border-bottom: 1px solid var(--ki-border-subtle); display: flex; align-items: center; justify-content: space-between; padding: 0 14px; flex-shrink: 0"
    >
      <div class="ki-flex-row">
        <el-button text size="small" aria-label="Toggle Sidebar" @click="ws.sidebarCollapsed = !ws.sidebarCollapsed">
          <PanelLeftOpen v-if="ws.sidebarCollapsed" :size="16" />
          <PanelLeftClose v-else :size="16" />
        </el-button>
        <span class="ki-mono" style="font-weight: 600; font-size: 15px; letter-spacing: -0.02em">kinstall</span>

        <el-select v-model="ws.currentProjectId" size="small" style="width: 160px; margin-left: 8px">
          <el-option v-for="p in ws.projects" :key="p.id" :label="p.name" :value="p.id" />
        </el-select>

        <el-select v-model="ws.currentEnv" size="small" style="width: 110px">
          <el-option label="prod" value="prod" />
          <el-option label="staging" value="staging" />
          <el-option label="dev" value="dev" />
          <el-option label="sandbox" value="sandbox" />
        </el-select>
        <KiEnvBadge :env="ws.currentEnv" />
      </div>

      <el-button size="small" plain style="width: 280px; justify-content: space-between" @click="showCmdK = true">
        <span class="ki-flex-row ki-muted"><Search :size="13" /> {{ $t('common.searchCmd') }}</span>
      </el-button>

      <div class="ki-flex-row">
        <el-badge :value="ws.pendingApprovalCount" :hidden="ws.pendingApprovalCount === 0">
          <el-button size="small" text @click="router.push('/operations/approvals')">
            <ClipboardCheck :size="16" />
          </el-button>
        </el-badge>
        <el-button size="small" text title="切换行密度 (40px / 32px)" @click="ws.toggleDensity"><Maximize2 :size="15" /></el-button>
        <el-button size="small" text title="切换暗/亮主题" @click="ws.toggleTheme">
          <Sun v-if="ws.theme === 'dark'" :size="15" />
          <Moon v-else :size="15" />
        </el-button>
        <el-button size="small" text title="切换中/英语言" @click="ws.toggleLang"><Languages :size="15" /></el-button>
        <span class="ki-mono ki-secondary" style="font-size: 12px">{{ auth.user?.username || 'ops' }}</span>
        <el-button size="small" text title="退出登录" @click="logout"><LogOut :size="15" /></el-button>
      </div>
    </header>

    <div style="display: flex; flex: 1; min-height: 0">
      <!-- Sidebar 224px / 56px -->
      <aside
        :style="{
          width: ws.sidebarCollapsed ? 'var(--ki-sidebar-collapsed-w)' : 'var(--ki-sidebar-w)',
          background: 'var(--ki-bg-surface)',
          borderRight: '1px solid var(--ki-border-subtle)',
          padding: '10px 8px',
          transition: 'width 180ms ease',
          flexShrink: 0,
        }"
      >
        <nav style="display: flex; flex-direction: column; gap: 2px">
          <router-link
            v-for="item in navItems"
            :key="item.path"
            :to="item.path"
            :style="{
              display: 'flex',
              alignItems: 'center',
              gap: '10px',
              height: 'var(--ki-menu-item-h)',
              padding: '0 10px',
              borderRadius: '4px',
              color: route.path === item.path ? 'var(--ki-text-primary)' : 'var(--ki-text-secondary)',
              background: route.path === item.path ? 'var(--ki-brand-subtle)' : 'transparent',
              fontWeight: route.path === item.path ? 600 : 400,
              textDecoration: 'none',
            }"
          >
            <component :is="item.icon" :size="16" style="flex-shrink: 0" />
            <span v-if="!ws.sidebarCollapsed" style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
              {{ $t(item.key) }}
            </span>
          </router-link>
        </nav>
      </aside>

      <!-- Main content canvas -->
      <main style="flex: 1; overflow: auto; padding: 20px 24px">
        <div style="max-width: var(--ki-content-max); margin: 0 auto">
          <router-view />
        </div>
      </main>
    </div>

    <el-dialog v-model="showCmdK" title="快速跳转 (⌘K)" width="520px">
      <el-input v-model="cmdQuery" placeholder="输入模块名称..." style="margin-bottom: 10px" />
      <div style="display: flex; flex-direction: column; gap: 4px">
        <el-button
          v-for="item in navItems.filter((n) => $t(n.key).toLowerCase().includes(cmdQuery.toLowerCase()) || n.path.includes(cmdQuery.toLowerCase()))"
          :key="item.path"
          text
          style="justify-content: flex-start"
          @click="showCmdK = false; router.push(item.path)"
        >
          <component :is="item.icon" :size="15" style="margin-right: 8px" />
          {{ $t(item.key) }} <span class="ki-mono ki-muted" style="margin-left: auto">{{ item.path }}</span>
        </el-button>
      </div>
    </el-dialog>
  </div>
</template>
