<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { VueFlow, type Edge, type Node } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { Download, GitFork, Plus, RefreshCw, Trash2, Upload } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError, apiRequest } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface ModelField {
  field_key: string
  name: string
  field_type: string
  required: boolean
  is_unique: boolean
  enum_values?: string[]
  default_value?: string
}

interface AssetModel {
  model_id: string
  code: string
  name: string
  icon: string
  description: string
  instance_count: number
  fields: ModelField[]
}

interface AssetInstance {
  instance_id: string
  model_id: string
  model_code: string
  model_name: string
  name: string
  unique_key: string
  status: string
  attrs: Record<string, unknown>
  version: number
}

interface AssetEdge {
  edge_id: string
  tree_kind: string
  rel_type_id: string
  source_id: string
  target_id: string
}

const scope = useScopeStore()
const activeTab = ref('instances')
const loading = ref(false)
const models = ref<AssetModel[]>([])
const selectedModelId = ref('')
const keyword = ref('')
const instances = ref<AssetInstance[]>([])

// Topology state
const treeKind = ref<'business' | 'resource' | 'custom'>('business')
const topoInstances = ref<AssetInstance[]>([])
const topoEdges = ref<AssetEdge[]>([])
const edgeForm = ref({ sourceId: '', targetId: '', relTypeId: 'rel-contains' })

// Create Instance Modal
const createDlg = ref(false)
const instForm = ref({ modelId: '', name: '', uniqueKey: '', status: 'ACTIVE', attrs: {} as Record<string, string> })

const activeModel = computed(() => models.value.find((m) => m.model_id === (instForm.value.modelId || selectedModelId.value)))

const flowNodes = computed<Node[]>(() => {
  const edges = topoEdges.value
  const involved = new Set<string>()
  for (const e of edges) {
    involved.add(e.source_id)
    involved.add(e.target_id)
  }
  const nodesToShow = topoInstances.value.filter((i) => involved.size === 0 || involved.has(i.instance_id))
  const inDeg = new Map<string, number>()
  const children = new Map<string, string[]>()
  for (const n of nodesToShow) {
    inDeg.set(n.instance_id, 0)
    children.set(n.instance_id, [])
  }
  for (const e of edges) {
    inDeg.set(e.target_id, (inDeg.get(e.target_id) || 0) + 1)
    children.get(e.source_id)?.push(e.target_id)
  }
  const depth = new Map<string, number>()
  const queue: string[] = []
  for (const [id, d] of inDeg.entries()) {
    if (d === 0) {
      depth.set(id, 0)
      queue.push(id)
    }
  }
  while (queue.length > 0) {
    const u = queue.shift()!
    const d = depth.get(u) || 0
    for (const v of children.get(u) || []) {
      if (!depth.has(v)) {
        depth.set(v, d + 1)
        queue.push(v)
      }
    }
  }
  const rowCounter = new Map<number, number>()
  return nodesToShow.map((inst) => {
    const col = depth.get(inst.instance_id) ?? 0
    const row = rowCounter.get(col) || 0
    rowCounter.set(col, row + 1)
    return {
      id: inst.instance_id,
      data: { label: `[${inst.model_name}] ${inst.name}` },
      position: { x: 40 + col * 270, y: 36 + row * 96 },
    }
  })
})

const flowEdges = computed<Edge[]>(() =>
  topoEdges.value.map((e) => ({
    id: e.edge_id,
    source: e.source_id,
    target: e.target_id,
    animated: treeKind.value === 'custom',
  }))
)

async function loadAll() {
  loading.value = true
  try {
    const [mRes, iRes] = await Promise.all([
      api.get<{ items: AssetModel[] }>('/api/v1/assets/models'),
      api.get<{ items: AssetInstance[] }>(
        `/api/v1/assets/instances?modelId=${encodeURIComponent(selectedModelId.value)}&q=${encodeURIComponent(keyword.value)}`
      ),
    ])
    models.value = mRes.data.items || []
    instances.value = iRes.data.items || []
    await loadTopology()
  } finally {
    loading.value = false
  }
}

async function loadTopology() {
  const { data } = await api.get<{ instances: AssetInstance[]; edges: AssetEdge[] }>(
    `/api/v1/assets/topology?treeKind=${treeKind.value}`
  )
  topoInstances.value = data.instances || []
  topoEdges.value = data.edges || []
}

function openCreateDialog() {
  const mid = selectedModelId.value || models.value[0]?.model_id || ''
  instForm.value = { modelId: mid, name: '', uniqueKey: '', status: 'ACTIVE', attrs: {} }
  createDlg.value = true
}

async function submitCreateInstance() {
  try {
    await api.post('/api/v1/assets/instances', instForm.value)
    ElMessage.success('CMDB 资产实例创建成功（已完成字段校验与唯一索引入库）')
    createDlg.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

async function cascadeDelete(inst: AssetInstance) {
  try {
    await ElMessageBox.confirm(`确认对资产「${inst.name}」执行检查点级联删除？`, '确认级联删除', { type: 'warning' })
    const { data } = await api.del<{ deleted_items: number }>(`/api/v1/assets/instances/${inst.instance_id}`)
    ElMessage.success(`已完成检查点级联删除（共清理 ${data.deleted_items} 个实例）`)
    await loadAll()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function addEdge() {
  if (!edgeForm.value.sourceId || !edgeForm.value.targetId) return
  try {
    await api.post('/api/v1/assets/edges', {
      treeKind: treeKind.value,
      relTypeId: edgeForm.value.relTypeId,
      sourceId: edgeForm.value.sourceId,
      targetId: edgeForm.value.targetId,
    })
    ElMessage.success('拓扑边已创建（通过跨租户隔离、单父树与无环校验）')
    edgeForm.value = { sourceId: '', targetId: '', relTypeId: 'rel-contains' }
    await loadTopology()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

async function downloadXLSX(templateOnly: boolean) {
  const mid = selectedModelId.value || 'm_host'
  const suffix = templateOnly ? 'template.xlsx' : 'export.xlsx'
  const res = await fetch(`/api/v1/assets/models/${encodeURIComponent(mid)}/${suffix}`, {
    headers: {
      Authorization: `Bearer ${scope.token}`,
      'X-Tenant-Id': scope.tenantId,
      'X-App-Id': scope.appId,
    },
  })
  if (!res.ok) {
    ElMessage.error('导出 Excel 失败')
    return
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `cmdb-${mid}-${suffix}`
  a.click()
  URL.revokeObjectURL(url)
}

async function onImportFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const mid = selectedModelId.value || 'm_host'
  const fd = new FormData()
  fd.append('file', file)
  try {
    const { data } = await apiRequest<{ imported: number; skipped: number }>('POST', `/api/v1/assets/models/${mid}/import.xlsx`, {
      rawBody: fd,
    })
    ElMessage.success(`Excel 批量导入完成：成功 ${data.imported} 条，跳过 ${data.skipped} 条`)
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    input.value = ''
  }
}

onMounted(loadAll)
watch(() => [scope.tenantId, scope.appId], loadAll)
watch(selectedModelId, loadAll)
watch(treeKind, loadTopology)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="CMDB 资产库与双视角拓扑 (S01–S04)"
      description="LiveOps 8 类标准元模型、If-Match 强版本乐观锁、唯一字段索引、单父无环业务/资源双树以及原生 Excelize (.xlsx) 模板导入导出。"
      :icon="GitFork"
      :badge="`${instances.length} 项资产`"
      :tabs="[
        { key: 'instances', label: '资产实例台账', count: instances.length },
        { key: 'topology', label: '双视角拓扑画布', count: topoEdges.length },
        { key: 'models', label: '元模型定义', count: models.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="loadAll"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button @click="downloadXLSX(false)"><Download :size="14" /><span class="ks-gap">导出 .xlsx</span></el-button>
        <label class="ki-upload-btn">
          <Upload :size="14" />
          <span>导入 .xlsx</span>
          <input type="file" accept=".xlsx" @change="onImportFile" />
        </label>
        <el-button type="primary" @click="openCreateDialog"><Plus :size="14" /><span class="ks-gap">新增实例</span></el-button>
      </template>
    </KsBanner>

    <!-- Tab 1: Instances -->
    <div v-if="activeTab === 'instances'" class="ki-card">
      <div class="ks-filter-row">
        <div class="ki-pill-tabs">
          <button
            type="button"
            class="ki-pill-tab"
            :class="{ 'ki-pill-tab--active': selectedModelId === '' }"
            @click="selectedModelId = ''"
          >
            全部模型
          </button>
          <button
            v-for="m in models"
            :key="m.model_id"
            type="button"
            class="ki-pill-tab"
            :class="{ 'ki-pill-tab--active': selectedModelId === m.model_id }"
            @click="selectedModelId = m.model_id"
          >
            <span>{{ m.name }}</span>
            <span class="ki-pill-tab__count">{{ m.instance_count }}</span>
          </button>
        </div>
        <div class="ks-search-box">
          <el-input v-model="keyword" placeholder="按名称、唯一标识或属性检索..." clearable size="small" @keyup.enter="loadAll" />
          <el-button size="small" @click="downloadXLSX(true)">下载导入模板</el-button>
        </div>
      </div>

      <el-table :data="instances" size="small" stripe>
        <el-table-column prop="name" label="资产名称" min-width="170" />
        <el-table-column prop="model_name" label="所属模型" width="120">
          <template #default="{ row }"><span class="ki-badge ks-pill-blue">{{ row.model_name }}</span></template>
        </el-table-column>
        <el-table-column prop="unique_key" label="唯一标识" width="170">
          <template #default="{ row }"><span class="ki-mono">{{ row.unique_key }}</span></template>
        </el-table-column>
        <el-table-column label="动态属性" min-width="220">
          <template #default="{ row }">
            <span class="ki-mono ki-muted">{{ JSON.stringify(row.attrs) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="ETag 版本" width="90">
          <template #default="{ row }"><span class="ki-mono">v{{ row.version }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="90">
          <template #default="{ row }">
            <el-button size="small" type="danger" plain @click="cascadeDelete(row)">
              <Trash2 :size="12" />
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Tab 2: Dual Topology Canvas -->
    <div v-else-if="activeTab === 'topology'" class="ki-card">
      <div class="ks-topo-bar">
        <div class="ki-pill-tabs">
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': treeKind === 'business' }" @click="treeKind = 'business'">
            业务拓扑树 (Business · 单父无环)
          </button>
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': treeKind === 'resource' }" @click="treeKind = 'resource'">
            资源拓扑树 (Resource · 单父无环)
          </button>
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': treeKind === 'custom' }" @click="treeKind = 'custom'">
            跨树依赖图 (Custom N:N)
          </button>
        </div>

        <div class="ks-edge-adder">
          <el-select v-model="edgeForm.sourceId" size="small" placeholder="父/源实例" style="width: 160px">
            <el-option v-for="i in topoInstances" :key="i.instance_id" :label="i.name" :value="i.instance_id" />
          </el-select>
          <span class="ki-muted">→</span>
          <el-select v-model="edgeForm.targetId" size="small" placeholder="子/目标实例" style="width: 160px">
            <el-option v-for="i in topoInstances" :key="i.instance_id" :label="i.name" :value="i.instance_id" />
          </el-select>
          <el-button size="small" type="primary" @click="addEdge">连线</el-button>
        </div>
      </div>

      <div class="ks-flow-wrap">
        <VueFlow :nodes="flowNodes" :edges="flowEdges" fit-view-on-init />
      </div>
    </div>

    <!-- Tab 3: Models -->
    <div v-else class="ki-card">
      <el-table :data="models" size="small" stripe>
        <el-table-column prop="code" label="模型编码" width="140"><template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template></el-table-column>
        <el-table-column prop="name" label="模型名称" width="140" />
        <el-table-column prop="instance_count" label="实例数" width="90" />
        <el-table-column label="字段 Schema" min-width="320">
          <template #default="{ row }">
            <div class="ks-field-tags">
              <span v-for="f in row.fields || []" :key="f.field_key" class="ki-badge ks-pill-neutral ki-mono">
                {{ f.field_key }}:{{ f.field_type }}{{ f.is_unique ? ' [U]' : '' }}{{ f.required ? '*' : '' }}
              </span>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Create Instance Dialog -->
    <el-dialog v-model="createDlg" title="新增 CMDB 资产实例" width="500px">
      <el-form label-position="top">
        <el-form-item label="选择元模型" required>
          <el-select v-model="instForm.modelId" style="width: 100%">
            <el-option v-for="m in models" :key="m.model_id" :label="`${m.name} (${m.code})`" :value="m.model_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="实例名称" required><el-input v-model="instForm.name" placeholder="例如: his-app-node-03" /></el-form-item>
        <el-form-item label="唯一标识 (unique_key)" required><el-input v-model="instForm.uniqueKey" placeholder="例如: 10.20.1.13" /></el-form-item>
        <template v-for="fld in activeModel?.fields || []" :key="fld.field_key">
          <el-form-item :label="`${fld.name} (${fld.field_key})${fld.is_unique ? ' [唯一]' : ''}`" :required="fld.required">
            <el-select v-if="fld.field_type === 'enum' && fld.enum_values?.length" v-model="instForm.attrs[fld.field_key]" style="width: 100%">
              <el-option v-for="ev in fld.enum_values" :key="ev" :label="ev" :value="ev" />
            </el-select>
            <el-input v-else v-model="instForm.attrs[fld.field_key]" :placeholder="fld.default_value || ''" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="submitCreateInstance">确认入库</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-filter-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--ki-space-3);
  margin-bottom: var(--ki-space-3);
}

.ks-search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 340px;
  max-width: 100%;
}

.ks-topo-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.ks-edge-adder {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ks-flow-wrap {
  height: 440px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-canvas-bg);
}

.ks-field-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-neutral {
  background-color: var(--ki-status-neutral-subtle);
  border: 1px solid var(--ki-status-neutral-border);
  color: var(--ki-status-neutral-text);
}
</style>
