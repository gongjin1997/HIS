<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CheckCircle2, FileCheck2, RefreshCw, ShieldAlert } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api } from '@/api/client'

interface AuditRow {
  audit_id: string
  seq: number
  tenant_id: string
  app_id: string
  actor_name: string
  action: string
  resource_type: string
  resource_id: string
  outcome: string
  prev_hash: string
  signature: string
  created_at: string
}

const logs = ref<AuditRow[]>([])
const loading = ref(false)
const verifying = ref(false)
const verifyRes = ref<{ valid: boolean; total: number; brokenAt: number; reason?: string } | null>(null)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: AuditRow[] }>('/api/v1/iam/audit/logs?limit=100&allTenants=true')
    logs.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function runVerify() {
  verifying.value = true
  try {
    const { data } = await api.post<{ valid: boolean; total: number; brokenAt: number; reason?: string }>('/api/v1/iam/audit/verify')
    verifyRes.value = data
    if (data.valid) {
      ElMessage.success(`全链 ${data.total} 条审计记录 HMAC-SHA256 完整性验签通过！`)
    } else {
      ElMessage.error(`审计链在第 ${data.brokenAt} 条检测到篡改：${data.reason}`)
    }
  } finally {
    verifying.value = false
  }
}

onMounted(async () => {
  await Promise.all([load(), runVerify()])
})
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="防篡改审计链 (HMAC-SHA256 + prev_hash)"
      description="所有登录、密钥轮换、CMDB 变更、KMS 密文揭示与 DAG 作业提交均按严格递增 seq 链接签名，任何离线改库都会立即触发验签告警。"
      :icon="FileCheck2"
      :badge="verifyRes?.valid ? `全链验签通过 (${verifyRes.total} 条)` : '验签中'"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" :loading="verifying" @click="runVerify">
          <CheckCircle2 :size="14" /><span class="ks-gap">立即执行全链验签</span>
        </el-button>
      </template>
    </KsBanner>

    <div v-if="verifyRes" class="ki-card ks-verify-strip" :class="verifyRes.valid ? 'is-ok' : 'is-bad'">
      <CheckCircle2 v-if="verifyRes.valid" :size="18" />
      <ShieldAlert v-else :size="18" />
      <span>
        {{ verifyRes.valid ? `审计哈希链完整无篡改：共验证 ${verifyRes.total} 条串联记录（GENESIS → #${verifyRes.total}）。` : `检测到篡改：${verifyRes.reason}` }}
      </span>
    </div>

    <div class="ki-card">
      <el-table :data="logs" size="small" stripe>
        <el-table-column prop="seq" label="Seq" width="70">
          <template #default="{ row }"><span class="ki-mono">#{{ row.seq }}</span></template>
        </el-table-column>
        <el-table-column prop="action" label="审计动作" width="210">
          <template #default="{ row }"><span class="ki-badge ks-pill-blue ki-mono">{{ row.action }}</span></template>
        </el-table-column>
        <el-table-column prop="actor_name" label="操作主体" width="120" />
        <el-table-column prop="resource_type" label="资源类型" width="130" />
        <el-table-column prop="resource_id" label="资源 ID" min-width="180">
          <template #default="{ row }"><span class="ki-mono">{{ row.resource_id }}</span></template>
        </el-table-column>
        <el-table-column label="HMAC 签名摘要" width="170">
          <template #default="{ row }"><span class="ki-mono">{{ row.signature.slice(0, 16) }}…</span></template>
        </el-table-column>
        <el-table-column prop="outcome" label="结果" width="112">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.outcome === 'SUCCESS' ? 'ks-pill-ok' : 'ks-pill-danger'">{{ row.outcome }}</span>
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
