<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { KeyRound, Lock, ShieldCheck, User } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import KiBrandLogo from '@/components/ki/KiBrandLogo.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const form = reactive({
  username: 'admin',
  password: '',
})
const loading = ref(false)
const errMsg = ref('')

async function onSubmit() {
  errMsg.value = ''
  loading.value = true
  try {
    await auth.loginLocal(form.username.trim(), form.password)
    await ws.loadWorkspaces()
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/dashboard'
    await router.push(redirect)
  } catch (e: unknown) {
    errMsg.value = e instanceof Error ? e.message : '登录失败，请检查账号和密码'
  } finally {
    loading.value = false
  }
}

function fillDefault() {
  form.username = 'admin'
  form.password = 'Kinstall#2026!'
}
</script>

<template>
  <div class="login-page">
    <div class="login-shell ki-card">
      <div class="login-aside">
        <div class="login-brand">
          <KiBrandLogo :size="36" variant="badge" />
          <div>
            <h1>Kinstall v2</h1>
            <p>全能动态元模型 CMDB 与声明式交付中枢</p>
          </div>
        </div>

        <ul class="login-points">
          <li>
            <ShieldCheck :size="16" class="pt-ico" />
            <div>
              <b>单一真实来源 (CMDB SSOT)</b>
              <span>动态元模型、蓝鲸双拓扑、42U 机柜与 IPAM、Excel 导入导出</span>
            </div>
          </li>
          <li>
            <KeyRound :size="16" class="pt-ico" />
            <div>
              <b>多环境配置与国密 KMS 密钥中心</b>
              <span>五环境隔离、AES-256/SM4 信封加密、审计显密与快照回滚</span>
            </div>
          </li>
          <li>
            <Lock :size="16" class="pt-ico" />
            <div>
              <b>零信任出站 Agent + SSH 双模执行面</b>
              <span>声明式 YAML 插件包、Kahn DAG 灰度编排与原生 Go OCI 引擎</span>
            </div>
          </li>
        </ul>

        <div class="login-foot ki-muted">
          CGO_ENABLED=0 静态单二进制 · SQLite WAL / PostgreSQL 双栈
        </div>
      </div>

      <div class="login-main">
        <h2>控制台登录</h2>
        <p class="login-sub">请输入管理员或运维成员账号进入工作空间</p>

        <el-alert v-if="errMsg" :title="errMsg" type="error" show-icon :closable="false" class="login-alert" />

        <el-form label-position="top" @submit.prevent="onSubmit">
          <el-form-item label="用户名">
            <el-input v-model="form.username" placeholder="admin" size="large" autocomplete="username">
              <template #prefix><User :size="15" /></template>
            </el-input>
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              placeholder="请输入登录密码"
              size="large"
              autocomplete="current-password"
              @keyup.enter="onSubmit"
            >
              <template #prefix><Lock :size="15" /></template>
            </el-input>
          </el-form-item>

          <el-button type="primary" size="large" class="login-btn" :loading="loading" native-type="submit">
            登录进入平台
          </el-button>
        </el-form>

        <div class="login-hint">
          <span>默认初始化账号：<code class="ki-mono">admin</code> / <code class="ki-mono">Kinstall#2026!</code></span>
          <el-button link type="primary" size="small" @click="fillDefault">一键填入</el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
  background-color: var(--ki-bg-canvas);
}

.login-shell {
  width: min(880px, 100%);
  display: grid;
  grid-template-columns: 1.05fr 1fr;
  padding: 0;
  overflow: hidden;
}

.login-aside {
  padding: 36px 32px;
  background-color: var(--ki-bg-subtle);
  border-right: 1px solid var(--ki-border-default);
  display: flex;
  flex-direction: column;
  gap: 28px;
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
}

.login-brand h1 {
  font-size: 20px;
  color: var(--ki-text-primary);
}

.login-brand p {
  margin: 2px 0 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.login-points {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.login-points li {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.pt-ico {
  flex: none;
  margin-top: 2px;
  color: var(--ki-brand);
}

.login-points b {
  display: block;
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.login-points span {
  display: block;
  margin-top: 2px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.login-foot {
  margin-top: auto;
  font-size: var(--ki-fs-xs);
}

.login-main {
  padding: 38px 36px;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.login-main h2 {
  font-size: 20px;
  color: var(--ki-text-primary);
}

.login-sub {
  margin: 6px 0 22px;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-muted);
}

.login-alert {
  margin-bottom: 16px;
}

.login-btn {
  width: 100%;
  margin-top: 6px;
}

.login-hint {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--ki-border-subtle);
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

@media (max-width: 767px) {
  .login-shell {
    grid-template-columns: 1fr;
  }

  .login-aside {
    display: none;
  }
}
</style>
