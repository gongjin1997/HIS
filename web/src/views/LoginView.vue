<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { KeyRound, Lock, ShieldCheck, User, Workflow } from 'lucide-vue-next'
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
      : '/platform/overview'
    await router.push(target)
  } catch (e) {
    errMsg.value = e instanceof ApiError ? `${e.code}: ${e.message}` : String(e)
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
          <span>LiveOps Foundation v1 · KubeDesign Console</span>
        </div>

        <h1 class="ks-login__headline">企业级多租户<br />云原生运维治理底座</h1>
        <p class="ks-login__lead">
          严格贯彻六边形干净架构、Composite (tenant_id, app_id) 物理隔离、KMS v2 双算法信封加密、DAG 事务级作业调度与零信任外联节点底座。
        </p>

        <div class="ks-login__pillars">
          <div class="ks-pillar">
            <ShieldCheck :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">多租户隔离与 HMAC 防篡改审计</div>
              <div class="ks-pillar__desc">Default-Deny RBAC + 渐进登录锁定 + 全链 HMAC-SHA256 验签</div>
            </div>
          </div>
          <div class="ks-pillar">
            <KeyRound :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">CMDB 双拓扑 &amp; KMS 配置快照</div>
              <div class="ks-pillar__desc">单父无环业务/资源树、If-Match 强版本锁、AES-256/SM4 密文审计揭示</div>
            </div>
          </div>
          <div class="ks-pillar">
            <Workflow :size="18" class="ks-pillar__icon" />
            <div>
              <div class="ks-pillar__title">40 项核心事务不变量 (D01–U09)</div>
              <div class="ks-pillar__desc">Idempotency-Key 幂等、lease_epoch 围栏、严格 Last-Event-ID SSE 流</div>
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
            <div class="ks-login__brand-name">LiveOps 控制台</div>
            <div class="ks-login__brand-sub">Platform · Workspace · Project 三级治理</div>
          </div>
        </div>

        <form class="ks-login__form" @submit.prevent="onSubmit">
          <div class="ks-field">
            <label for="ks-username">账号</label>
            <div class="ks-input-wrap">
              <User :size="15" class="ks-input-icon" />
              <input
                id="ks-username"
                v-model="username"
                type="text"
                autocomplete="username"
                placeholder="输入用户名"
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
                placeholder="输入登录密码"
                required
              />
            </div>
          </div>

          <div v-if="errMsg" class="ks-login__error" role="alert">{{ errMsg }}</div>

          <button type="submit" class="ks-submit-btn" :disabled="loading" data-testid="login-submit">
            {{ loading ? '安全认证中...' : '登录控制台' }}
          </button>
        </form>

        <div class="ks-login__hint">
          <div class="ks-login__hint-row">
            <span>平台超管</span>
            <code class="ki-mono">admin / LiveOps#2026!</code>
          </div>
          <div class="ks-login__hint-row">
            <span>空间运维</span>
            <code class="ki-mono">operator / Operator#2026!</code>
          </div>
          <div class="ks-login__hint-row">
            <span>合规审计</span>
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
  max-width: 540px;
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
