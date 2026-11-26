<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ShieldCheck, FileCheck2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiStatusBadge from '@/components/KiStatusBadge.vue'

const ws = useWorkspaceStore()
const logs = ref<Array<Record<string, unknown>>>([])
const verifyRes = ref<Record<string, unknown> | null>(null)
const compReport = ref<Record<string, unknown> | null>(null)

async function loadLogs() {
  if (!ws.currentProjectId) return
  const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/audit`
  )
  logs.value = data.items || []
}

async function verifyChain() {
  try {
    const { data } = await apiRequest<Record<string, unknown>>('POST', '/api/v1/audit:verify')
    verifyRes.value = data
    if (data.valid) ElMessage.success(`审计链 1..${data.lastSeq} 校验 100% 完整无篡改`)
    else ElMessage.error(`告警：序号 #${data.brokenAtSeq} 检测到篡改！`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function generateCompliance() {
  try {
    const { data } = await apiRequest<{ report: Record<string, unknown>; evidenceTarGzSha256: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/compliance:report`
    )
    compReport.value = { ...data.report, evidenceTarGzSha256: data.evidenceTarGzSha256 }
    ElMessage.success('等保 2.0 三级 + 密评 GM/T-0054 证据包已生成')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadLogs)
onMounted(loadLogs)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">审计哈希链与等保 / 密评合规中心</h1>
        <div class="ki-muted" style="font-size: 12px">单调递增防分叉 SHA-256 + 国密 SM3 双哈希链 · 等保 2.0 三级 · GM/T-0054 · Tongsuo TLCP</div>
      </div>
      <div class="ki-flex-row">
        <el-button type="primary" @click="verifyChain"><ShieldCheck :size="14" style="margin-right: 4px" /> 立即校验哈希链完整性</el-button>
        <el-button @click="generateCompliance"><FileCheck2 :size="14" style="margin-right: 4px" /> 生成合规证据报告</el-button>
      </div>
    </div>

    <div v-if="verifyRes" class="ki-panel" style="margin-bottom: 14px">
      <div class="ki-flex-between">
        <div class="ki-flex-row">
          <KiStatusBadge :status="verifyRes.valid ? 'valid' : 'error'" />
          <span class="ki-mono">已校验 {{ verifyRes.totalChecked }} 条记录 · 最新序号 #{{ verifyRes.lastSeq }}</span>
        </div>
        <span class="ki-muted ki-mono" style="font-size: 12px">SHA-256 / SM3 双链核验</span>
      </div>
    </div>

    <div v-if="compReport" class="ki-panel" style="margin-bottom: 14px">
      <div class="ki-flex-between" style="margin-bottom: 10px">
        <strong>{{ compReport.standard }} 合规评估结果</strong>
        <span class="ki-mono ki-muted" style="font-size: 11px">evidence.tar.gz SHA-256: {{ compReport.evidenceTarGzSha256 }}</span>
      </div>
      <el-table :data="(compReport.controls as Array<Record<string, unknown>>) || []" size="small">
        <el-table-column prop="id" label="条款" width="130"><template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template></el-table-column>
        <el-table-column prop="category" label="标准" width="110" />
        <el-table-column prop="title" label="控制项" />
        <el-table-column prop="status" label="结论" width="100"><template #default="{ row }"><KiStatusBadge :status="String(row.status)" /></template></el-table-column>
        <el-table-column prop="evidence" label="自动证据" />
      </el-table>
    </div>

    <div class="ki-panel">
      <el-table :data="logs" size="small">
        <el-table-column prop="seq" label="SEQ" width="75"><template #default="{ row }"><span class="ki-mono">#{{ row.seq }}</span></template></el-table-column>
        <el-table-column prop="occurredAt" label="时间" width="190"><template #default="{ row }"><span class="ki-mono">{{ row.occurredAt }}</span></template></el-table-column>
        <el-table-column label="动作" width="180"><template #default="{ row }"><span class="ki-mono">{{ row.entry?.action }}</span></template></el-table-column>
        <el-table-column label="目标" width="200"><template #default="{ row }"><span class="ki-mono">{{ row.entry?.targetType }}/{{ row.entry?.targetId }}</span></template></el-table-column>
        <el-table-column label="SHA-256 摘要"><template #default="{ row }"><span class="ki-mono ki-muted">{{ String(row.recordSha256 || '').slice(0, 24) }}...</span></template></el-table-column>
      </el-table>
    </div>
  </div>
</template>
