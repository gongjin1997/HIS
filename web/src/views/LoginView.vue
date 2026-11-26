<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Lock, ShieldCheck, KeyRound } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { APIError } from '@/api/client'

const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()

const username = ref('admin')
const password = ref('')
const loading = ref(false)
const errMsg = ref('')
const errCode = ref('')
const suggestion = ref('')

async function onSubmit() {
  loading.value = true
  errMsg.value = ''
  errCode.value = ''
  suggestion.value = ''
  try {
    await auth.loginLocal(username.value.trim(), password.value)
    await ws.loadProjects()
    await ws.refreshApprovalBadge()
    await router.push('/dashboard')
  } catch (e) {
    if (e instanceof APIError) {
      errMsg.value = e.message
      errCode.value = e.code
      suggestion.value = e.suggestion
    } else {
      errMsg.value = String(e)
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div style="min-height: 100vh; display: flex; align-items: center; justify-content: center; background: var(--ki-bg-canvas); padding: 24px">
    <div class="ki-panel" style="width: 420px; padding: 28px">
      <div class="ki-flex-row" style="margin-bottom: 18px">
        <ShieldCheck :size="24" style="color: var(--ki-brand)" />
        <div>
          <h1 style="font-size: 18px">kinstall 控制平面</h1>
          <div class="ki-muted" style="font-size: 12px">企业离线基础设施自动化交付平台</div>
        </div>
      </div>

      <div
        v-if="errMsg"
        style="padding: 10px 12px; margin-bottom: 14px; border-radius: 4px; background: var(--ki-status-danger-subtle); color: var(--ki-status-danger-text); font-size: 12.5px"
      >
        <div class="ki-mono" style="font-weight: 600">{{ errCode }} · {{ errMsg }}</div>
        <div v-if="suggestion" style="margin-top: 4px; opacity: 0.9">{{ suggestion }}</div>
      </div>

      <el-form label-position="top" @submit.prevent="onSubmit">
        <el-form-item label="用户名">
          <el-input v-model="username" placeholder="admin" class="ki-mono" />
        </el-form-item>
        <el-form-item label="密码 (Argon2id · 5次失败锁定15分钟)">
          <el-input v-model="password" type="password" show-password placeholder="请输入登录密码" />
        </el-form-item>
        <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%; margin-top: 4px">
          <Lock :size="14" style="margin-right: 6px" />
          本地安全登录
        </el-button>
      </el-form>

      <div style="margin-top: 16px; padding-top: 14px; border-top: 1px solid var(--ki-border-subtle)">
        <el-button plain disabled style="width: 100%">
          <KeyRound :size="14" style="margin-right: 6px" />
          企业 OIDC / PKCE 单点登录（按需在服务端配置）
        </el-button>
      </div>
    </div>
  </div>
</template>
