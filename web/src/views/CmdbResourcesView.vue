<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Background, Controls, Handle, Position, VueFlow, type Node, type Edge } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { Download, FileSpreadsheet, GitBranch, Link2, Plus, Search, Trash2, Upload } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiDownload, apiRequest, apiUpload } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'

interface ModelAttr {
  propId: string
  propName: string
  fieldType: string
  unit?: string
  required: boolean
  readonly: boolean
  enumOptions?: string[]
}

interface CMDBModel {
  code: string
  name: string
  instanceCount: number
  attributes: ModelAttr[]
}

interface CIInstance {
  id: string
  modelCode: string
  modelName: string
  name: string
  uniqueKey: string
  status: string
  env: string
  owner: string
  attrs: Record<string, unknown>
  version: number
  updatedAt: string
}

interface CIGraph {
  rootId: string
  nodes: Array<{ id: string; modelCode: string; modelName: string; name: string; status: string; env: string; depth: number; direction: string }>
  edges: Array<{ id: string; sourceCiId: string; targetCiId: string; relationType: string; label: string }>
}

interface CIHistory {
  id: string
  action: string
  actor: string
  before: Record<string, unknown>
  after: Record<string, unknown>
  createdAt: string
}

const ws = useWorkspaceStore()
const { formatRelTime } = useKiFormat()
const loading = ref(false)
const models = ref<CMDBModel[]>([])
const selectedModelCode = ref('m_host')
const keyword = ref('')
const items = ref<CIInstance[]>([])
const allCis = ref<CIInstance[]>([])

const formDialog = reactive({
  open: false,
  editId: '',
  name: '',
  uniqueKey: '',
  env: 'prod',
  status: 'active',
  owner: '',
  attrs: {} as Record<string, unknown>,
})

const xlsxDialog = reactive({
  open: false,
  file: null as File | null,
  loading: false,
  report: null as null | { dryRun: boolean; totalRows: number; created: number; updated: number; errors: number; rows: Array<{ row: number; name: string; action: string; message: string }> },
})

const graphDrawer = reactive({
  open: false,
  ci: null as CIInstance | null,
  tab: 'graph' as 'graph' | 'history',
  graph: null as CIGraph | null,
  history: [] as CIHistory[],
  targetCiId: '',
  relationType: 'depends_on',
  label: '依赖于',
})

const currentModel = computed(() => models.value.find((m) => m.code === selectedModelCode.value) || null)
const displayAttrs = computed(() => (currentModel.value?.attributes || []).slice(0, 3))

const graphFlowNodes = computed<Node[]>(() => {
  const g = graphDrawer.graph
  if (!g) return []
  const byCol: Record<number, typeof g.nodes> = {}
  for (const n of g.nodes) {
    const col = n.direction === 'upstream' ? -n.depth : n.direction === 'downstream' ? n.depth : 0
    if (!byCol[col]) byCol[col] = []
    byCol[col].push(n)
  }
  const out: Node[] = []
  for (const [colStr, list] of Object.entries(byCol)) {
    const col = Number(colStr)
    list.forEach((n, idx) => {
      out.push({
        id: n.id,
        type: 'ciNode',
        position: { x: 260 + col * 240, y: 60 + idx * 115 },
        data: { ...n, isRoot: n.id === g.rootId },
      })
    })
  }
  return out
})

const graphFlowEdges = computed<Edge[]>(() =>
  (graphDrawer.graph?.edges || []).map((e) => ({
    id: e.id,
    source: e.sourceCiId,
    target: e.targetCiId,
    label: e.label || e.relationType,
    type: 'smoothstep',
    animated: true,
  }))
)

async function loadModelsAndItems() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [mRes, ciRes, allRes] = await Promise.all([
      apiRequest<{ models: CMDBModel[] }>('GET', `/api/v1/projects/${pid}/cmdb/meta`),
      apiRequest<{ items: CIInstance[] }>('GET', `/api/v1/projects/${pid}/cmdb/cis?modelCode=${encodeURIComponent(selectedModelCode.value)}&q=${encodeURIComponent(keyword.value)}`),
      apiRequest<{ items: CIInstance[] }>('GET', `/api/v1/projects/${pid}/cmdb/cis`),
    ])
    models.value = mRes.data.models || []
    items.value = ciRes.data.items || []
    allCis.value = allRes.data.items || []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  formDialog.editId = ''
  formDialog.name = ''
  formDialog.uniqueKey = ''
  formDialog.env = 'prod'
  formDialog.status = 'active'
  formDialog.owner = ''
  formDialog.attrs = {}
  formDialog.open = true
}

function openEdit(ci: CIInstance) {
  formDialog.editId = ci.id
  formDialog.name = ci.name
  formDialog.uniqueKey = ci.uniqueKey
  formDialog.env = ci.env
  formDialog.status = ci.status
  formDialog.owner = ci.owner
  formDialog.attrs = { ...(ci.attrs || {}) }
  formDialog.open = true
}

async function saveCI() {
  if (!formDialog.name.trim()) {
    ElMessage.warning('请输入 CI 名称')
    return
  }
  try {
    const body = {
      modelCode: selectedModelCode.value,
      name: formDialog.name.trim(),
      uniqueKey: formDialog.uniqueKey.trim(),
      env: formDialog.env,
      status: formDialog.status,
      owner: formDialog.owner.trim(),
      attrs: formDialog.attrs,
    }
    if (formDialog.editId) {
      await apiRequest('PUT', `/api/v1/projects/${ws.currentProjectId}/cmdb/cis/${encodeURIComponent(formDialog.editId)}`, { body })
      ElMessage.success('CI 实例已更新并写入历史快照')
    } else {
      await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/cis`, { body })
      ElMessage.success('CI 实例已入库（动态 Schema 强校验通过）')
    }
    formDialog.open = false
    await loadModelsAndItems()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteCI(ci: CIInstance) {
  try {
    await ElMessageBox.confirm(`确认删除 CI 实例「${ci.name}」吗？`, '删除 CI', { type: 'warning' })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/cis/${encodeURIComponent(ci.id)}`)
    ElMessage.success('CI 实例已删除')
    await loadModelsAndItems()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function downloadTemplateXLSX() {
  const res = await apiDownload(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/cmdb/models/${encodeURIComponent(selectedModelCode.value)}/template.xlsx`,
    `cmdb-${selectedModelCode.value}-template.xlsx`
  )
  ElMessage.success(`已导出带枚举下拉校验的 Excel 模板：${res.filename}`)
}

async function exportXLSX() {
  const res = await apiDownload(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/cmdb/models/${encodeURIComponent(selectedModelCode.value)}/export.xlsx`,
    `cmdb-${selectedModelCode.value}-export.xlsx`
  )
  ElMessage.success(`已导出防公式注入 Excel 台账：${res.filename}`)
}

function onXlsxPicked(ev: Event) {
  const inp = ev.target as HTMLInputElement
  xlsxDialog.file = inp.files?.[0] || null
  xlsxDialog.report = null
}

async function runXlsxImport(dryRun: boolean) {
  if (!xlsxDialog.file) {
    ElMessage.warning('请先选择 .xlsx 文件')
    return
  }
  xlsxDialog.loading = true
  try {
    const fd = new FormData()
    fd.append('file', xlsxDialog.file)
    fd.append('dryRun', dryRun ? 'true' : 'false')
    const data = await apiUpload<typeof xlsxDialog.report>(
      `/api/v1/projects/${ws.currentProjectId}/cmdb/models/${encodeURIComponent(selectedModelCode.value)}/import.xlsx?dryRun=${dryRun}`,
      fd
    )
    xlsxDialog.report = data
    if (!dryRun) {
      ElMessage.success(`Excel 批量入库完成：新增 ${data?.created || 0} / 更新 ${data?.updated || 0} / 错误 ${data?.errors || 0}`)
      await loadModelsAndItems()
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    xlsxDialog.loading = false
  }
}

async function openGraphDrawer(ci: CIInstance) {
  graphDrawer.ci = ci
  graphDrawer.tab = 'graph'
  graphDrawer.open = true
  const pid = ws.currentProjectId
  const [gRes, hRes] = await Promise.all([
    apiRequest<CIGraph>('GET', `/api/v1/projects/${pid}/cmdb/cis/${encodeURIComponent(ci.id)}/graph?depth=3`),
    apiRequest<{ items: CIHistory[] }>('GET', `/api/v1/projects/${pid}/cmdb/ci-history?ciId=${encodeURIComponent(ci.id)}`),
  ])
  graphDrawer.graph = gRes.data
  graphDrawer.history = hRes.data.items || []
}

async function addCIRelation() {
  if (!graphDrawer.ci || !graphDrawer.targetCiId) return
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/ci-relations`, {
      body: {
        sourceCiId: graphDrawer.ci.id,
        targetCiId: graphDrawer.targetCiId,
        relationType: graphDrawer.relationType,
        label: graphDrawer.label || graphDrawer.relationType,
      },
    })
    ElMessage.success('CI 血缘关系已建立')
    graphDrawer.targetCiId = ''
    await openGraphDrawer(graphDrawer.ci)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(selectedModelCode, loadModelsAndItems)
watch(() => ws.currentProjectId, loadModelsAndItems)
onMounted(loadModelsAndItems)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="通用 CI 资源库与血缘图谱"
      subtitle="动态 Schema 表单、原生 Excel (.xlsx) 模板导入导出与多跳血缘影响面图"
      :refresh="loadModelsAndItems"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="downloadTemplateXLSX">
          <FileSpreadsheet :size="14" class="btn-ico" /> 下载 .xlsx 模板
        </el-button>
        <el-button @click="xlsxDialog.open = true">
          <Upload :size="14" class="btn-ico" /> Excel 导入
        </el-button>
        <el-button @click="exportXLSX">
          <Download :size="14" class="btn-ico" /> 导出 .xlsx
        </el-button>
        <el-button type="primary" @click="openCreate">
          <Plus :size="14" class="btn-ico" /> 新增 {{ currentModel?.name || 'CI' }}
        </el-button>
      </template>
    </PageHeader>

    <!-- 模型切换条 -->
    <div class="model-pills ki-mb-3">
      <button
        v-for="m in models"
        :key="m.code"
        type="button"
        class="m-pill"
        :class="{ 'm-pill--active': selectedModelCode === m.code }"
        @click="selectedModelCode = m.code"
      >
        <span>{{ m.name }}</span>
        <span class="ki-mono m-pill__cnt">{{ m.instanceCount }}</span>
      </button>
    </div>

    <div class="ki-card">
      <div class="toolbar ki-mb-3">
        <el-input v-model="keyword" clearable placeholder="检索 CI 名称、唯一标识、负责人或任意动态属性值…" class="search-inp" @keyup.enter="loadModelsAndItems">
          <template #prefix><Search :size="14" /></template>
        </el-input>
        <el-button @click="loadModelsAndItems">检索</el-button>
      </div>

      <el-table :data="items" size="small">
        <el-table-column prop="name" label="CI 名称" min-width="136">
          <template #default="{ row }">
            <b>{{ row.name }}</b>
            <div v-if="row.uniqueKey" class="ki-mono ki-muted sub-key">{{ row.uniqueKey }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="env" label="环境" width="64">
          <template #default="{ row }"><span class="ki-badge env-pill ki-mono">{{ row.env }}</span></template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="84">
          <template #default="{ row }"><KiStatusBadge :status="row.status === 'active' ? 'ok' : 'warn'" :label="row.status" /></template>
        </el-table-column>
        <el-table-column
          v-for="attr in displayAttrs"
          :key="attr.propId"
          :label="attr.propName"
          min-width="104"
        >
          <template #default="{ row }">
            <span class="ki-mono">{{ row.attrs?.[attr.propId] ?? '—' }}{{ row.attrs?.[attr.propId] != null && attr.unit ? ' ' + attr.unit : '' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="owner" label="负责人" width="90" />
        <el-table-column label="更新" width="76">
          <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(row.updatedAt) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="154" align="right">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="openGraphDrawer(row)">
              <GitBranch :size="12" class="btn-ico" /> 血缘图
            </el-button>
            <el-button link type="primary" size="small" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" size="small" @click="deleteCI(row)">
              <Trash2 :size="12" />
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <span class="ki-muted">当前模型「{{ currentModel?.name }}」暂无 CI 实例，点击右上角「新增」或「Excel 导入」批量入库。</span>
        </template>
      </el-table>
    </div>

    <!-- 动态 Schema 驱动的 CI 创建/编辑弹窗 -->
    <el-dialog v-model="formDialog.open" :title="`${formDialog.editId ? '编辑' : '新增'}「${currentModel?.name}」CI 实例`" width="580px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="CI 名称 *"><el-input v-model="formDialog.name" /></el-form-item>
          <el-form-item label="唯一键 (uniqueKey)"><el-input v-model="formDialog.uniqueKey" placeholder="留空自动取首个唯一属性" /></el-form-item>
        </div>
        <div class="ki-grid-3">
          <el-form-item label="所属环境">
            <el-select v-model="formDialog.env" class="ki-w-full">
              <el-option v-for="e in ['prod','beta','uat','sit','dev']" :key="e" :label="e" :value="e" />
            </el-select>
          </el-form-item>
          <el-form-item label="生命周期状态">
            <el-select v-model="formDialog.status" class="ki-w-full">
              <el-option label="active (服役中)" value="active" />
              <el-option label="maintenance (维护中)" value="maintenance" />
              <el-option label="offline (已下线)" value="offline" />
            </el-select>
          </el-form-item>
          <el-form-item label="负责人"><el-input v-model="formDialog.owner" /></el-form-item>
        </div>

        <div class="dyn-sec-title">动态模型属性（实时强校验 Schema）</div>
        <div class="ki-grid-2">
          <el-form-item
            v-for="attr in currentModel?.attributes || []"
            :key="attr.propId"
            :label="`${attr.propName}${attr.required ? ' *' : ''}${attr.unit ? ' (' + attr.unit + ')' : ''}`"
          >
            <el-select
              v-if="attr.fieldType === 'enum' && attr.enumOptions?.length"
              v-model="formDialog.attrs[attr.propId]"
              class="ki-w-full"
              clearable
            >
              <el-option v-for="opt in attr.enumOptions" :key="opt" :label="opt" :value="opt" />
            </el-select>
            <el-switch v-else-if="attr.fieldType === 'bool'" v-model="formDialog.attrs[attr.propId]" />
            <el-input-number
              v-else-if="attr.fieldType === 'int' || attr.fieldType === 'long' || attr.fieldType === 'double'"
              v-model="formDialog.attrs[attr.propId]"
              class="ki-w-full"
            />
            <el-input v-else v-model="formDialog.attrs[attr.propId]" :disabled="Boolean(formDialog.editId && attr.readonly)" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="formDialog.open = false">取消</el-button>
        <el-button type="primary" @click="saveCI">校验并保存</el-button>
      </template>
    </el-dialog>

    <!-- 原生 Excel (.xlsx) 导入弹窗 -->
    <el-dialog v-model="xlsxDialog.open" :title="`原生 Excel (.xlsx) 批量导入 · ${currentModel?.name}`" width="560px">
      <p class="ki-muted xlsx-tip">
        支持上传由「下载 .xlsx 模板」或「导出 .xlsx」生成的工作簿，后端使用 <code>excelize/v2</code> 按模型 Schema 逐行强校验并执行幂等覆盖。
      </p>
      <input type="file" accept=".xlsx" class="file-input ki-mb-3" @change="onXlsxPicked" />
      <div v-if="xlsxDialog.report" class="ki-mb-3">
        <div class="ki-flex-row ki-mb-2">
          <b>{{ xlsxDialog.report.dryRun ? '预检报告' : '入库结果' }}：</b>
          <span>总计 {{ xlsxDialog.report.totalRows }} 行 · 新增 {{ xlsxDialog.report.created }} · 更新 {{ xlsxDialog.report.updated }} · 错误 {{ xlsxDialog.report.errors }}</span>
        </div>
        <el-table :data="xlsxDialog.report.rows" size="small" max-height="220">
          <el-table-column prop="row" label="行号" width="64" />
          <el-table-column prop="name" label="CI 名称" min-width="120" />
          <el-table-column prop="action" label="动作" width="84" />
          <el-table-column prop="message" label="校验详情" min-width="160" />
        </el-table>
      </div>
      <template #footer>
        <el-button @click="xlsxDialog.open = false">关闭</el-button>
        <el-button :loading="xlsxDialog.loading" @click="runXlsxImport(true)">预检 Dry-Run</el-button>
        <el-button type="primary" :loading="xlsxDialog.loading" @click="runXlsxImport(false)">确认导入入库</el-button>
      </template>
    </el-dialog>

    <!-- 多跳 CI 血缘影响面图 & 变更历史抽屉 -->
    <KiDrawer v-model="graphDrawer.open" :title="`CI 血缘拓扑与变更审计 · ${graphDrawer.ci?.name || ''}`" size="lg">
      <el-tabs v-model="graphDrawer.tab">
        <el-tab-pane label="多跳上下游血缘影响面图" name="graph">
          <div class="rel-add-bar ki-mb-3">
            <el-select v-model="graphDrawer.relationType" size="small" style="width: 130px">
              <el-option label="依赖于" value="depends_on" />
              <el-option label="运行于" value="runs_on" />
              <el-option label="路由至" value="routes_to" />
              <el-option label="连接至" value="connects_to" />
            </el-select>
            <el-select v-model="graphDrawer.targetCiId" filterable size="small" placeholder="选择关联的目标 CI 实例…" style="flex: 1">
              <el-option
                v-for="c in allCis.filter((x) => x.id !== graphDrawer.ci?.id)"
                :key="c.id"
                :label="`[${c.modelName}] ${c.name}`"
                :value="c.id"
              />
            </el-select>
            <el-button type="primary" size="small" :disabled="!graphDrawer.targetCiId" @click="addCIRelation">
              <Link2 :size="13" class="btn-ico" /> 建立关联
            </el-button>
          </div>
          <div class="graph-box">
            <VueFlow :nodes="graphFlowNodes" :edges="graphFlowEdges" fit-view-on-init>
              <Background :gap="18" />
              <Controls />
              <template #node-ciNode="{ data }">
                <div class="ci-gnode" :class="{ 'ci-gnode--root': data.isRoot }">
                  <Handle type="target" :position="Position.Left" />
                  <div class="ci-gnode__m">{{ data.modelName }}</div>
                  <b>{{ data.name }}</b>
                  <div class="ci-gnode__env ki-mono">{{ data.env }} · {{ data.status }}</div>
                  <Handle type="source" :position="Position.Right" />
                </div>
              </template>
            </VueFlow>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="`变更快照历史 (${graphDrawer.history.length})`" name="history">
          <el-table :data="graphDrawer.history" size="small">
            <el-table-column prop="action" label="动作" width="84" />
            <el-table-column prop="actor" label="操作人" width="100" />
            <el-table-column label="变更后快照" min-width="260">
              <template #default="{ row }">
                <pre class="hist-json ki-mono">{{ JSON.stringify(row.after || {}) }}</pre>
              </template>
            </el-table-column>
            <el-table-column label="时间" width="96" align="right">
              <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(row.createdAt) }}</span></template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </KiDrawer>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.model-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.m-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 999px;
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  cursor: pointer;
}

.m-pill:hover {
  border-color: var(--ki-brand-primary);
  color: var(--ki-text-primary);
}

.m-pill--active {
  background-color: var(--ki-brand-primary);
  border-color: var(--ki-brand-primary);
  color: var(--ki-text-inverse);
  font-weight: 600;
}

.m-pill__cnt {
  font-size: 11px;
  opacity: 0.85;
}

.toolbar {
  display: flex;
  gap: 10px;
}

.search-inp {
  max-width: 380px;
}

.sub-key {
  font-size: 11px;
}

.env-pill {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.dyn-sec-title {
  font-size: var(--ki-fs-xs);
  font-weight: 600;
  color: var(--ki-text-primary);
  margin: 8px 0 10px;
  padding-top: 10px;
  border-top: 1px solid var(--ki-border-subtle);
}

.xlsx-tip {
  font-size: var(--ki-fs-xs);
  margin: 0 0 12px;
}

.file-input {
  width: 100%;
  padding: 10px;
  border: 1px dashed var(--ki-border-control);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
}

.rel-add-bar {
  display: flex;
  gap: 8px;
  align-items: center;
}

.graph-box {
  height: 440px;
  border: 1px solid var(--ki-border-subtle);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
}

.ci-gnode {
  min-width: 164px;
  padding: 9px 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-control);
  background-color: var(--ki-bg-surface);
  font-size: var(--ki-fs-xs);
}

.ci-gnode--root {
  border: 2px solid var(--ki-brand-primary);
  box-shadow: var(--ki-shadow-md);
}

.ci-gnode__m {
  font-size: 10px;
  color: var(--ki-brand-primary);
  font-weight: 600;
}

.ci-gnode__env {
  font-size: 11px;
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.hist-json {
  margin: 0;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
