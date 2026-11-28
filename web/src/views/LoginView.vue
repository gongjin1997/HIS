<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { GitFork, KeyRound, Lock, ShieldCheck, User } from 'lucide-vue-next'
import { useScopeStore } from '@/stores/scope'
import { ApiError } from '@/api/client'

const router = useRouter()
const route = useRoute()
const scope = useScopeStore()

const username = ref('admin')
const password = ref('LiveOps#2026!')
const loading = ref(false)
const errMsg = ref('')

async function onSubmit() {
  errMsg.value = ''
  loading.value = true
  try {
    await scope.login(username.value.trim(), password.value)
    const target = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
      ? route.query.redirect
      : '/home'
    await router.push(target)
  } catch (e) {
    errMsg.value = e instanceof ApiError ? e.message : String(e)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="ks-login">
    <aside class="ks-login__hero">
      <div class="ks-login__hero-inner">
        <div class="ks-login__badge">
          <span class="ks-login__badge-dot" />
          <span>LiveOps Enterprise Platform</span>
        </div>

        <h1 class="ks-login__headline">企业级多租户<br />运维与交付控制平台</h1>
        <p class="ks-login__lead">
          面向核心业务集群打造的一体化运维底座，统一纳管企业空间、资产拓扑、加密配置、零信任节点与高可靠发布流程。
        </p>

        <div class="ks-login__pillars">
          <div class="ks-pillar">
            <ShieldCheck :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">多租户隔离与精细权限治理</div>
              <div class="ks-pillar__desc">平台、企业空间、项目三级隔离，关键操作全程留痕防篡改</div>
            </div>
          </div>
          <div class="ks-pillar">
            <GitFork :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">业务与资源双视角 CMDB</div>
              <div class="ks-pillar__desc">树形资产目录、拓扑可视化追踪及 Excel 批量维护</div>
            </div>
          </div>
          <div class="ks-pillar">
            <KeyRound :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">高可靠配置中心与发布编排</div>
              <div class="ks-pillar__desc">国密与信封加密保护、版本差异回滚与实时流水线观测</div>
            </div>
          </div>
        </div>
      </div>
    </aside>

    <main class="ks-login__main">
      <div class="ks-login__card">
        <div class="ks-login__brand">
          <span class="ks-login__mark" />
          <div>
            <div class="ks-login__brand-name">登录 LiveOps 控制台</div>
            <div class="ks-login__brand-sub">请输入您的企业平台账号密码</div>
          </div>
        </div>

        <form class="ks-login__form" @submit.prevent="onSubmit">
          <div class="ks-field">
            <label for="ks-username">用户名</label>
            <div class="ks-input-wrap">
              <User :size="15" class="ks-input-icon" />
              <input
                id="ks-username"
                v-model="username"
                type="text"
                autocomplete="username"
                placeholder="请输入用户名"
                required
              />
            </div>
          </div>

          <div class="ks-field">
            <label for="ks-password">密码</label>
            <div class="ks-input-wrap">
              <Lock :size="15" class="ks-input-icon" />
              <input
                id="ks-password"
                v-model="password"
                type="password"
                autocomplete="current-password"
                placeholder="请输入密码"
                required
              />
            </div>
          </div>

          <div v-if="errMsg" class="ks-login__error" role="alert">{{ errMsg }}</div>

          <button type="submit" class="ks-submit-btn" :disabled="loading" data-testid="login-submit">
            {{ loading ? '登录中...' : '立即登录' }}
          </button>
        </form>

        <div class="ks-login__hint">
          <div class="ks-login__hint-row">
            <span>平台管理员</span>
            <code class="ki-mono">admin / LiveOps#2026!</code>
          </div>
          <div class="ks-login__hint-row">
            <span>运维工程师</span>
            <code class="ki-mono">operator / Operator#2026!</code>
          </div>
          <div class="ks-login__hint-row">
            <span>安全审计员</span>
            <code class="ki-mono">auditor / Auditor#2026!</code>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<style scoped>
.ks-login {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 1.12fr 1fr;
  background-color: var(--ki-bg-canvas);
}

@media (max-width: 960px) {
  .ks-login {
    grid-template-columns: 1fr;
  }

  .ks-login__hero {
    display: none;
  }
}

.ks-login__hero {
  position: relative;
  padding: 56px 52px;
  display: flex;
  align-items: center;
  background: var(--ki-login-hero-bg);
  color: var(--ki-login-hero-text);
  overflow: hidden;
}

.ks-login__hero::before {
  content: "";
  position: absolute;
  inset: 0;
  background: var(--ki-login-hero-glow);
  pointer-events: none;
}

.ks-login__hero-inner {
  position: relative;
  max-width: 520px;
}

.ks-login__badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 12px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-login-hero-badge-bg);
  border: 1px solid var(--ki-login-hero-badge-border);
  color: var(--ki-login-hero-badge-text);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
}

.ks-login__badge-dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-login-hero-accent);
}

.ks-login__headline {
  margin: 22px 0 14px;
  font-size: 34px;
  line-height: 1.26;
  font-weight: var(--ki-fw-bold);
  letter-spacing: var(--ki-tracking-tight);
}

.ks-login__lead {
  margin: 0 0 28px;
  font-size: 14px;
  line-height: 1.65;
  color: var(--ki-login-hero-secondary);
}

.ks-login__pillars {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ks-pillar {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px;
  border-radius: var(--ki-radius-lg);
  background-color: var(--ki-login-hero-surface);
  border: 1px solid var(--ki-login-hero-border);
}

.ks-pillar__icon {
  color: var(--ki-login-hero-accent);
  margin-top: 2px;
  flex: none;
}

.ks-pillar__title {
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-semibold);
}

.ks-pillar__desc {
  font-size: var(--ki-fs-xs);
  color: var(--ki-login-hero-muted);
  margin-top: 3px;
}

.ks-login__main {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 32px;
}

.ks-login__card {
  width: 100%;
  max-width: 420px;
  padding: 36px 32px;
  border-radius: var(--ki-radius-xl);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-default);
  box-shadow: var(--ki-shadow-modal);
}

.ks-login__brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 26px;
}

.ks-login__mark {
  width: 36px;
  height: 36px;
  border-radius: var(--ki-radius-lg);
  background: var(--ki-brand-logo-bg);
  box-shadow: var(--ki-brand-logo-shadow);
}

.ks-login__brand-name {
  font-size: 18px;
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-login__brand-sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-login__form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ks-field label {
  display: block;
  margin-bottom: 6px;
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-secondary);
}

.ks-input-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 40px;
  padding: 0 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-strong);
  background-color: var(--ki-bg-surface);
}

.ks-input-wrap:focus-within {
  border-color: var(--ki-border-focus);
  box-shadow: var(--ki-focus-ring);
}

.ks-input-icon {
  color: var(--ki-text-muted);
  flex: none;
}

.ks-input-wrap input {
  width: 100%;
  border: none;
  background: transparent;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-body);
}

.ks-input-wrap input:focus-visible {
  outline: 0 solid transparent;
}

.ks-login__error {
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
  font-size: var(--ki-fs-sm);
}

.ks-submit-btn {
  height: 42px;
  border: none;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-brand-solid);
  color: var(--ki-text-on-solid);
  font-size: var(--ki-fs-sub);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
  margin-top: 4px;
}

.ks-submit-btn:disabled {
  opacity: 0.65;
  cursor: not-allowed;
}

.ks-submit-btn:hover:not(:disabled) {
  background-color: var(--ki-brand-solid-hover);
}

.ks-login__hint {
  margin-top: 22px;
  padding-top: 16px;
  border-top: 1px solid var(--ki-border-subtle);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ks-login__hint-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}
</style>
