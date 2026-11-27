<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { KeyRound, RefreshCw, FolderPlus, ShieldCheck } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import KiMonoText from '@/components/ki/KiMonoText.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()

const newProj = reactive({ name: '', description: '' })
const tokName = ref('ci-deployer')
const issuedToken = ref('')
const rewrapping = ref(false)
const rewrappedCount = ref<number | null>(null)

async function createProject() {
  if (!newProj.name.trim()) return
  try {
    await apiRequest('POST', '/api/v1/projects', { body: { ...newProj }, idempotent: true })
    ElMessage.success('项目已创建')
    newProj.name = ''
    newProj.description = ''
    await ws.loadProjects()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function createToken() {
  try {
    const { data } = await apiRequest<{ token: string }>('POST', '/api/v1/auth/tokens', {
      body: { name: tokName.value, scopes: ['*'], ttlDays: 90 },
      idempotent: true,
    })
    issuedToken.value = data.token
    ElMessage.success('API Token 已签发（仅本次明文展示，请妥善保存）')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function rewrapSecrets() {
  if (!ws.currentProjectId) return
  rewrapping.value = true
  try {
    const { data } = await apiRequest<{ rewrapped: number }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/secrets:rewrap`,
      { idempotent: true }
    )
    rewrappedCount.value = data.rewrapped
    ElMessage.success(`KEK 信封密钥重包装完成：已轮换 ${data.rewrapped} 条凭据`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    rewrapping.value = false
  }
}
</script>

<template>
  <div class="settings-grid ki-grid-2">
    <div class="ki-card">
      <div class="section-title">新建租户项目</div>
      <el-form label-position="top">
        <el-form-item label="项目标识"><el-input v-model="newProj.name" placeholder="his-prod" class="ki-mono" /></el-form-item>
        <el-form-item label="项目说明"><el-input v-model="newProj.description" placeholder="核心生产集群" /></el-form-item>
        <el-button type="primary" @click="createProject">
          <FolderPlus :size="14" class="btn-ico" /> 创建项目
        </el-button>
      </el-form>
    </div>

    <div class="ki-card">
      <div class="section-title">签发 CLI / CI 专用 API Token</div>
      <el-form label-position="top">
        <el-form-item label="Token 名称"><el-input v-model="tokName" class="ki-mono" /></el-form-item>
        <el-button type="primary" @click="createToken">
          <KeyRound :size="14" class="btn-ico" /> 签发 90 天 Token
        </el-button>
      </el-form>
      <div v-if="issuedToken" class="token-out ki-mt-3">
        <KiMonoText :value="issuedToken" :ellipsis="false" />
      </div>
    </div>

    <div class="ki-card">
      <div class="section-title">信封加密密钥轮换 (KEK Rewrap · O-11)</div>
      <p class="ki-secondary settings-desc">
        使用最新主密钥版本重新封装当前项目内所有主机与中间件凭据的 DEK，零停机、全过程写入审计哈希链。
      </p>
      <div class="ki-flex-row">
        <el-button
          v-if="auth.can('secrets:rewrap', ws.currentProjectId)"
          type="primary"
          plain
          :loading="rewrapping"
          @click="rewrapSecrets"
        >
          <RefreshCw :size="14" class="btn-ico" /> 立即重包装项目凭据
        </el-button>
        <span v-if="rewrappedCount !== null" class="ki-text-ok ki-mono">已完成：{{ rewrappedCount }} 条</span>
      </div>
    </div>

    <div class="ki-card">
      <div class="section-title">外观与界面偏好</div>
      <div class="kv-grid">
        <div class="k">界面主题</div>
        <div>
          <el-radio-group :model-value="ws.themePreference" size="small" @update:model-value="(v: string) => ws.setTheme(v as 'light' | 'dark' | 'system')">
            <el-radio-button value="light">浅色（默认）</el-radio-button>
            <el-radio-button value="dark">暗色</el-radio-button>
            <el-radio-button value="system">跟随系统</el-radio-button>
          </el-radio-group>
        </div>
        <div class="k">行密度</div>
        <div>
          <el-radio-group v-model="ws.density" size="small">
            <el-radio-button value="comfortable">舒适 (44px)</el-radio-button>
            <el-radio-button value="compact">紧凑 (34px)</el-radio-button>
          </el-radio-group>
        </div>
        <div class="k">当前角色</div>
        <div class="ki-flex-row"><ShieldCheck :size="14" class="ki-text-ok" /> <b class="ki-mono">{{ auth.primaryRole }}</b></div>
        <div class="k">界面语言</div>
        <div><el-button size="small" @click="ws.toggleLang">{{ ws.lang }}</el-button></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.settings-desc {
  margin: 0 0 12px;
  font-size: var(--ki-fs-sm);
}

.token-out {
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
  word-break: break-all;
}
</style>
