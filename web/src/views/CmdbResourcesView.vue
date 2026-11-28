<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Download,
  GitBranch,
  Plus,
  RefreshCw,
  Terminal,
  Trash2,
  Upload,
} from 'lucide-vue-next'
import { apiDownload, apiRequest, apiUpload } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import WebTerminalDrawer from '@/components/cmdb/WebTerminalDrawer.vue'

interface ModelAttr {
  attrKey: string
  name: string
  fieldType: string
  required: boolean
  defaultValue: string
  options: string[]
}

interface CmdbModel {
  code: string
  name: string
  instanceCount: number
  attrs?: ModelAttr[]
}

interface HostEndpoint {
  ip: string
  sshPort: number
  sshUser: string
  credentialId: string
  execMode: string
  agentId: string
  agentStatus: string
  osFamily: string
  osVersion: string
  arch: string
  cpuCores: number
  memMb: number
  loadAvg: string
}

interface CIItem {
  id: string
  modelCode: string
  uniqueKey: string
  name: string
  env: string
  status: string
  owner: string
  tags: string[]
  data: Record<string, unknown>
  host?: HostEndpoint
  updatedAt: string
}

interface DynamicGroup {
  id: string
  name: string
  modelCode: string
  matchCount: number
}

interface CredentialItem {
  id: string
  name: string
  kind: string
  username: string
}

interface LineageGraph {
  rootCiId: string
  nodes: CIItem[]
  edges: Array<{ id: string; sourceCiId: string; targetCiId: string; relationCode: string; note: string }>
}

interface HistoryRow {
  id: string
  action: string
  actor: string
  createdAt: string
  afterData: Record<string, unknown>
}

const loading = ref(false)
const models = ref<CmdbModel[]>([])
const cis = ref<CIItem[]>([])
const dynGroups = ref<DynamicGroup[]>([])
const credentials = ref<CredentialItem[]>([])

const activeModelCode = ref('m_host')
const activeDynGroup = ref('')
const filterEnv = ref('')
const keyword = ref('')

// WebSSH Drawer
const sshOpen = ref(false)
const sshHost = reactive({ id: '', name: '', address: '' })

// Create / Edit CI Dialog
const showCIDialog = ref(false)
const editingId = ref('')
const ciForm = reactive({
  uniqueKey: '',
  name: '',
  env: 'prod',
  status: 'active',
  owner: 'SRE 核心组',
  tagsRaw: '',
  data: {} as Record<string, string>,
  sshPort: 22,
  sshUser: 'root',
  credentialId: '',
  execMode: 'auto',
})

// Lineage & History Drawer
const showDetailDrawer = ref(false)
const detailCI = ref<CIItem | null>(null)
const lineage = ref<LineageGraph | null>(null)
const historyRows = ref<HistoryRow[]>([])
const newRelTarget = ref('')
const newRelCode = ref('depends_on')

const activeModel = computed(() => models.value.find((m) => m.code === activeModelCode.value) || null)

async function loadMeta() {
  const [mRes, dRes, cRes] = await Promise.all([
    apiRequest<{ items: CmdbModel[] }>('GET', '/api/v1/cmdb/models?includeAttrs=true'),
    apiRequest<{ items: DynamicGroup[] }>('GET', '/api/v1/cmdb/dynamic-groups'),
    apiRequest<{ items: CredentialItem[] }>('GET', '/api/v1/iam/credentials'),
  ])
  models.value = mRes.data.items || []
  dynGroups.value = dRes.data.items || []
  credentials.value = cRes.data.items || []
}

async function loadCIs() {
  loading.value = true
  try {
    if (activeDynGroup.value) {
      const { data } = await apiRequest<{ items: CIItem[] }>('GET', `/api/v1/cmdb/dynamic-groups/${activeDynGroup.value}/evaluate`)
      cis.value = data.items || []
    } else {
      const qs = new URLSearchParams()
      if (activeModelCode.value) qs.set('modelCode', activeModelCode.value)
      if (filterEnv.value) qs.set('env', filterEnv.value)
      if (keyword.value.trim()) qs.set('q', keyword.value.trim())
      const { data } = await apiRequest<{ items: CIItem[] }>('GET', `/api/v1/cmdb/cis?${qs.toString()}`)
      cis.value = data.items || []
    }
  } finally {
    loading.value = false
  }
}

async function loadAll() {
  await loadMeta()
  await loadCIs()
}

function selectModel(code: string) {
  activeModelCode.value = code
  activeDynGroup.value = ''
  void loadCIs()
}

function openCreateCI() {
  editingId.value = ''
  ciForm.uniqueKey = ''
  ciForm.name = ''
  ciForm.env = 'prod'
  ciForm.status = 'active'
  ciForm.owner = 'SRE 核心组'
  ciForm.tagsRaw = ''
  ciForm.sshPort = 22
  ciForm.sshUser = 'root'
  ciForm.credentialId = ''
  ciForm.execMode = 'auto'
  const initData: Record<string, string> = {}
  for (const a of activeModel.value?.attrs || []) {
    initData[a.attrKey] = a.defaultValue || ''
  }
  ciForm.data = initData
  showCIDialog.value = true
}

function openEditCI(row: CIItem) {
  editingId.value = row.id
  ciForm.uniqueKey = row.uniqueKey
  ciForm.name = row.name
  ciForm.env = row.env
  ciForm.status = row.status
  ciForm.owner = row.owner
  ciForm.tagsRaw = (row.tags || []).join(',')
  ciForm.sshPort = row.host?.sshPort || 22
  ciForm.sshUser = row.host?.sshUser || 'root'
  ciForm.credentialId = row.host?.credentialId || ''
  ciForm.execMode = row.host?.execMode || 'auto'
  const d: Record<string, string> = {}
  for (const a of activeModel.value?.attrs || []) {
    d[a.attrKey] = row.data?.[a.attrKey] !== undefined ? String(row.data[a.attrKey]) : ''
  }
  ciForm.data = d
  showCIDialog.value = true
}

async function saveCI() {
  try {
    const payload: Record<string, unknown> = {
      modelCode: activeModelCode.value,
      uniqueKey: ciForm.uniqueKey || ciForm.data.ip || '',
      name: ciForm.name,
      env: ciForm.env,
      status: ciForm.status,
      owner: ciForm.owner,
      tags: ciForm.tagsRaw.split(',').map((s) => s.trim()).filter(Boolean),
      data: ciForm.data,
    }
    if (activeModelCode.value === 'm_host') {
      payload.host = {
        ip: ciForm.data.ip || ciForm.uniqueKey,
        sshPort: ciForm.sshPort,
        sshUser: ciForm.sshUser,
        credentialId: ciForm.credentialId,
        execMode: ciForm.execMode,
      }
    }
    if (editingId.value) {
      await apiRequest('PUT', `/api/v1/cmdb/cis/${editingId.value}`, { body: payload })
      ElMessage.success('资产已更新')
    } else {
      await apiRequest('POST', '/api/v1/cmdb/cis', { body: payload })
      ElMessage.success('资产已录入 CMDB')
    }
    showCIDialog.value = false
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  }
}

async function deleteCI(row: CIItem) {
  await ElMessageBox.confirm(`确认删除资产「${row.name}」？`, '删除确认', { type: 'warning' })
  await apiRequest('DELETE', `/api/v1/cmdb/cis/${row.id}`)
  ElMessage.success('资产已删除')
  await loadAll()
}

function openWebSSH(row: CIItem) {
  sshHost.id = row.id
  sshHost.name = row.name
  sshHost.address = `${row.host?.sshUser || 'root'}@${row.host?.ip || row.uniqueKey}:${row.host?.sshPort || 22}`
  sshOpen.value = true
}

async function collectFacts(row: CIItem) {
  try {
    await apiRequest('POST', `/api/v1/cmdb/cis/${row.id}/collect`)
    ElMessage.success(`已完成 ${row.name} 白名单主机事实采集并回填 CMDB`)
    await loadCIs()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '采集失败，请确认主机 Agent 在线或已绑定 SSH 凭据')
  }
}

async function openDetail(row: CIItem) {
  detailCI.value = row
  showDetailDrawer.value = true
  const [lin, hist] = await Promise.all([
    apiRequest<LineageGraph>('GET', `/api/v1/cmdb/cis/${row.id}/lineage?depth=2`),
    apiRequest<{ items: HistoryRow[] }>('GET', `/api/v1/cmdb/cis/${row.id}/history`),
  ])
  lineage.value = lin.data
  historyRows.value = hist.data.items || []
}

async function addLineageEdge() {
  if (!detailCI.value || !newRelTarget.value) return
  await apiRequest('POST', '/api/v1/cmdb/ci-relations', {
    body: { sourceCiId: detailCI.value.id, targetCiId: newRelTarget.value, relationCode: newRelCode.value },
  })
  ElMessage.success('实例血缘关系已建立')
  newRelTarget.value = ''
  await openDetail(detailCI.value)
}

async function downloadTemplate() {
  await apiDownload(`/api/v1/cmdb/models/${activeModelCode.value}/excel/template`, `cmdb-${activeModelCode.value}-template.xlsx`)
  ElMessage.success('已下载标准 Excel 导入模板 (.xlsx)')
}

async function exportXLSX() {
  await apiDownload(`/api/v1/cmdb/models/${activeModelCode.value}/excel/export`, `cmdb-${activeModelCode.value}-export.xlsx`)
  ElMessage.success('已导出当前模型资产 Excel (.xlsx)')
}

async function onImportXLSX(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const fd = new FormData()
  fd.append('file', file)
  try {
    const res = await apiUpload<{ created: number; updated: number; failed: number; errors: string[] }>(
      `/api/v1/cmdb/models/${activeModelCode.value}/excel/import`,
      fd
    )
    ElMessage.success(`Excel 导入完成：新增 ${res.created} 条，更新 ${res.updated} 条，失败 ${res.failed} 条`)
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '导入失败')
  } finally {
    input.value = ''
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="res-page">
    <PageHeader
      title="统一资产台账 (CMDB SSOT)"
      subtitle="主机即 m_host CI · WebSSH · 白名单采集 · 原生 Excel 导入导出"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="downloadTemplate"><Download :size="14" class="btn-ico" />Excel 模板</el-button>
        <el-button @click="exportXLSX"><Download :size="14" class="btn-ico" />导出 Excel</el-button>
        <label class="ki-upload-btn">
          <Upload :size="14" />导入 Excel
          <input type="file" accept=".xlsx" @change="onImportXLSX" />
        </label>
        <el-button type="primary" @click="openCreateCI"><Plus :size="14" class="btn-ico" />录入资产</el-button>
      </template>
    </PageHeader>

    <div class="ki-card">
      <div class="ki-pill-tabs model-tabs">
        <button
          v-for="m in models"
          :key="m.code"
          type="button"
          class="ki-pill-tab"
          :class="{ 'ki-pill-tab--active': !activeDynGroup && activeModelCode === m.code }"
          @click="selectModel(m.code)"
        >
          <span>{{ m.name }}</span>
          <span class="ki-pill-tab__count ki-num">{{ m.instanceCount }}</span>
        </button>
      </div>

      <div class="filter-bar">
        <el-input v-model="keyword" placeholder="搜索名称 / IP / 唯一键 / 负责人…" clearable style="width: 240px" @keyup.enter="loadCIs" />
        <el-select v-model="filterEnv" placeholder="全部环境" clearable style="width: 120px" @change="loadCIs">
          <el-option label="prod" value="prod" />
          <el-option label="beta" value="beta" />
          <el-option label="uat" value="uat" />
          <el-option label="sit" value="sit" />
          <el-option label="dev" value="dev" />
        </el-select>
        <el-select v-model="activeDynGroup" placeholder="按动态分组筛选…" clearable style="width: 210px" @change="loadCIs">
          <el-option v-for="g in dynGroups" :key="g.id" :label="`${g.name} (${g.matchCount})`" :value="g.id" />
        </el-select>
        <el-button @click="loadCIs">查询</el-button>
      </div>

      <el-table :data="cis" v-loading="loading" size="small">
        <el-table-column prop="name" label="资产名称" min-width="150" />
        <el-table-column label="唯一键 / IP" width="145">
          <template #default="{ row }"><code class="ki-mono">{{ row.host?.ip || row.uniqueKey }}</code></template>
        </el-table-column>
        <el-table-column prop="env" label="环境" width="76" />
        <el-table-column v-if="activeModelCode === 'm_host'" label="执行通道 / 规格" min-width="210">
          <template #default="{ row }">
            <div class="host-meta">
              <KiStatusBadge
                :status="row.host?.agentStatus === 'ONLINE' ? 'ok' : 'cancelled'"
                :label="row.host?.agentStatus === 'ONLINE' ? 'Agent 在线' : `SSH :${row.host?.sshPort || 22}`"
              />
              <span class="ki-mono ki-muted">{{ row.host?.arch || 'amd64' }} · {{ row.host?.cpuCores || row.data?.cpu_cores || 0 }}C / {{ row.host?.memMb ? Math.round(row.host.memMb / 1024) : (row.data?.mem_gb || 0) }}G</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-else label="核心属性摘要" min-width="210">
          <template #default="{ row }">
            <span class="ki-mono ki-muted">
              {{ Object.entries(row.data || {}).slice(0, 3).map(([k, v]) => `${k}=${v}`).join(' · ') || '—' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="owner" label="负责人" width="105" />
        <el-table-column label="操作" width="248" fixed="right">
          <template #default="{ row }">
            <div class="ki-row-actions">
              <el-button v-if="row.modelCode === 'm_host'" link type="primary" size="small" @click="openWebSSH(row)">
                <Terminal :size="13" class="btn-ico" />终端
              </el-button>
              <el-button v-if="row.modelCode === 'm_host'" link type="primary" size="small" @click="collectFacts(row)">
                <RefreshCw :size="12" class="btn-ico" />采集
              </el-button>
              <el-button link type="primary" size="small" @click="openDetail(row)">
                <GitBranch :size="12" class="btn-ico" />血缘
              </el-button>
              <el-button link size="small" @click="openEditCI(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="deleteCI(row)"><Trash2 :size="12" /></el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="showCIDialog" :title="editingId ? '编辑资产实例' : `录入 ${activeModel?.name || 'CI'} 资产`" width="560px">
      <el-form label-position="top">
        <div class="two-col">
          <el-form-item label="资产名称"><el-input v-model="ciForm.name" placeholder="his-app-01" /></el-form-item>
          <el-form-item label="唯一键 (主机填管理 IP)"><el-input v-model="ciForm.uniqueKey" placeholder="10.20.10.50" /></el-form-item>
        </div>
        <div class="three-col">
          <el-form-item label="环境">
            <el-select v-model="ciForm.env" style="width: 100%">
              <el-option v-for="e in ['prod', 'beta', 'uat', 'sit', 'dev']" :key="e" :label="e" :value="e" />
            </el-select>
          </el-form-item>
          <el-form-item label="负责人"><el-input v-model="ciForm.owner" /></el-form-item>
          <el-form-item label="标签 (逗号分隔)"><el-input v-model="ciForm.tagsRaw" placeholder="core,linux" /></el-form-item>
        </div>

        <template v-if="activeModelCode === 'm_host'">
          <div class="section-title">统一主机执行端点 (Agent / SSH)</div>
          <div class="three-col">
            <el-form-item label="SSH 端口"><el-input-number v-model="ciForm.sshPort" :min="1" :max="65535" style="width: 100%" /></el-form-item>
            <el-form-item label="SSH 用户"><el-input v-model="ciForm.sshUser" /></el-form-item>
            <el-form-item label="执行模式">
              <el-select v-model="ciForm.execMode" style="width: 100%">
                <el-option label="自动优先 Agent" value="auto" />
                <el-option label="强制 Agent" value="agent" />
                <el-option label="仅 SSH 无代理" value="ssh" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="绑定 KMS 加密 SSH 凭据">
            <el-select v-model="ciForm.credentialId" clearable placeholder="可在「权限审计与设置」中录入加密凭据" style="width: 100%">
              <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
            </el-select>
          </el-form-item>
        </template>

        <div class="section-title">模型动态属性 Schema</div>
        <div class="two-col">
          <el-form-item v-for="a in activeModel?.attrs || []" :key="a.attrKey" :label="`${a.name}${a.required ? ' *' : ''}`">
            <el-select v-if="a.fieldType === 'enum' && a.options?.length" v-model="ciForm.data[a.attrKey]" style="width: 100%">
              <el-option v-for="o in a.options" :key="o" :label="o" :value="o" />
            </el-select>
            <el-input v-else v-model="ciForm.data[a.attrKey]" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="showCIDialog = false">取消</el-button>
        <el-button type="primary" @click="saveCI">保存入库</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="showDetailDrawer" :title="`多跳血缘影响面与变更历史 · ${detailCI?.name || ''}`" size="560px">
      <div v-if="detailCI">
        <div class="section-title">关联上下游实例 ({{ lineage?.edges?.length || 0 }} 条边)</div>
        <div class="rel-add">
          <el-select v-model="newRelCode" style="width: 120px" size="small">
            <el-option label="runs_on" value="runs_on" />
            <el-option label="depends_on" value="depends_on" />
            <el-option label="uplink_to" value="uplink_to" />
            <el-option label="routes_to" value="routes_to" />
          </el-select>
          <el-select v-model="newRelTarget" placeholder="选择目标 CI 实例" filterable size="small" style="flex: 1">
            <el-option v-for="c in cis.filter((x) => x.id !== detailCI?.id)" :key="c.id" :label="`${c.name} (${c.modelCode})`" :value="c.id" />
          </el-select>
          <el-button type="primary" size="small" :disabled="!newRelTarget" @click="addLineageEdge">关联</el-button>
        </div>

        <el-table :data="lineage?.edges || []" size="small" empty-text="暂无上下游血缘关系">
          <el-table-column label="源 CI" min-width="120">
            <template #default="{ row }">{{ lineage?.nodes.find((n) => n.id === row.sourceCiId)?.name || row.sourceCiId }}</template>
          </el-table-column>
          <el-table-column prop="relationCode" label="关系" width="100" />
          <el-table-column label="目标 CI" min-width="120">
            <template #default="{ row }">{{ lineage?.nodes.find((n) => n.id === row.targetCiId)?.name || row.targetCiId }}</template>
          </el-table-column>
        </el-table>

        <div class="section-title">属性变更审计快照 ({{ historyRows.length }})</div>
        <el-table :data="historyRows" size="small">
          <el-table-column prop="action" label="动作" width="80" />
          <el-table-column prop="actor" label="操作者" width="110" />
          <el-table-column prop="createdAt" label="时间" min-width="160" />
        </el-table>
      </div>
    </el-drawer>

    <WebTerminalDrawer
      v-model="sshOpen"
      :host-id="sshHost.id"
      :host-name="sshHost.name"
      :host-address="sshHost.address"
    />
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.model-tabs {
  margin-bottom: var(--ki-space-3);
}

.host-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.three-col {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}

.rel-add {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
</style>
