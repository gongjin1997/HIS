<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Handle, Position, VueFlow, type Edge, type Node } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { Download, GitFork, Link2, Plus, RefreshCw, Save, Trash2, Upload } from 'lucide-vue-next'
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
  group_id: string
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

interface TreeNodeItem {
  id: string
  label: string
  modelName: string
  children: TreeNodeItem[]
}

const scope = useScopeStore()
const activeTab = ref('directory')
const loading = ref(false)
const models = ref<AssetModel[]>([])
const instances = ref<AssetInstance[]>([])
const allEdges = ref<AssetEdge[]>([])

// Left Tree Filter (Directory Tab)
const dirTreeKind = ref<'business' | 'resource'>('business')
const selectedTreeNodeId = ref<string>('')
const selectedModelId = ref<string>('')
const keyword = ref('')

// Topology Tab
const topoKind = ref<'business' | 'resource' | 'custom'>('business')
const edgeDlg = ref(false)
const edgeForm = ref({ sourceId: '', targetId: '', relTypeId: 'rel-contains', treeKind: 'business' as 'business' | 'resource' | 'custom' })

// Detail & Edit Drawer
const detailDrawer = ref(false)
const detailInst = ref<AssetInstance | null>(null)
const editForm = ref({ name: '', status: 'ACTIVE', attrs: {} as Record<string, string> })
const quickLinkTarget = ref('')
const quickLinkKind = ref<'business' | 'resource' | 'custom'>('custom')

// Create Instance Modal
const createDlg = ref(false)
const instForm = ref({ modelId: '', name: '', uniqueKey: '', status: 'ACTIVE', attrs: {} as Record<string, string> })

// Model Studio Tab
const selectedStudioModel = ref<AssetModel | null>(null)
const modelDlg = ref(false)
const newModelForm = ref({
  groupId: 'grp-res',
  code: '',
  name: '',
  description: '',
  fields: [{ field_key: 'owner', name: '负责人', field_type: 'string', required: false, is_unique: false }] as ModelField[],
})

const instanceById = computed(() => {
  const map = new Map<string, AssetInstance>()
  for (const i of instances.value) map.set(i.instance_id, i)
  return map
})

const activeCreateModel = computed(() => models.value.find((m) => m.model_id === instForm.value.modelId))
const detailModel = computed(() => models.value.find((m) => m.model_id === detailInst.value?.model_id))

// Build hierarchical business / resource tree for left directory panel
const dirTreeData = computed<TreeNodeItem[]>(() => {
  const edges = allEdges.value.filter((e) => e.tree_kind === dirTreeKind.value)
  const childrenMap = new Map<string, string[]>()
  const hasParent = new Set<string>()
  const involved = new Set<string>()
  for (const e of edges) {
    involved.add(e.source_id)
    involved.add(e.target_id)
    hasParent.add(e.target_id)
    const arr = childrenMap.get(e.source_id) || []
    arr.push(e.target_id)
    childrenMap.set(e.source_id, arr)
  }

  const build = (id: string, visited = new Set<string>()): TreeNodeItem | null => {
    if (visited.has(id)) return null
    const inst = instanceById.value.get(id)
    if (!inst) return null
    visited.add(id)
    const kids: TreeNodeItem[] = []
    for (const cid of childrenMap.get(id) || []) {
      const childNode = build(cid, new Set(visited))
      if (childNode) kids.push(childNode)
    }
    return { id: inst.instance_id, label: inst.name, modelName: inst.model_name, children: kids }
  }

  const roots: TreeNodeItem[] = []
  for (const id of involved) {
    if (!hasParent.has(id)) {
      const node = build(id)
      if (node) roots.push(node)
    }
  }
  return roots
})

// Collect subtree IDs when user clicks a tree node
const subtreeIds = computed<Set<string> | null>(() => {
  if (!selectedTreeNodeId.value) return null
  const edges = allEdges.value.filter((e) => e.tree_kind === dirTreeKind.value)
  const childrenMap = new Map<string, string[]>()
  for (const e of edges) {
    const arr = childrenMap.get(e.source_id) || []
    arr.push(e.target_id)
    childrenMap.set(e.source_id, arr)
  }
  const set = new Set<string>([selectedTreeNodeId.value])
  const q = [selectedTreeNodeId.value]
  while (q.length > 0) {
    const u = q.shift()!
    for (const v of childrenMap.get(u) || []) {
      if (!set.has(v)) {
        set.add(v)
        q.push(v)
      }
    }
  }
  return set
})

const filteredInstances = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return instances.value.filter((i) => {
    if (subtreeIds.value && !subtreeIds.value.has(i.instance_id)) return false
    if (selectedModelId.value && i.model_id !== selectedModelId.value) return false
    if (!q) return true
    return (
      i.name.toLowerCase().includes(q) ||
      i.unique_key.toLowerCase().includes(q) ||
      JSON.stringify(i.attrs || {}).toLowerCase().includes(q)
    )
  })
})

const detailRelations = computed(() => {
  if (!detailInst.value) return []
  const id = detailInst.value.instance_id
  return allEdges.value
    .filter((e) => e.source_id === id || e.target_id === id)
    .map((e) => {
      const isOut = e.source_id === id
      const peer = instanceById.value.get(isOut ? e.target_id : e.source_id)
      return {
        edge_id: e.edge_id,
        direction: isOut ? '下级 / 依赖' : '上级 / 被依赖',
        treeKind: e.tree_kind === 'business' ? '业务树' : e.tree_kind === 'resource' ? '资源树' : '跨树关联',
        peerName: peer?.name || (isOut ? e.target_id : e.source_id),
        peerModel: peer?.model_name || '',
      }
    })
})

// VueFlow custom KubeSphere asset cards for Topology tab
const topoEdges = computed(() => allEdges.value.filter((e) => e.tree_kind === topoKind.value))

const flowNodes = computed<Node[]>(() => {
  const edges = topoEdges.value
  const involved = new Set<string>()
  for (const e of edges) {
    involved.add(e.source_id)
    involved.add(e.target_id)
  }
  const nodesToShow = instances.value.filter((i) => involved.size === 0 || involved.has(i.instance_id))
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
      type: 'asset',
      data: {
        name: inst.name,
        modelName: inst.model_name,
        uniqueKey: inst.unique_key,
        status: inst.status,
      },
      position: { x: 48 + col * 290, y: 40 + row * 120 },
    }
  })
})

const flowEdges = computed<Edge[]>(() =>
  topoEdges.value.map((e) => ({
    id: e.edge_id,
    source: e.source_id,
    target: e.target_id,
    animated: topoKind.value === 'custom',
  }))
)

async function loadAll() {
  loading.value = true
  try {
    const [mRes, topoRes] = await Promise.all([
      api.get<{ items: AssetModel[] }>('/api/v1/assets/models'),
      api.get<{ instances: AssetInstance[]; edges: AssetEdge[] }>('/api/v1/assets/topology'),
    ])
    models.value = mRes.data.items || []
    instances.value = topoRes.data.instances || []
    allEdges.value = topoRes.data.edges || []
    if (!selectedStudioModel.value && models.value.length > 0) {
      selectedStudioModel.value = models.value[0]
    }
    if (detailInst.value) {
      const fresh = instances.value.find((i) => i.instance_id === detailInst.value?.instance_id)
      if (fresh) openDetail(fresh)
    }
  } finally {
    loading.value = false
  }
}

function openDetail(inst: AssetInstance) {
  detailInst.value = inst
  const attrs: Record<string, string> = {}
  for (const [k, v] of Object.entries(inst.attrs || {})) {
    attrs[k] = v === null || v === undefined ? '' : String(v)
  }
  editForm.value = { name: inst.name, status: inst.status, attrs }
  detailDrawer.value = true
}

async function saveDetail() {
  if (!detailInst.value) return
  try {
    await api.put(
      `/api/v1/assets/instances/${detailInst.value.instance_id}`,
      { name: editForm.value.name, status: editForm.value.status, attrs: editForm.value.attrs },
      { ifMatch: detailInst.value.version }
    )
    ElMessage.success('资产属性已保存')
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function addQuickRelation() {
  if (!detailInst.value || !quickLinkTarget.value) return
  try {
    await api.post('/api/v1/assets/edges', {
      treeKind: quickLinkKind.value,
      relTypeId: quickLinkKind.value === 'custom' ? 'rel-runs-on' : 'rel-contains',
      sourceId: detailInst.value.instance_id,
      targetId: quickLinkTarget.value,
    })
    ElMessage.success('关联关系已建立')
    quickLinkTarget.value = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function removeEdge(edgeId: string) {
  try {
    await api.del(`/api/v1/assets/edges/${edgeId}`)
    ElMessage.success('关联关系已解除')
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

function openCreateDialog() {
  const mid = selectedModelId.value || models.value[0]?.model_id || ''
  instForm.value = { modelId: mid, name: '', uniqueKey: '', status: 'ACTIVE', attrs: {} }
  createDlg.value = true
}

async function submitCreateInstance() {
  try {
    await api.post('/api/v1/assets/instances', instForm.value)
    ElMessage.success('资产实例已创建')
    createDlg.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function cascadeDelete(inst: AssetInstance) {
  try {
    await ElMessageBox.confirm(`确认清理资产「${inst.name}」及其下属全部子树节点？`, '删除确认', { type: 'warning' })
    const { data } = await api.del<{ deleted_items: number }>(`/api/v1/assets/instances/${inst.instance_id}`)
    ElMessage.success(`已清理 ${data.deleted_items} 项资产`)
    detailDrawer.value = false
    selectedTreeNodeId.value = ''
    await loadAll()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function submitEdgeDialog() {
  try {
    await api.post('/api/v1/assets/edges', {
      treeKind: edgeForm.value.treeKind,
      relTypeId: edgeForm.value.relTypeId,
      sourceId: edgeForm.value.sourceId,
      targetId: edgeForm.value.targetId,
    })
    ElMessage.success('拓扑关系已创建')
    edgeDlg.value = false
    edgeForm.value = { sourceId: '', targetId: '', relTypeId: 'rel-contains', treeKind: topoKind.value }
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function submitCreateModel() {
  try {
    await api.post('/api/v1/assets/models', newModelForm.value)
    ElMessage.success('自定义模型已创建')
    modelDlg.value = false
    newModelForm.value = {
      groupId: 'grp-res',
      code: '',
      name: '',
      description: '',
      fields: [{ field_key: 'owner', name: '负责人', field_type: 'string', required: false, is_unique: false }],
    }
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
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
    ElMessage.error('导出失败')
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
    ElMessage.success(`批量导入完成：新增 ${data.imported} 条，跳过 ${data.skipped} 条`)
    await loadAll()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    input.value = ''
  }
}

onMounted(loadAll)
watch(() => [scope.tenantId, scope.appId], loadAll)
watch(dirTreeKind, () => {
  selectedTreeNodeId.value = ''
})
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="配置管理数据库 (CMDB)"
      description="按业务架构与物理资源双视角组织资产台账，支持树形目录联动、自定义元模型、拓扑关系追踪及 Excel 批量导入导出。"
      :icon="GitFork"
      :badge="`${instances.length} 项资产`"
      :tabs="[
        { key: 'directory', label: '资产目录与树', count: instances.length },
        { key: 'topology', label: '拓扑可视化', count: topoEdges.length },
        { key: 'models', label: '模型与字段', count: models.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="loadAll"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button @click="downloadXLSX(false)"><Download :size="14" /><span class="ks-gap">导出 Excel</span></el-button>
        <label class="ki-upload-btn">
          <Upload :size="14" />
          <span>导入 Excel</span>
          <input type="file" accept=".xlsx" @change="onImportFile" />
        </label>
        <el-button type="primary" @click="openCreateDialog"><Plus :size="14" /><span class="ks-gap">新建资产</span></el-button>
      </template>
    </KsBanner>

    <!-- Tab 1: Authentic Master-Detail Tree + Asset Table -->
    <div v-if="activeTab === 'directory'" class="ks-cmdb-split">
      <aside class="ki-card ks-tree-pane">
        <div class="ks-tree-switch">
          <button type="button" class="ks-tree-tab" :class="{ 'is-active': dirTreeKind === 'business' }" @click="dirTreeKind = 'business'">
            业务拓扑树
          </button>
          <button type="button" class="ks-tree-tab" :class="{ 'is-active': dirTreeKind === 'resource' }" @click="dirTreeKind = 'resource'">
            资源拓扑树
          </button>
        </div>

        <button
          type="button"
          class="ks-tree-all"
          :class="{ 'is-active': selectedTreeNodeId === '' }"
          @click="selectedTreeNodeId = ''"
        >
          <span>全部资产节点</span>
          <span class="ks-tree-cnt">{{ instances.length }}</span>
        </button>

        <el-tree
          :data="dirTreeData"
          node-key="id"
          default-expand-all
          highlight-current
          :expand-on-click-node="false"
          @node-click="(n: TreeNodeItem) => (selectedTreeNodeId = n.id)"
        >
          <template #default="{ data }">
            <div class="ks-tree-node">
              <span class="ks-tree-node__name">{{ data.label }}</span>
              <span class="ks-tree-node__tag">{{ data.modelName }}</span>
            </div>
          </template>
        </el-tree>
      </aside>

      <section class="ki-card ks-table-pane">
        <div class="ks-filter-top">
          <div class="ki-pill-tabs">
            <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': selectedModelId === '' }" @click="selectedModelId = ''">
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

          <div class="ks-search-row">
            <el-input v-model="keyword" placeholder="搜索资产名称、IP 或属性..." clearable size="small" style="width: 240px" />
            <el-button size="small" @click="downloadXLSX(true)">下载模板</el-button>
          </div>
        </div>

        <el-table :data="filteredInstances" size="small">
          <el-table-column label="资产名称" min-width="180">
            <template #default="{ row }">
              <button type="button" class="ks-link-btn" @click="openDetail(row)">{{ row.name }}</button>
              <div class="ks-sub ki-mono">{{ row.unique_key }}</div>
            </template>
          </el-table-column>
          <el-table-column label="模型分类" width="115">
            <template #default="{ row }"><span class="ki-badge ks-pill-blue">{{ row.model_name }}</span></template>
          </el-table-column>
          <el-table-column label="关键属性" min-width="230">
            <template #default="{ row }">
              <div class="ks-attr-chips">
                <span v-for="(v, k) in row.attrs || {}" :key="k" class="ks-attr-chip">
                  <span class="ks-attr-chip__k">{{ k }}:</span> {{ v }}
                </span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <span class="ki-badge ks-pill-ok">{{ row.status === 'ACTIVE' ? '运行中' : row.status }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="130">
            <template #default="{ row }">
              <div class="ki-row-actions">
                <el-button size="small" @click="openDetail(row)">详情</el-button>
                <el-button size="small" type="danger" plain @click="cascadeDelete(row)"><Trash2 :size="12" /></el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </div>

    <!-- Tab 2: Custom Styled KubeSphere Asset Topology -->
    <div v-else-if="activeTab === 'topology'" class="ki-card">
      <div class="ks-topo-head">
        <div class="ki-pill-tabs">
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': topoKind === 'business' }" @click="topoKind = 'business'">
            业务拓扑视图
          </button>
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': topoKind === 'resource' }" @click="topoKind = 'resource'">
            物理资源拓扑
          </button>
          <button type="button" class="ki-pill-tab" :class="{ 'ki-pill-tab--active': topoKind === 'custom' }" @click="topoKind = 'custom'">
            服务部署依赖关系
          </button>
        </div>

        <el-button type="primary" size="small" @click="edgeForm.treeKind = topoKind; edgeDlg = true">
          <Link2 :size="13" /><span class="ks-gap">新建拓扑连线</span>
        </el-button>
      </div>

      <div class="ks-flow-wrap">
        <VueFlow :nodes="flowNodes" :edges="flowEdges" fit-view-on-init>
          <template #node-asset="{ data }">
            <div class="ks-vf-node">
              <Handle type="target" :position="Position.Left" />
              <div class="ks-vf-node__top">
                <span class="ks-vf-node__model">{{ data.modelName }}</span>
                <span class="ks-vf-node__dot" />
              </div>
              <div class="ks-vf-node__name">{{ data.name }}</div>
              <div class="ks-vf-node__key ki-mono">{{ data.uniqueKey }}</div>
              <Handle type="source" :position="Position.Right" />
            </div>
          </template>
        </VueFlow>
      </div>
    </div>

    <!-- Tab 3: Meta-Model Studio -->
    <div v-else class="ks-model-split">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>资产元模型 ({{ models.length }})</span>
          <el-button size="small" type="primary" @click="modelDlg = true"><Plus :size="12" /><span class="ks-gap-xs">新建模型</span></el-button>
        </div>
        <div class="ks-model-grid">
          <button
            v-for="m in models"
            :key="m.model_id"
            type="button"
            class="ks-model-card"
            :class="{ 'is-active': selectedStudioModel?.model_id === m.model_id }"
            @click="selectedStudioModel = m"
          >
            <div class="ks-model-card__top">
              <span class="ks-model-card__name">{{ m.name }}</span>
              <span class="ki-badge ks-pill-neutral">{{ m.instance_count }}</span>
            </div>
            <div class="ks-model-card__code ki-mono">{{ m.code }}</div>
          </button>
        </div>
      </div>

      <div v-if="selectedStudioModel" class="ki-card">
        <div class="ki-card__title">
          <span>{{ selectedStudioModel.name }} — 字段定义</span>
          <span class="ki-mono ki-muted">{{ selectedStudioModel.code }}</span>
        </div>
        <p class="ki-muted ks-mb">{{ selectedStudioModel.description || '标准 CMDB 元模型定义' }}</p>
        <el-table :data="selectedStudioModel.fields || []" size="small">
          <el-table-column prop="name" label="字段名称" width="140" />
          <el-table-column prop="field_key" label="字段标识" width="140"><template #default="{ row }"><span class="ki-mono">{{ row.field_key }}</span></template></el-table-column>
          <el-table-column prop="field_type" label="数据类型" width="100" />
          <el-table-column label="约束条件" min-width="150">
            <template #default="{ row }">
              <span v-if="row.required" class="ki-badge ks-pill-warn ks-mr">必填</span>
              <span v-if="row.is_unique" class="ki-badge ks-pill-blue">全局唯一</span>
              <span v-if="!row.required && !row.is_unique" class="ki-muted">可选</span>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <!-- Asset Detail & Inline Edit Drawer -->
    <el-drawer v-model="detailDrawer" :title="detailInst?.name || '资产详情'" size="560px">
      <div v-if="detailInst" class="ks-drawer-stack">
        <div class="section-title">基础属性编辑</div>
        <el-form label-position="top" size="small">
          <div class="ks-two-form">
            <el-form-item label="资产名称"><el-input v-model="editForm.name" /></el-form-item>
            <el-form-item label="运行状态">
              <el-select v-model="editForm.status" style="width: 100%">
                <el-option label="运行中" value="ACTIVE" />
                <el-option label="维护中" value="MAINTENANCE" />
                <el-option label="已下线" value="RETIRED" />
              </el-select>
            </el-form-item>
          </div>
          <div class="ks-two-form">
            <el-form-item v-for="fld in detailModel?.fields || []" :key="fld.field_key" :label="fld.name">
              <el-select v-if="fld.field_type === 'enum' && fld.enum_values?.length" v-model="editForm.attrs[fld.field_key]" style="width: 100%">
                <el-option v-for="ev in fld.enum_values" :key="ev" :label="ev" :value="ev" />
              </el-select>
              <el-input v-else v-model="editForm.attrs[fld.field_key]" />
            </el-form-item>
          </div>
          <el-button type="primary" @click="saveDetail"><Save :size="13" /><span class="ks-gap">保存属性修改</span></el-button>
        </el-form>

        <div class="section-title">拓扑关联关系 ({{ detailRelations.length }})</div>
        <el-table :data="detailRelations" size="small" empty-text="暂无拓扑关联关系">
          <el-table-column prop="treeKind" label="视角" width="90" />
          <el-table-column prop="direction" label="方向" width="110" />
          <el-table-column prop="peerName" label="关联资产" min-width="150" />
          <el-table-column label="操作" width="76">
            <template #default="{ row }">
              <el-button size="small" type="danger" plain @click="removeEdge(row.edge_id)">移除</el-button>
            </template>
          </el-table-column>
        </el-table>

        <div class="ks-quick-link">
          <el-select v-model="quickLinkKind" size="small" style="width: 110px">
            <el-option label="跨树关联" value="custom" />
            <el-option label="业务树下级" value="business" />
            <el-option label="资源树下级" value="resource" />
          </el-select>
          <el-select v-model="quickLinkTarget" size="small" placeholder="选择目标资产" style="flex: 1">
            <el-option
              v-for="cand in instances.filter((x) => x.instance_id !== detailInst?.instance_id)"
              :key="cand.instance_id"
              :label="`${cand.name} (${cand.model_name})`"
              :value="cand.instance_id"
            />
          </el-select>
          <el-button size="small" type="primary" @click="addQuickRelation">建立关联</el-button>
        </div>

        <div class="ks-danger-zone">
          <el-button type="danger" plain size="small" @click="cascadeDelete(detailInst)">
            <Trash2 :size="13" /><span class="ks-gap">级联删除该资产及下属节点</span>
          </el-button>
        </div>
      </div>
    </el-drawer>

    <!-- Create Instance Modal -->
    <el-dialog v-model="createDlg" title="新建 CMDB 资产实例" width="500px">
      <el-form label-position="top">
        <el-form-item label="资产模型" required>
          <el-select v-model="instForm.modelId" style="width: 100%">
            <el-option v-for="m in models" :key="m.model_id" :label="`${m.name} (${m.code})`" :value="m.model_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="资产名称" required><el-input v-model="instForm.name" placeholder="例如: his-app-node-03" /></el-form-item>
        <el-form-item label="唯一标识" required><el-input v-model="instForm.uniqueKey" placeholder="例如: 10.20.1.13" /></el-form-item>
        <template v-for="fld in activeCreateModel?.fields || []" :key="fld.field_key">
          <el-form-item :label="`${fld.name}${fld.is_unique ? '（唯一）' : ''}`" :required="fld.required">
            <el-select v-if="fld.field_type === 'enum' && fld.enum_values?.length" v-model="instForm.attrs[fld.field_key]" style="width: 100%">
              <el-option v-for="ev in fld.enum_values" :key="ev" :label="ev" :value="ev" />
            </el-select>
            <el-input v-else v-model="instForm.attrs[fld.field_key]" :placeholder="fld.default_value || ''" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="submitCreateInstance">确认创建</el-button>
      </template>
    </el-dialog>

    <!-- Add Topology Edge Modal -->
    <el-dialog v-model="edgeDlg" title="新建拓扑关联关系" width="460px">
      <el-form label-position="top">
        <el-form-item label="关系视角">
          <el-select v-model="edgeForm.treeKind" style="width: 100%">
            <el-option label="业务拓扑树（严格单父无环）" value="business" />
            <el-option label="物理资源拓扑（严格单父无环）" value="resource" />
            <el-option label="服务部署依赖（支持多对多）" value="custom" />
          </el-select>
        </el-form-item>
        <el-form-item label="上级 / 源资产" required>
          <el-select v-model="edgeForm.sourceId" style="width: 100%">
            <el-option v-for="i in instances" :key="i.instance_id" :label="`${i.name} (${i.model_name})`" :value="i.instance_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="下级 / 目标资产" required>
          <el-select v-model="edgeForm.targetId" style="width: 100%">
            <el-option v-for="i in instances" :key="i.instance_id" :label="`${i.name} (${i.model_name})`" :value="i.instance_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="edgeDlg = false">取消</el-button>
        <el-button type="primary" @click="submitEdgeDialog">确认连线</el-button>
      </template>
    </el-dialog>

    <!-- Create Custom Model Modal -->
    <el-dialog v-model="modelDlg" title="新建自定义 CMDB 模型" width="540px">
      <el-form label-position="top">
        <div class="ks-two-form">
          <el-form-item label="模型编码" required><el-input v-model="newModelForm.code" placeholder="例如: load_balancer" /></el-form-item>
          <el-form-item label="模型名称" required><el-input v-model="newModelForm.name" placeholder="例如: 负载均衡器" /></el-form-item>
        </div>
        <el-form-item label="说明"><el-input v-model="newModelForm.description" /></el-form-item>
        <div class="ks-sub-head">
          <span>模型字段定义</span>
          <el-button size="small" @click="newModelForm.fields.push({ field_key: '', name: '', field_type: 'string', required: false, is_unique: false })">添加字段</el-button>
        </div>
        <div v-for="(f, idx) in newModelForm.fields" :key="idx" class="ks-field-edit-row">
          <el-input v-model="f.field_key" size="small" placeholder="字段键" />
          <el-input v-model="f.name" size="small" placeholder="显示名称" />
          <el-select v-model="f.field_type" size="small" style="width: 100px">
            <el-option label="文本" value="string" />
            <el-option label="数字" value="number" />
          </el-select>
          <el-checkbox v-model="f.required">必填</el-checkbox>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="modelDlg = false">取消</el-button>
        <el-button type="primary" @click="submitCreateModel">创建模型</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-gap-xs {
  margin-left: 4px;
}

.ks-mr {
  margin-right: 4px;
}

.ks-mb {
  margin: 0 0 12px;
}

.ks-cmdb-split {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

@media (max-width: 1080px) {
  .ks-cmdb-split {
    grid-template-columns: 1fr;
  }
}

.ks-tree-pane {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ks-tree-switch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px;
  padding: 3px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-canvas);
}

.ks-tree-tab {
  height: 28px;
  border: none;
  border-radius: var(--ki-radius-xs);
  background: transparent;
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ks-tree-tab.is-active {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  font-weight: var(--ki-fw-semibold);
  box-shadow: var(--ki-shadow-surface);
}

.ks-tree-all {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-radius: var(--ki-radius-md);
  border: 1px solid transparent;
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-medium);
  cursor: pointer;
}

.ks-tree-all.is-active {
  background-color: var(--ki-sider-active-bg);
  border-color: var(--ki-brand-border);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.ks-tree-cnt {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  width: 100%;
  padding-right: 4px;
}

.ks-tree-node__name {
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
}

.ks-tree-node__tag {
  font-size: 11px;
  color: var(--ki-text-muted);
  flex: none;
}

.ks-table-pane {
  min-width: 0;
}

.ks-filter-top {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: var(--ki-space-3);
}

.ks-search-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ks-link-btn {
  border: none;
  background: transparent;
  padding: 0;
  color: var(--ki-text-link);
  font-size: var(--ki-fs-body);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
}

.ks-link-btn:hover {
  text-decoration: underline;
}

.ks-sub {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.ks-attr-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.ks-attr-chip {
  padding: 2px 7px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
  font-size: 11.5px;
  color: var(--ki-text-primary);
}

.ks-attr-chip__k {
  color: var(--ki-text-muted);
}

.ks-topo-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.ks-flow-wrap {
  height: 480px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-canvas-bg);
}

.ks-vf-node {
  min-width: 210px;
  padding: 12px 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-surface);
  border: 1px solid var(--ki-border-strong);
  box-shadow: var(--ki-shadow-surface);
}

.ks-vf-node__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.ks-vf-node__model {
  font-size: 11px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-brand);
}

.ks-vf-node__dot {
  width: 7px;
  height: 7px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-ks-green);
}

.ks-vf-node__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.ks-vf-node__key {
  font-size: 11px;
  color: var(--ki-text-muted);
  margin-top: 2px;
}

.ks-model-split {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-model-split {
    grid-template-columns: 1fr;
  }
}

.ks-model-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.ks-model-card {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
}

.ks-model-card.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-sider-active-bg);
}

.ks-model-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.ks-model-card__name {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-model-card__code {
  margin-top: 4px;
  font-size: 11px;
  color: var(--ki-text-muted);
}

.ks-two-form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.ks-quick-link {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.ks-danger-zone {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-sub-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
}

.ks-field-edit-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
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

.ks-pill-warn {
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
}

.ks-pill-neutral {
  background-color: var(--ki-status-neutral-subtle);
  border: 1px solid var(--ki-status-neutral-border);
  color: var(--ki-status-neutral-text);
}
</style>
