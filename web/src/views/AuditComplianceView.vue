<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { FileCheck2, Download, Search, ShieldCheck } from 'lucide-vue-next'
import { apiDownload, apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiAuditHashBadge from '@/components/domain/KiAuditHashBadge.vue'

interface AuditEntryPayload {
  actorId?: string
  actorType?: string
  action?: string
  targetType?: string
  targetId?: string
  result?: string
}

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const ws = useWorkspaceStore()
const { formatAbsTime } = useKiFormat()

const activeTab = ref<'audit' | 'compliance'>(route.path === '/security/compliance' ? 'compliance' : 'audit')
const loading = ref(false)
const verifying = ref(false)
const generating = ref(false)
const downloading = ref(false)

const logs = ref<Array<Record<string, unknown>>>([])
const userMap = ref<Record<string, string>>({})
const keyword = ref('')
const resultFilter = ref<'all' | 'ok' | 'failed'>('all')
const verifyRes = ref<{ valid: boolean; totalChecked: number; lastSeq: number; brokenAtSeq?: number } | null>(null)
const compReport = ref<Record<string, unknown> | null>(null)
const highlightSeq = ref<number>(0)

const complianceStandards = [
  { id: 'DJBH-L3-01', category: '等保 2.0 三级', item: '身份鉴别与强口令（Argon2id + 5 级 RBAC）' },
  { id: 'DJBH-L3-02', category: '等保 2.0 三级', item: '安全审计与防篡改双哈希链（SHA-256 + SM3）' },
  { id: 'DJBH-L3-03', category: '等保 2.0 三级', item: '生产变更双人审批与最小权限控制' },
  { id: 'GMT-0054-01', category: '密评 GM/T-0054', item: '凭据信封加密存储与主密钥 KEK 轮换能力' },
  { id: 'GMT-0054-02', category: '密评 GM/T-0054', item: '制品交付完整性校验（SHA-256 清单核验）' },
]

function getEntry(row: Record<string, unknown>): AuditEntryPayload {
  return (row.entry || row.Entry || {}) as AuditEntryPayload
}

function resolveActor(row: Record<string, unknown>): string {
  const e = getEntry(row)
  const raw = String(e.actorId || 'system')
  return userMap.value[raw] || raw
}

function formatTarget(row: Record<string, unknown>): string {
  const e = getEntry(row)
  const parts = [e.targetType, e.targetId].filter((x) => Boolean(x && String(x).trim()))
  return parts.length > 0 ? parts.join(' / ') : '—'
}

function resultStatus(result?: string): string {
  const low = String(result || 'success').toLowerCase()
  if (low === 'denied' || low === 'failure' || low === 'failed') return 'failed'
  return 'ok'
}

function resultLabel(result?: string): string {
  const low = String(result || 'success').toLowerCase()
  if (low === 'denied') return '已拒绝'
  if (low === 'failure' || low === 'failed') return '失败'
  return '成功'
}

const sortedLogs = computed(() =>
  [...logs.value].sort((a, b) => Number(b.seq || 0) - Number(a.seq || 0))
)

const filteredLogs = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  return sortedLogs.value.filter((row) => {
    const e = getEntry(row)
    const st = resultStatus(e.result)
    if (resultFilter.value !== 'all' && st !== resultFilter.value) return false
    if (!kw) return true
    const hay = [
      String(row.seq || ''),
      resolveActor(row),
      e.actorId || '',
      e.action || '',
      e.targetType || '',
      e.targetId || '',
      String(row.recordSha256 || ''),
    ]
      .join(' ')
      .toLowerCase()
    return hay.includes(kw)
  })
})

const latestHash = computed(() => String(sortedLogs.value[0]?.recordSha256 || ''))
const latestSeq = computed(() => Number(verifyRes.value?.lastSeq ?? sortedLogs.value[0]?.seq ?? 0))

async function loadLogs() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [aRes, uRes] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/audit`),
      apiRequest<{ items: Array<{ id: string; username: string; displayName?: string }> }>('GET', '/api/v1/users').catch(
        () => ({ data: { items: [] } })
      ),
    ])
    logs.value = aRes.data.items || []
    const map: Record<string, string> = {}
    for (const u of uRes.data.items || []) {
      map[u.id] = u.displayName ? `${u.username} (${u.displayName})` : u.username
    }
    userMap.value = map
  } finally {
    loading.value = false
  }
}

async function verifyChain() {
  verifying.value = true
  try {
    const { data } = await apiRequest<{ valid: boolean; totalChecked: number; lastSeq: number; brokenAtSeq?: number }>(
      'POST',
      '/api/v1/audit:verify'
    )
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
  generating.value = true
  try {
    const { data } = await apiRequest<{ report: Record<string, unknown>; evidenceTarGzSha256: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/compliance:report`
    )
    compReport.value = { ...data.report, evidenceTarGzSha256: data.evidenceTarGzSha256 }
    activeTab.value = 'compliance'
    ElMessage.success('等保 2.0 三级 + 密评 GM/T-0054 证据包已生成')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    generating.value = false
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

function onTabChange(name: string | number) {
  const target = name === 'compliance' ? '/security/compliance' : '/security/audit'
  if (route.path !== target) {
    void router.replace(target)
  }
}

const auditRowClass = ({ row }: { row: Record<string, unknown> }) => {
  if (highlightSeq.value && Number(row.seq) === highlightSeq.value) return 'ki-row-danger'
  return ''
}

watch(
  () => route.path,
  (p) => {
    activeTab.value = p === '/security/compliance' ? 'compliance' : 'audit'
  }
)

watch(() => ws.currentProjectId, () => {
  compReport.value = null
  void loadLogs()
})
onMounted(loadLogs)
</script>

<template>
  <div>
    <PageHeader
      title="审计与合规"
      subtitle="单调递增防篡改 SHA-256 + 国密 SM3 双哈希审计链与等保 2.0 / 密评自动取证中心"
      :refresh="loadLogs"
      :refreshing="loading"
    >
      <template #actions>
        <el-button
          v-if="auth.can('compliance:report', ws.currentProjectId)"
          :loading="downloading"
          @click="downloadEvidence"
        >
          <Download :size="14" class="btn-ico" /> 下载取证归档 (.tar.gz)
        </el-button>
        <el-button
          v-if="auth.can('compliance:report', ws.currentProjectId)"
          type="primary"
          :loading="generating"
          @click="generateCompliance"
        >
          <FileCheck2 :size="14" class="btn-ico" /> 生成等保密评证据报告
        </el-button>
      </template>
    </PageHeader>

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
      @jump-tamper="(seq) => { activeTab = 'audit'; highlightSeq = seq }"
    />

    <div class="ki-card">
      <el-tabs v-model="activeTab" @tab-change="onTabChange">
        <el-tab-pane :label="`审计哈希链 (${filteredLogs.length})`" name="audit">
          <div class="filter-bar ki-mb-3">
            <el-input
              v-model="keyword"
              clearable
              placeholder="搜索操作主体、动作、目标对象或哈希前缀…"
              class="search-input"
            >
              <template #prefix><Search :size="14" /></template>
            </el-input>
            <el-radio-group v-model="resultFilter" size="small">
              <el-radio-button value="all">全部 ({{ sortedLogs.length }})</el-radio-button>
              <el-radio-button value="ok">成功</el-radio-button>
              <el-radio-button value="failed">失败 / 拒绝</el-radio-button>
            </el-radio-group>
          </div>

          <el-table v-loading="loading" :data="filteredLogs" size="small" :row-class-name="auditRowClass">
            <el-table-column label="SEQ" width="76">
              <template #default="{ row }"><b class="ki-mono">#{{ row.seq }}</b></template>
            </el-table-column>
            <el-table-column label="发生时间" width="172">
              <template #default="{ row }"><span class="ki-mono">{{ formatAbsTime(String(row.occurredAt || '')) }}</span></template>
            </el-table-column>
            <el-table-column label="操作主体" min-width="150">
              <template #default="{ row }">
                <span v-if="userMap[String(getEntry(row).actorId || '')]" class="actor-name">{{ resolveActor(row) }}</span>
                <KiMonoText v-else :value="getEntry(row).actorId || 'system'" :head="10" :tail="4" />
              </template>
            </el-table-column>
            <el-table-column label="操作动作" width="190">
              <template #default="{ row }">
                <span class="ki-badge ki-mono action-pill">{{ getEntry(row).action || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="目标对象" min-width="190">
              <template #default="{ row }">
                <span class="ki-mono">{{ formatTarget(row) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="结果" width="90">
              <template #default="{ row }">
                <KiStatusBadge :status="resultStatus(getEntry(row).result)" :label="resultLabel(getEntry(row).result)" />
              </template>
            </el-table-column>
            <el-table-column label="SHA-256 链哈希" min-width="200">
              <template #default="{ row }">
                <KiMonoText :value="String(row.recordSha256 || '')" :head="14" :tail="6" />
              </template>
            </el-table-column>
            <template #empty><el-empty description="暂无匹配的审计日志记录" :image-size="60" /></template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="等保密评中心" name="compliance">
          <div v-if="compReport" class="ki-mb-4">
            <div class="table-heading">
              <span class="section-title no-mb">{{ compReport.standard }} 自动取证评估结果</span>
              <div class="ki-flex-row">
                <span class="ki-muted">证据包 SHA-256:</span>
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
              <el-table-column prop="category" label="标准" width="130" />
              <el-table-column prop="title" label="控制项" min-width="180" />
              <el-table-column label="结论" width="110">
                <template #default="{ row }"><KiStatusBadge :status="String(row.status)" /></template>
              </el-table-column>
              <el-table-column prop="evidence" label="自动证据" min-width="220" />
            </el-table>
          </div>

          <div v-else class="comp-intro">
            <div class="comp-intro__head">
              <div class="ki-flex-row">
                <ShieldCheck :size="18" class="ki-text-brand" />
                <div>
                  <strong>等保 2.0 三级与商用密码应用安全性评估 (GM/T-0054) 自动核查</strong>
                  <div class="ki-muted comp-sub">点击右上角「生成等保密评证据报告」即可实时采集 RBAC、审计链、信封加密与离线校验项并产出取证包。</div>
                </div>
              </div>
              <el-button
                v-if="auth.can('compliance:report', ws.currentProjectId)"
                type="primary"
                :loading="generating"
                @click="generateCompliance"
              >
                <FileCheck2 :size="14" class="btn-ico" /> 立即生成评估报告
              </el-button>
            </div>
            <el-table :data="complianceStandards" size="small">
              <el-table-column prop="id" label="基线编号" width="130">
                <template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template>
              </el-table-column>
              <el-table-column prop="category" label="合规标准" width="150" />
              <el-table-column prop="item" label="核查维度" />
              <el-table-column label="当前状态" width="110">
                <template #default><KiStatusBadge status="unprobed" label="待生成" /></template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.no-mb {
  margin-bottom: 0;
}

.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.search-input {
  width: 320px;
}

.actor-name {
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-primary);
}

.action-pill {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  border: 1px solid var(--ki-border-subtle);
}

.comp-intro__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 16px;
  margin-bottom: 14px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.comp-sub {
  font-size: var(--ki-fs-xs);
  margin-top: 2px;
}
</style>
