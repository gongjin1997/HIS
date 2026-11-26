<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'

const ws = useWorkspaceStore()
const newProj = reactive({ name: '', description: '' })
const tokName = ref('ci-deployer')
const issuedToken = ref('')

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
    ElMessage.success('API Token 已签发（仅本次明文展示）')
  } catch (e) {
    ElMessage.error(String(e))
  }
}
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">平台系统设置与 API Token 管理</h1>
        <div class="ki-muted" style="font-size: 12px">多租户项目隔离、双主题/双密度控制与短期 API Token 签发 (ki_&lt;8&gt;_&lt;32&gt;)</div>
      </div>
    </div>

    <div class="ki-grid-2">
      <div class="ki-panel">
        <h2 style="font-size: var(--ki-fs-section); margin-bottom: 12px">新建租户项目</h2>
        <el-form label-position="top">
          <el-form-item label="项目标识"><el-input v-model="newProj.name" placeholder="his-prod" class="ki-mono" /></el-form-item>
          <el-form-item label="说明"><el-input v-model="newProj.description" placeholder="核心生产集群" /></el-form-item>
          <el-button type="primary" @click="createProject">创建项目</el-button>
        </el-form>
      </div>

      <div class="ki-panel">
        <h2 style="font-size: var(--ki-fs-section); margin-bottom: 12px">签发 CLI / CI 专用 API Token</h2>
        <el-form label-position="top">
          <el-form-item label="Token 名称"><el-input v-model="tokName" class="ki-mono" /></el-form-item>
          <el-button type="primary" @click="createToken">签发 90 天 Token</el-button>
        </el-form>
        <div v-if="issuedToken" style="margin-top: 12px; padding: 10px; border-radius: 4px; background: var(--ki-status-ok-subtle); color: var(--ki-status-ok-text)" class="ki-mono">
          {{ issuedToken }}
        </div>
      </div>
    </div>
  </div>
</template>
