<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CheckCircle2, FileCheck2, RefreshCw, ShieldAlert } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api } from '@/api/client'

interface AuditRow {
  audit_id: string
  seq: number
  actor_name: string
  action: string
  resource_type: string
  resource_id: string
  outcome: string
  signature: string
  created_at: string
}

const ACTION_LABELS: Record<string, string> = {
  BOOTSTRAP_SEED: '系统初始化',
  AUTH_LOGIN_SUCCESS: '登录控制台',
  AUTH_LOGIN_FAILED: '登录密码失败',
  TENANT_CREATE: '创建企业空间',
  APP_CREATE: '创建应用',
  APP_KEY_ROTATE: '轮换应用密钥',
  APP_KEY_REVOKE: '吊销应用密钥',
  PROJECT_CREATE: '创建项目',
  PROJECT_ROLE_PERMS_REPLACE: '更新项目权限',
  USER_CREATE: '创建用户',
  CMDB_INSTANCE_CREATE: '新增 CMDB 资产',
  CMDB_INSTANCE_UPDATE: '更新 CMDB 资产',
  CMDB_EDGE_CREATE: '新增拓扑关系',
  CMDB_CASCADE_DELETE: '级联清理资产',
  CONFIG_CREATE: '新建配置集',
  CONFIG_UPDATE: '发布配置版本',
  CONFIG_SECRET_REVEAL: '查看加密明文',
  DEPLOY_JOB_SUBMIT: '提交发布作业',
  DEPLOY_JOB_CANCEL: '取消发布作业',
  NODE_ENROLL_TOKEN_ISSUE: '签发节点接入令牌',
  NODE_CRED_ROTATE: '轮换节点凭据',
  GW_ROUTE_CREATE: '新增网关路由',
  GW_DEFAULT_ROUTE_SWITCH: '切换默认路由',
}

const logs = ref<AuditRow[]>([])
const keyword = ref('')
const loading = ref(false)
const verifying = ref(false)
const verifyRes = ref<{ valid: boolean; total: number; brokenAt: number; reason?: string } | null>(null)

const filtered = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return logs.value
  return logs.value.filter(
    (l) =>
      l.actor_name.toLowerCase().includes(q) ||
      l.action.toLowerCase().includes(q) ||
      (ACTION_LABELS[l.action] || '').includes(q) ||
      l.resource_id.toLowerCase().includes(q)
  )
})

function fmtTime(iso: string) {
  if (!iso) return '-'
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: AuditRow[] }>('/api/v1/iam/audit/logs?limit=100&allTenants=true')
    logs.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function runVerify(showToast = false) {
  verifying.value = true
  try {
    const { data } = await api.post<{ valid: boolean; total: number; brokenAt: number; reason?: string }>('/api/v1/iam/audit/verify')
    verifyRes.value = data
    if (showToast) {
      if (data.valid) ElMessage.success(`已完成全量 ${data.total} 条审计记录的完整性校验，数据未被篡改`)
      else ElMessage.error(`审计记录校验异常：${data.reason}`)
    }
  } finally {
    verifying.value = false
  }
}

onMounted(async () => {
  await Promise.all([load(), runVerify(false)])
})
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="操作审计"
      description="完整记录平台登录、权限变更、配置发布、敏感密文查阅与节点管控操作，支持随时执行全量防篡改校验。"
      :icon="FileCheck2"
      :badge="verifyRes?.valid ? `完整性已验证 (${verifyRes.total} 条)` : '校验中'"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" :loading="verifying" @click="runVerify(true)">
          <CheckCircle2 :size="14" /><span class="ks-gap">校验日志完整性</span>
        </el-button>
      </template>
    </KsBanner>

    <div v-if="verifyRes" class="ki-card ks-verify-strip" :class="verifyRes.valid ? 'is-ok' : 'is-bad'">
      <CheckCircle2 v-if="verifyRes.valid" :size="17" />
      <ShieldAlert v-else :size="17" />
      <span>
        {{ verifyRes.valid ? `审计日志完整性校验通过：全部 ${verifyRes.total} 条操作记录链路连续且签名一致。` : `发现异常记录：${verifyRes.reason}` }}
      </span>
    </div>

    <div class="ki-card">
      <div class="ks-bar">
        <el-input v-model="keyword" placeholder="按操作人、行为类型或资源标识检索..." clearable style="max-width: 320px" />
      </div>
      <el-table :data="filtered" size="small">
        <el-table-column prop="seq" label="序号" width="70">
          <template #default="{ row }"><span class="ki-mono">#{{ row.seq }}</span></template>
        </el-table-column>
        <el-table-column label="操作行为" width="180">
          <template #default="{ row }">
            <span class="ki-badge ks-pill-blue">{{ ACTION_LABELS[row.action] || row.action }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="actor_name" label="操作人" width="110" />
        <el-table-column prop="resource_id" label="操作对象" min-width="200">
          <template #default="{ row }"><span class="ki-mono">{{ row.resource_id }}</span></template>
        </el-table-column>
        <el-table-column label="操作时间" width="170">
          <template #default="{ row }"><span class="ki-muted">{{ fmtTime(row.created_at) }}</span></template>
        </el-table-column>
        <el-table-column label="结果" width="100">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.outcome === 'SUCCESS' ? 'ks-pill-ok' : 'ks-pill-danger'">
              {{ row.outcome === 'SUCCESS' ? '成功' : '拒绝' }}
            </span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-bar {
  margin-bottom: var(--ki-space-3);
}

.ks-verify-strip {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: var(--ki-space-4);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
}

.ks-verify-strip.is-ok {
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-verify-strip.is-bad {
  background-color: var(--ki-status-danger-subtle);
  border-color: var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}
</style>
