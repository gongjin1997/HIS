<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  KeyRound,
  Layers,
  Lock,
  Moon,
  Server,
  ShieldCheck,
  Sun,
  User,
  Workflow,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { apiRequest, APIError, type ProblemDetail } from '@/api/client'
import KiBrandLogo from '@/components/ki/KiBrandLogo.vue'
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

const highlights = [
  {
    icon: Server,
    title: '主机资产与基线初始化',
    desc: '多架构自动采集、7 套内核 Profile 与 22 模块幂等加固',
  },
  {
    icon: Layers,
    title: '三态中间件与组件编排',
    desc: '统一覆盖主机二进制、容器与 Kubernetes，内置 12 类核心插件',
  },
  {
    icon: Workflow,
    title: '应用栈拓扑与安全变更',
    desc: 'DAG 依赖编排、三向配置漂移预检与高危操作双人复核',
  },
  {
    icon: ShieldCheck,
    title: '离线闭环与防篡改审计',
    desc: '零外网依赖、内网三级介质回退与 SHA-256 哈希链存证',
  },
]

function startOidcLogin() {
  if (!oidcEnabled.value) return
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
  problem.value = null
}

async function onSubmit() {
  if (loading.value) return
  if (!form.username.trim() || !form.password) {
    problem.value = { code: 'KI-AUTH-0001', title: '请输入运维账号和登录密码' }
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
  <div class="ki-login-shell">
    <!-- Left Architectural Brand Showcase -->
    <section class="ki-login-hero" aria-label="平台概览">
      <div class="ki-login-hero__glow" aria-hidden="true" />
      <div class="ki-login-hero__grid" aria-hidden="true" />

      <div class="ki-login-hero__inner">
        <header class="ki-hero-head">
          <div class="ki-hero-brand">
            <KiBrandLogo :size="38" variant="hero" />
            <div class="ki-hero-brand-text">
              <strong>kinstall</strong>
              <span>Enterprise Infrastructure Control Plane</span>
            </div>
          </div>
          <span class="ki-hero-status">
            <i class="ki-hero-status__dot" />
            离线安全模式就绪
          </span>
        </header>

        <div class="ki-hero-copy">
          <h1>企业基础设施与全栈中间件<br />统一交付控制台</h1>
          <p>
            面向物理离线内网与多架构信创环境的声明式运维底座，将主机基线、核心组件部署、应用栈编排与合规审计收敛至同一控制平面。
          </p>

          <div class="ki-hero-features">
            <article v-for="item in highlights" :key="item.title" class="ki-hero-card">
              <div class="ki-hero-card__icon" aria-hidden="true">
                <component :is="item.icon" :size="18" />
              </div>
              <div class="ki-hero-card__body">
                <h3>{{ item.title }}</h3>
                <p>{{ item.desc }}</p>
              </div>
            </article>
          </div>
        </div>

        <footer class="ki-hero-foot">
          <span class="ki-hero-arch ki-mono">x86_64 · ARM64 (4K / 64K) · Kylin / openEuler / Debian / RHEL</span>
          <span class="ki-hero-ver ki-mono">Air-Gapped · Zero CDN</span>
        </footer>
      </div>
    </section>

    <!-- Right Clean Authentication Panel -->
    <main class="ki-login-panel">
      <div class="ki-panel-topbar">
        <div class="ki-mobile-brand">
          <KiBrandLogo :size="30" variant="badge" />
          <strong>kinstall 交付平台</strong>
        </div>
        <el-tooltip :content="ws.theme === 'light' ? '切换暗色外观' : '切换浅色外观'" placement="left">
          <el-button text circle aria-label="切换明暗主题" @click="ws.toggleTheme">
            <Moon v-if="ws.theme === 'light'" :size="16" />
            <Sun v-else :size="16" />
          </el-button>
        </el-tooltip>
      </div>

      <div class="ki-login-box">
        <div class="ki-login-heading">
          <h2>登录控制台</h2>
          <p>输入运维账号与密码接入当前交付项目域</p>
        </div>

        <KiProblemBanner v-if="problem" :problem="problem" compact class="ki-mb-4" />

        <el-form label-position="top" size="large" class="ki-login-form" @submit.prevent="onSubmit">
          <el-form-item label="运维账号">
            <el-input
              v-model="form.username"
              placeholder="请输入账号，如 admin"
              autocomplete="username"
            >
              <template #prefix>
                <User :size="15" class="ki-muted" />
              </template>
            </el-input>
          </el-form-item>

          <el-form-item label="登录密码">
            <el-input
              v-model="form.password"
              type="password"
              placeholder="请输入密码"
              show-password
              autocomplete="current-password"
            >
              <template #prefix>
                <Lock :size="15" class="ki-muted" />
              </template>
            </el-input>
          </el-form-item>

          <div class="ki-login-options">
            <el-checkbox v-model="form.remember">记住登录账号</el-checkbox>
            <el-tooltip content="Argon2id 密码哈希校验，连续 5 次认证失败自动锁定 15 分钟" placement="top">
              <span class="ki-login-policy">安全锁定策略</span>
            </el-tooltip>
          </div>

          <el-button type="primary" native-type="submit" class="ki-login-submit" :loading="loading">
            登录控制台
          </el-button>
        </el-form>

        <div class="ki-login-divider">
          <span>企业身份联邦</span>
        </div>

        <el-tooltip
          :disabled="oidcEnabled"
          content="在服务端配置 KINSTALL_OIDC_ISSUER 与客户端参数后自动启用"
          placement="top"
        >
          <div class="ki-oidc-wrap">
            <el-button plain :disabled="!oidcEnabled" class="ki-oidc-btn" @click="startOidcLogin">
              <KeyRound :size="15" class="ki-oidc-icon" />
              <span class="ki-oidc-label">
                {{ oidcEnabled ? '使用企业 OIDC / PKCE 单点登录' : '企业 OIDC 单点登录（未配置）' }}
              </span>
            </el-button>
            <p v-if="oidcEnabled && oidcIssuer" class="ki-oidc-issuer ki-mono" :title="oidcIssuer">
              Issuer: {{ oidcIssuer }}
            </p>
          </div>
        </el-tooltip>

        <button type="button" class="ki-login-hint" @click="fillDefault">
          <span class="ki-hint-main">
            <span class="ki-muted">默认管理员</span>
            <code class="ki-mono">admin / Kinstall#2026!</code>
          </span>
          <span class="ki-hint-action">一键填入</span>
        </button>
      </div>

      <footer class="ki-panel-foot">
        <span>本地离线会话 · 全链路防篡改审计</span>
      </footer>
    </main>
  </div>
</template>

<style scoped>
.ki-login-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: minmax(0, 1.18fr) minmax(440px, 0.82fr);
  background-color: var(--ki-bg-surface);
  overflow-x: hidden;
}

/* Left Architectural Showcase */
.ki-login-hero {
  position: relative;
  display: flex;
  flex-direction: column;
  background: var(--ki-login-hero-bg);
  color: var(--ki-login-hero-text);
  overflow: hidden;
}

.ki-login-hero__glow {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: var(--ki-login-hero-glow);
}

.ki-login-hero__grid {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: var(--ki-login-hero-grid);
  background-size: 40px 40px;
}

.ki-login-hero__inner {
  position: relative;
  z-index: var(--ki-z-sticky);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-height: 100%;
  padding: 40px 56px 36px;
  gap: 40px;
}

.ki-hero-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.ki-hero-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ki-hero-brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}

.ki-hero-brand-text strong {
  font-size: 20px;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
  color: var(--ki-login-hero-text);
}

.ki-hero-brand-text span {
  font-size: 11px;
  color: var(--ki-login-hero-muted);
  letter-spacing: 0.02em;
}

.ki-hero-status {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 28px;
  padding: 0 12px;
  border-radius: var(--ki-radius-pill);
  border: 1px solid var(--ki-login-hero-badge-border);
  background-color: var(--ki-login-hero-badge-bg);
  color: var(--ki-login-hero-badge-text);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  white-space: nowrap;
}

.ki-hero-status__dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: currentcolor;
}

.ki-hero-copy {
  max-width: 620px;
}

.ki-hero-copy h1 {
  font-size: 34px;
  line-height: 1.28;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
  color: var(--ki-login-hero-text);
}

.ki-hero-copy > p {
  margin: 16px 0 32px;
  font-size: var(--ki-fs-section);
  line-height: 1.65;
  color: var(--ki-login-hero-secondary);
}

.ki-hero-features {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.ki-hero-card {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  border-radius: var(--ki-radius-card);
  border: 1px solid var(--ki-login-hero-border);
  background-color: var(--ki-login-hero-surface);
  transition: background-color var(--ki-dur-base) var(--ki-ease);
}

.ki-hero-card:hover {
  background-color: var(--ki-login-hero-surface-hover);
}

.ki-hero-card__icon {
  flex: none;
  width: 34px;
  height: 34px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-login-hero-border);
  background-color: var(--ki-login-hero-surface-hover);
  color: var(--ki-login-hero-accent);
  display: grid;
  place-items: center;
}

.ki-hero-card__body {
  min-width: 0;
}

.ki-hero-card__body h3 {
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-login-hero-text);
}

.ki-hero-card__body p {
  margin: 4px 0 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.5;
  color: var(--ki-login-hero-secondary);
}

.ki-hero-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 20px;
  border-top: 1px solid var(--ki-login-hero-border);
  font-size: var(--ki-fs-xs);
  color: var(--ki-login-hero-muted);
}

/* Right Authentication Surface */
.ki-login-panel {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 24px 40px 28px;
  background-color: var(--ki-bg-surface);
}

.ki-panel-topbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  min-height: 36px;
}

.ki-mobile-brand {
  display: none;
  align-items: center;
  gap: 10px;
  margin-right: auto;
  color: var(--ki-text-primary);
}

.ki-login-box {
  width: 100%;
  max-width: 392px;
  margin: auto;
}

.ki-login-heading {
  margin-bottom: 28px;
}

.ki-login-heading h2 {
  font-size: 24px;
  line-height: 1.3;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
  letter-spacing: var(--ki-tracking-tight);
}

.ki-login-heading p {
  margin: 8px 0 0;
  font-size: var(--ki-fs-body);
  color: var(--ki-text-muted);
}

.ki-login-options {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: -2px 0 20px;
}

.ki-login-policy {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  cursor: help;
}

.ki-login-policy:hover {
  color: var(--ki-brand);
}

.ki-login-submit {
  width: 100%;
  height: 44px;
  font-size: var(--ki-fs-section);
  font-weight: var(--ki-fw-semibold);
}

.ki-login-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 22px 0 14px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ki-login-divider::before,
.ki-login-divider::after {
  content: "";
  flex: 1;
  height: 1px;
  background-color: var(--ki-border-subtle);
}

.ki-oidc-wrap {
  width: 100%;
}

.ki-oidc-btn {
  width: 100%;
  height: 40px;
  max-width: 100%;
  overflow: hidden;
}

.ki-oidc-icon {
  margin-right: 6px;
  flex: none;
}

.ki-oidc-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-oidc-issuer {
  margin: 6px 0 0;
  font-size: 11px;
  color: var(--ki-text-muted);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ki-login-hint {
  margin-top: 20px;
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-family: var(--ki-font-sans);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.ki-login-hint:hover {
  border-color: var(--ki-brand-border);
  background-color: var(--ki-bg-hover);
}

.ki-hint-main {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.ki-hint-main code {
  color: var(--ki-text-primary);
  font-weight: var(--ki-fw-semibold);
}

.ki-hint-action {
  flex: none;
  padding: 2px 8px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-medium);
}

.ki-panel-foot {
  text-align: center;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

@media (max-width: 1024px) {
  .ki-login-shell {
    grid-template-columns: 1fr;
  }

  .ki-login-hero {
    display: none;
  }

  .ki-mobile-brand {
    display: inline-flex;
  }

  .ki-login-panel {
    min-height: 100vh;
    padding: 20px 24px 28px;
  }
}
</style>
