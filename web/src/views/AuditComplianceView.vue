<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { FileCheck2, Download } from 'lucide-vue-next'
import { apiDownload, apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiAuditHashBadge from '@/components/domain/KiAuditHashBadge.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()
const { formatAbsTime } = useKiFormat()

const loading = ref(false)
const verifying = ref(false)
const downloading = ref(false)
const logs = ref<Array<Record<string, unknown>>>([])
const verifyRes = ref<{ valid: boolean; totalChecked: number; lastSeq: number; brokenAtSeq?: number } | null>(null)
const compReport = ref<Record<string, unknown> | null>(null)
const highlightSeq = ref<number>(0)

const latestHash = computed(() => String(logs.value[0]?.recordSha256 || ''))
const latestSeq = computed(() => Number(verifyRes.value?.lastSeq ?? logs.value[0]?.seq ?? 0))

async function loadLogs() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/audit`
    )
    logs.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function verifyChain() {
  verifying.value = true
  try {
    const { data } = await apiRequest<{ valid: boolean; totalChecked: number; lastSeq: number; brokenAtSeq?: number }>('POST', '/api/v1/audit:verify')
    verifyRes.value = data
    if (data.valid) ElMessage.success(`审计链 1..${data.lastSeq} 校验 100% 完整无篡改`)
    else ElMessage.error(`告警：序号 #${data.brokenAtSeq} 检测到篡改！`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    verifying.value = false
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

async function downloadEvidence() {
  if (!ws.currentProjectId) return
  downloading.value = true
  try {
    const res = await apiDownload(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/compliance:download`,
      'kinstall-compliance-evidence.tar.gz'
    )
    ElMessage.success(`已下载 ${res.filename} (${res.size} B · SHA-256 ${res.sha256.slice(0, 12)}…)`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    downloading.value = false
  }
}

const auditRowClass = ({ row }: { row: Record<string, unknown> }) => {
  if (highlightSeq.value && Number(row.seq) === highlightSeq.value) return 'ki-row-danger'
  return ''
}

watch(() => ws.currentProjectId, loadLogs)
onMounted(loadLogs)
</script>

<template>
  <div>
    <PageHeader title="审计与合规" :refresh="loadLogs" :refreshing="loading" />

    <KiAuditHashBadge
      :valid="verifyRes ? verifyRes.valid : true"
      :last-seq="latestSeq"
      :total-checked="verifyRes ? verifyRes.totalChecked : logs.length"
      :broken-at-seq="verifyRes?.brokenAtSeq || 0"
      :sm3-enabled="true"
      :latest-hash="latestHash"
      :verifying="verifying"
      class="ki-mb-4"
      @verify="verifyChain"
      @jump-tamper="(seq) => (highlightSeq = seq)"
    />

    <div v-if="compReport" class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>{{ compReport.standard }} 等保 / 密评自动取证评估结果</span>
        <div class="ki-flex-row">
          <KiMonoText :value="String(compReport.evidenceTarGzSha256 || '')" :head="16" :tail="8" />
          <el-button size="small" type="primary" plain :loading="downloading" @click="downloadEvidence">
            <Download :size="13" class="btn-ico" /> 下载证据包 (.tar.gz)
          </el-button>
        </div>
      </div>
      <el-table :data="(compReport.controls as Array<Record<string, unknown>>) || []" size="small">
        <el-table-column label="条款" width="130">
          <template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template>
        </el-table-column>
        <el-table-column prop="category" label="标准" width="120" />
        <el-table-column prop="title" label="控制项" />
        <el-table-column label="结论" width="110">
          <template #default="{ row }"><KiStatusBadge :status="String(row.status)" /></template>
        </el-table-column>
        <el-table-column prop="evidence" label="自动证据" />
      </el-table>
    </div>

    <div class="ki-card">
      <div class="table-heading">
        <span class="ki-secondary">单调递增防分叉 SHA-256 + 国密 SM3 双哈希审计流水</span>
        <div class="table-actions">
          <el-button
            v-if="auth.can('compliance:report', ws.currentProjectId)"
            :loading="downloading"
            @click="downloadEvidence"
          >
            <Download :size="14" class="btn-ico" /> 下载取证归档 (.tar.gz)
          </el-button>
          <el-button v-if="auth.can('compliance:report', ws.currentProjectId)" type="primary" @click="generateCompliance">
            <FileCheck2 :size="14" class="btn-ico" /> 生成等保密评证据报告
          </el-button>
        </div>
      </div>

      <el-table v-loading="loading" :data="logs" size="small" :row-class-name="auditRowClass">
        <el-table-column label="SEQ" width="80">
          <template #default="{ row }"><b class="ki-mono">#{{ row.seq }}</b></template>
        </el-table-column>
        <el-table-column label="发生时间" width="180">
          <template #default="{ row }"><span class="ki-mono">{{ formatAbsTime(String(row.occurredAt || '')) }}</span></template>
        </el-table-column>
        <el-table-column label="操作动作" width="200">
          <template #default="{ row }"><span class="ki-mono">{{ (row.entry as Record<string, unknown>)?.action }}</span></template>
        </el-table-column>
        <el-table-column label="目标对象" min-width="200">
          <template #default="{ row }">
            <span class="ki-mono">{{ (row.entry as Record<string, unknown>)?.targetType }}/{{ (row.entry as Record<string, unknown>)?.targetId }}</span>
          </template>
        </el-table-column>
        <el-table-column label="SHA-256 链哈希" min-width="220">
          <template #default="{ row }">
            <KiMonoText :value="String(row.recordSha256 || '')" :head="16" :tail="8" />
          </template>
        </el-table-column>
        <template #empty><el-empty description="暂无审计日志记录" :image-size="60" /></template>
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}
</style>
