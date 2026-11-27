<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Lock, User, KeyRound } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { apiRequest, APIError, type ProblemDetail } from '@/api/client'
import KiProblemBanner from '@/components/domain/KiProblemBanner.vue'

const LAST_USER_KEY = 'ki.last_user'
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const form = reactive({
  username: localStorage.getItem(LAST_USER_KEY) || 'admin',
  password: '',
  remember: true,
})
const loading = ref(false)
const problem = ref<ProblemDetail | null>(null)
const oidcEnabled = ref(false)
const oidcIssuer = ref('')

function startOidcLogin() {
  window.location.href = '/api/v1/auth/oidc/login'
}

onMounted(async () => {
  const cbTok = typeof route.query.oidc_token === 'string' ? route.query.oidc_token : ''
  if (cbTok) {
    auth.setToken(cbTok)
    const ok = await auth.fetchMe()
    if (ok) {
      await ws.loadProjects()
      await ws.refreshApprovalBadge()
      await router.replace('/dashboard')
      return
    }
  }
  try {
    const { data } = await apiRequest<{ enabled: boolean; issuer?: string }>('GET', '/api/v1/auth/oidc/config')
    oidcEnabled.value = Boolean(data?.enabled)
    oidcIssuer.value = data?.issuer || ''
  } catch {
    oidcEnabled.value = false
  }
})

function fillDefault() {
  form.username = 'admin'
  form.password = 'Kinstall#2026!'
}

async function onSubmit() {
  if (loading.value) return
  if (!form.username.trim() || !form.password) {
    problem.value = { code: 'KI-AUTH-0001', title: '请输入账号和密码' }
    return
  }
  loading.value = true
  problem.value = null
  try {
    await auth.loginLocal(form.username.trim(), form.password)
    if (form.remember) localStorage.setItem(LAST_USER_KEY, form.username.trim())
    await ws.loadProjects()
    await ws.refreshApprovalBadge()
    await router.replace('/dashboard')
  } catch (e) {
    if (e instanceof APIError) {
      problem.value = e.problem
    } else {
      problem.value = { code: 'KI-AUTH-0001', title: '登录失败', detail: String(e) }
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="ki-login">
    <div class="ki-login-bg" aria-hidden="true" />
    <div class="ki-login-card">
      <div class="ki-login-brand">
        <span class="ki-login-emblem" aria-hidden="true">
          <svg viewBox="0 0 32 32" width="40" height="40">
            <rect width="32" height="32" rx="9" fill="currentColor" />
            <path d="M16 6l8 3v6c0 5-3.4 9.3-8 11-4.6-1.7-8-6-8-11V9z" fill="none" stroke="var(--ki-text-on-solid)" stroke-width="2" />
            <path d="M12 16l3 3 5-6" fill="none" stroke="var(--ki-text-on-solid)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </span>
        <div>
          <h1>kinstall 交付平台</h1>
          <p class="ki-login-sub">企业离线基础设施与全栈中间件控制台</p>
        </div>
      </div>

      <KiProblemBanner v-if="problem" :problem="problem" compact class="ki-mb-3" />

      <el-form size="large" class="ki-login-form" @submit.prevent="onSubmit">
        <el-form-item>
          <el-input
            v-model="form.username"
            placeholder="运维账号"
            autocomplete="username"
          >
            <template #prefix><User :size="15" class="ki-muted" /></template>
          </el-input>
        </el-form-item>
        <el-form-item>
          <el-input
            v-model="form.password"
            type="password"
            placeholder="登录密码"
            show-password
            autocomplete="current-password"
          >
            <template #prefix><Lock :size="15" class="ki-muted" /></template>
          </el-input>
        </el-form-item>

        <div class="ki-login-row">
          <el-checkbox v-model="form.remember">记住登录账号</el-checkbox>
          <el-tooltip content="Argon2id 加密校验，连续 5 次失败自动锁定 15 分钟" placement="top">
            <span class="ki-login-help">安全策略说明</span>
          </el-tooltip>
        </div>

        <el-button type="primary" native-type="submit" class="ki-login-submit" :loading="loading">
          登 录
        </el-button>
      </el-form>

      <div class="ki-login-oidc">
        <el-button plain :disabled="!oidcEnabled" class="ki-w-full" @click="startOidcLogin">
          <KeyRound :size="14" class="ki-oidc-icon" />
          {{ oidcEnabled ? `企业 OIDC / PKCE 单点登录 (${oidcIssuer})` : '企业 OIDC / PKCE 单点登录（配置 KINSTALL_OIDC_ISSUER 后启用）' }}
        </el-button>
      </div>

      <button type="button" class="ki-login-hint" @click="fillDefault">
        <span class="ki-muted">初始管理员：</span>
        <b class="ki-mono">admin / Kinstall#2026!</b>
        <em class="ki-muted">点击填充</em>
      </button>
    </div>
  </div>
</template>

<style scoped>
.ki-login {
  position: relative;
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background-color: var(--ki-bg-canvas);
  overflow: hidden;
  padding: var(--ki-space-4);
}

.ki-login-bg {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: var(--ki-login-radials);
}

.ki-login-bg::after {
  content: "";
  position: absolute;
  inset: 0;
  opacity: 0.55;
  background: var(--ki-login-grid-bg);
  background-size: 36px 36px;
}

.ki-login-card {
  position: relative;
  width: min(408px, calc(100vw - 32px));
  padding: 36px 36px 24px;
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-xl);
  box-shadow: var(--ki-shadow-modal);
}

.ki-login-brand {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 26px;
}

.ki-login-emblem {
  display: inline-flex;
  color: var(--ki-brand);
  flex: none;
}

.ki-login-brand h1 {
  font-size: 20px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ki-login-sub {
  margin: 3px 0 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-login-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: -4px 0 18px;
}

.ki-login-help {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  cursor: help;
}

.ki-login-help:hover {
  color: var(--ki-brand);
}

.ki-login-submit {
  width: 100%;
  height: 42px;
  font-size: var(--ki-fs-section);
}

.ki-login-oidc {
  margin-top: 12px;
}

.ki-oidc-icon {
  margin-right: 6px;
}

.ki-login-hint {
  margin-top: 18px;
  width: 100%;
  padding: 8px 12px;
  border: 1px dashed var(--ki-border-default);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
}

.ki-login-hint:hover {
  border-color: var(--ki-brand-border);
  color: var(--ki-brand);
}

.ki-login-hint em {
  font-style: normal;
}
</style>
