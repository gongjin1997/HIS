<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { CheckCircle2, Download, Play, Plus, Save, Trash2, Upload } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiEnvBadge from '@/components/ki/KiEnvBadge.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'
import StackCanvas, { type StackNodeInput } from '@/components/StackCanvas.vue'

interface ResolvedDAG {
  installLayers: string[][]
  uninstallLayers: string[][]
  edges: Record<string, string[]>
  externalRefIds?: string[]
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()

const presets: Record<string, { label: string; name: string; yaml: string }> = {
  logging: {
    label: '日志分析栈 (S3 + Kafka + OpenSearch + DataPrepper)',
    name: 'prod-t3-logging-platform',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: prod-t3-logging-platform
spec:
  instances:
    - id: s3
      product: object-storage
      productVersion: "4.54.0"
      deployTarget: host-binary
    - id: kafka
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
    - id: opensearch
      product: opensearch
      productVersion: "3.10.0"
      deployTarget: host-binary
      dependsOn: [s3]
    - id: dataprepper
      product: elk-pipeline
      productVersion: "2.16.0"
      deployTarget: host-binary
      dependsOn: [kafka, opensearch, s3]
`,
  },
  hisCore: {
    label: 'HIS 核心交易栈 (PostgreSQL + Valkey + Kafka)',
    name: 'his-core-app-stack',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: his-core-app-stack
spec:
  instances:
    - id: pg
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: mq
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
      dependsOn: [pg, cache]
`,
  },
  devops: {
    label: '研发交付栈 (PG + Valkey + GitLab + Harbor)',
    name: 'devops-platform-stack',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: devops-platform-stack
spec:
  instances:
    - id: db
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: gitlab
      product: gitlab
      productVersion: "18.10.0"
      deployTarget: host-container
      dependsOn: [db, cache]
    - id: harbor
      product: harbor
      productVersion: "2.15.0"
      deployTarget: host-container
      dependsOn: [db, cache]
`,
  },
}

const loading = ref(false)
const validating = ref(false)
const stacks = ref<Array<Record<string, unknown>>>([])
const editingStackId = ref('')
const editingStackState = ref('draft')
const stackName = ref(presets.logging.name)
const specYaml = ref(presets.logging.yaml)
const activePreset = ref('logging')
const canvasNodes = ref<StackNodeInput[]>([])
const resolvedDag = ref<ResolvedDAG | null>(null)
const validateError = ref('')

const dangerModal = reactive({
  open: false,
  mode: 'uninstall' as 'uninstall' | 'delete',
  stack: null as Record<string, unknown> | null,
  loading: false,
})

async function validateAndRender() {
  if (!ws.currentProjectId || !specYaml.value.trim()) return
  validating.value = true
  validateError.value = ''
  try {
    const { data } = await apiRequest<{ dag: ResolvedDAG; nodes: StackNodeInput[]; valid: boolean }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/stacks:validate`,
      { body: { specYaml: specYaml.value } }
    )
    resolvedDag.value = data.dag
    const nodeStatus = editingStackId.value ? editingStackState.value || 'planned' : 'draft'
    canvasNodes.value = (data.nodes || []).map((n) => ({ ...n, status: nodeStatus }))
  } catch (e) {
    validateError.value = String(e)
    resolvedDag.value = null
  } finally {
    validating.value = false
  }
}

function selectPreset(key: string) {
  const p = presets[key]
  if (!p) return
  activePreset.value = key
  editingStackId.value = ''
  editingStackState.value = 'draft'
  stackName.value = p.name
  specYaml.value = p.yaml
  void validateAndRender()
}

function loadStackIntoEditor(row: Record<string, unknown>) {
  activePreset.value = ''
  editingStackId.value = String(row.id || '')
  editingStackState.value = String(row.state || 'planned')
  stackName.value = String(row.name || '')
  if (typeof row.specYaml === 'string' && row.specYaml.trim()) {
    specYaml.value = row.specYaml
    void validateAndRender()
  }
}

function startNewStack() {
  selectPreset('hisCore')
}

function onImportStackYaml(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const txt = String(reader.result || '')
    specYaml.value = txt
    const nameMatch = txt.match(/^\s*name:\s*([a-zA-Z0-9._-]+)/m)
    if (nameMatch?.[1]) stackName.value = nameMatch[1]
    else stackName.value = file.name.replace(/\.(ya?ml)$/i, '')
    activePreset.value = ''
    editingStackId.value = ''
    editingStackState.value = 'draft'
    ElMessage.success(`已导入应用栈文件 ${file.name}`)
    void validateAndRender()
  }
  reader.readAsText(file)
  input.value = ''
}

function exportStackYaml() {
  const blob = new Blob([specYaml.value], { type: 'application/yaml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${stackName.value || 'kinstall-stack'}.yaml`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
  ElMessage.success(`已导出 ${stackName.value || 'kinstall-stack'}.yaml`)
}

interface PluginProductOption {
  product: string
  defaultVersion: string
  targets: string[]
}

const availableProducts = ref<PluginProductOption[]>([])
const addNodeDialog = reactive({
  open: false,
  id: '',
  product: 'valkey',
  productVersion: '9.1.4',
  deployTarget: 'host-container',
  dependsOn: [] as string[],
})
const editNodeDialog = reactive({
  open: false,
  id: '',
  product: '',
  version: '',
  target: '',
  dependsOn: [] as string[],
})

function openAddNodeDialog() {
  const first = availableProducts.value[0]
  const prod = first?.product || 'valkey'
  addNodeDialog.product = prod
  addNodeDialog.productVersion = first?.defaultVersion || '9.1.4'
  addNodeDialog.deployTarget = first?.targets?.[0] || 'host-container'
  addNodeDialog.id = `${prod.replace(/[^a-z0-9]/gi, '')}-${canvasNodes.value.length + 1}`
  addNodeDialog.dependsOn = []
  addNodeDialog.open = true
}

function onAddProductChange(prodName: string) {
  const found = availableProducts.value.find((p) => p.product === prodName)
  if (found) {
    addNodeDialog.productVersion = found.defaultVersion || '1.0.0'
    addNodeDialog.deployTarget = found.targets?.[0] || 'host-container'
    addNodeDialog.id = `${found.product.replace(/[^a-z0-9]/gi, '')}-${canvasNodes.value.length + 1}`
  }
}

function rebuildYamlFromNodes(nodesList: StackNodeInput[]): string {
  const lines = [
    'apiVersion: kinstall.io/v1alpha1',
    'kind: Stack',
    'metadata:',
    `  name: ${stackName.value.trim() || 'custom-stack'}`,
    'spec:',
    '  instances:',
  ]
  for (const n of nodesList) {
    lines.push(`    - id: ${n.id}`)
    lines.push(`      product: ${n.product}`)
    lines.push(`      productVersion: "${n.version}"`)
    lines.push(`      deployTarget: ${n.target}`)
    if (n.dependsOn && n.dependsOn.length > 0) {
      lines.push(`      dependsOn: [${n.dependsOn.join(', ')}]`)
    }
  }
  return lines.join('\n') + '\n'
}

function submitAddNode() {
  const id = addNodeDialog.id.trim()
  if (!id) {
    ElMessage.warning('请输入节点 ID')
    return
  }
  if (canvasNodes.value.some((n) => n.id === id)) {
    ElMessage.warning(`节点 ID "${id}" 已存在`)
    return
  }
  const nextNodes: StackNodeInput[] = [
    ...canvasNodes.value,
    {
      id,
      product: addNodeDialog.product,
      version: addNodeDialog.productVersion || '1.0.0',
      target: addNodeDialog.deployTarget || 'host-binary',
      dependsOn: [...addNodeDialog.dependsOn],
      status: 'draft',
    },
  ]
  specYaml.value = rebuildYamlFromNodes(nextNodes)
  addNodeDialog.open = false
  ElMessage.success(`已添加节点 ${id} 至画布，正在实时校验依赖拓扑`)
  void validateAndRender()
}

function connectDagNodes(sourceId: string, targetId: string) {
  const nextNodes = canvasNodes.value.map((n) => {
    if (n.id !== targetId) return n
    const deps = new Set(n.dependsOn || [])
    deps.add(sourceId)
    return { ...n, dependsOn: Array.from(deps) }
  })
  specYaml.value = rebuildYamlFromNodes(nextNodes)
  ElMessage.success(`已建立依赖连线：${sourceId} → ${targetId}`)
  void validateAndRender()
}

function openNodeEditor(nodeId: string) {
  const found = canvasNodes.value.find((n) => n.id === nodeId)
  if (!found) return
  editNodeDialog.id = found.id
  editNodeDialog.product = found.product
  editNodeDialog.version = found.version
  editNodeDialog.target = found.target
  editNodeDialog.dependsOn = [...(found.dependsOn || [])]
  editNodeDialog.open = true
}

function saveNodeEditor() {
  const nextNodes = canvasNodes.value.map((n) =>
    n.id === editNodeDialog.id
      ? { ...n, version: editNodeDialog.version, target: editNodeDialog.target, dependsOn: [...editNodeDialog.dependsOn] }
      : n
  )
  specYaml.value = rebuildYamlFromNodes(nextNodes)
  editNodeDialog.open = false
  void validateAndRender()
}

function removeEditingNode() {
  const rmId = editNodeDialog.id
  const nextNodes = canvasNodes.value
    .filter((n) => n.id !== rmId)
    .map((n) => ({ ...n, dependsOn: (n.dependsOn || []).filter((d) => d !== rmId) }))
  specYaml.value = rebuildYamlFromNodes(nextNodes)
  editNodeDialog.open = false
  ElMessage.success(`已从拓扑移除节点 ${rmId}`)
  void validateAndRender()
}

async function loadStacks() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [sRes, pRes] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/stacks`),
      apiRequest<{ items: Array<{ products?: Array<{ product: string; defaultVersion?: string; targets?: string[] }> }> }>('GET', '/api/v1/plugins').catch(
        () => ({ data: { items: [] } })
      ),
    ])
    stacks.value = sRes.data.items || []
    const prods: PluginProductOption[] = []
    for (const pl of pRes.data.items || []) {
      for (const pr of pl.products || []) {
        prods.push({
          product: pr.product,
          defaultVersion: pr.defaultVersion || '1.0.0',
          targets: pr.targets || ['host-binary'],
        })
      }
    }
    availableProducts.value = prods
    await validateAndRender()
  } finally {
    loading.value = false
  }
}

async function saveStack() {
  if (!stackName.value.trim()) {
    ElMessage.warning('请输入应用栈名称')
    return
  }
  try {
    if (editingStackId.value) {
      await apiRequest('PUT', `/api/v1/projects/${ws.currentProjectId}/stacks/${editingStackId.value}`, {
        body: { env: ws.currentEnv, name: stackName.value.trim(), specYaml: specYaml.value },
      })
      ElMessage.success(`应用栈 ${stackName.value} 已更新`)
    } else {
      await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/stacks`, {
        body: { env: ws.currentEnv, name: stackName.value.trim(), specYaml: specYaml.value },
        idempotent: true,
      })
      ElMessage.success('应用栈拓扑与输出参数校验通过，已保存')
    }
    await loadStacks()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function applyStack(row: Record<string, unknown>) {
  try {
    const { data } = await apiRequest<{ id: string; state: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/stacks/${row.id}:apply`,
      { idempotent: true }
    )
    await ws.refreshApprovalBadge()
    if (data.state === 'awaiting_approval') {
      ElMessage.warning(`生产环境应用栈 ${row.name} 已进入变更审批队列`)
      await router.push('/operations/approvals')
    } else {
      ElMessage.success(`应用栈分层部署任务 ${data.id} 已下发`)
      await router.push(`/operations?id=${data.id}`)
    }
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openDangerStack(row: Record<string, unknown>, mode: 'uninstall' | 'delete') {
  dangerModal.mode = mode
  dangerModal.stack = row
  dangerModal.open = true
}

async function confirmDangerStack() {
  if (!dangerModal.stack) return
  dangerModal.loading = true
  try {
    if (dangerModal.mode === 'delete') {
      await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/stacks/${dangerModal.stack.id}`)
      ElMessage.success(`应用栈 ${dangerModal.stack.name} 已删除`)
      if (editingStackId.value === String(dangerModal.stack.id)) {
        editingStackId.value = ''
        editingStackState.value = 'draft'
      }
      dangerModal.open = false
      await loadStacks()
    } else {
      const { data } = await apiRequest<{ id: string; state: string }>(
        'POST',
        `/api/v1/projects/${ws.currentProjectId}/stacks/${dangerModal.stack.id}:uninstall`,
        { idempotent: true }
      )
      dangerModal.open = false
      await ws.refreshApprovalBadge()
      if (data.state === 'awaiting_approval') {
        ElMessage.warning('逆序卸载已进入变更审批队列')
        await router.push('/operations/approvals')
      } else {
        ElMessage.success(`逆序卸载操作 ${data.id} 已入队（外部引用已自动保护）`)
        await router.push(`/operations?id=${data.id}`)
      }
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    dangerModal.loading = false
  }
}

watch(() => ws.currentProjectId, () => {
  editingStackId.value = ''
  editingStackState.value = 'draft'
  void loadStacks()
})
onMounted(loadStacks)
</script>

<template>
  <div>
    <PageHeader
      title="应用栈编排"
      subtitle="实时依赖 DAG 拓扑校验 · 跨层输出参数自动插值 · 逆序卸载自动保护 externalRefs"
      :refresh="loadStacks"
      :refreshing="loading"
    >
      <template #actions>
        <el-button :loading="validating" @click="validateAndRender">
          <CheckCircle2 :size="14" class="btn-ico" /> 校验并渲染拓扑
        </el-button>
        <el-button v-if="editingStackId" @click="startNewStack">
          <Plus :size="14" class="btn-ico" /> 新建草稿
        </el-button>
        <el-button v-if="auth.can('stack:write', ws.currentProjectId)" type="primary" @click="saveStack">
          <Save :size="14" class="btn-ico" /> {{ editingStackId ? '更新当前应用栈' : '校验并保存应用栈' }}
        </el-button>
      </template>
    </PageHeader>

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>
          复合应用栈拓扑画布
          <span class="sub">{{ editingStackId ? `正在编辑：${stackName}` : '模板草稿预览（保存后可分层 Apply）' }}</span>
        </span>
        <div class="ki-flex-row ki-flex-wrap">
          <div v-if="resolvedDag" class="dag-layers">
            <span v-for="(layer, i) in resolvedDag.installLayers" :key="i" class="ki-badge ki-mono layer-pill">
              L{{ i }} · {{ layer.join(', ') }}
            </span>
            <span v-if="resolvedDag.externalRefIds?.length" class="ki-badge ki-mono ext-pill">
              外部引用保护：{{ resolvedDag.externalRefIds.join(', ') }}
            </span>
          </div>
          <el-button size="small" type="primary" plain @click="openAddNodeDialog">
            <Plus :size="13" class="btn-ico" /> 添加组件节点
          </el-button>
        </div>
      </div>

      <KiBanner v-if="validateError" variant="danger" :text="validateError" class="ki-mb-3" />
      <StackCanvas :instances="canvasNodes" @connect-edge="connectDagNodes" @select-node="openNodeEditor" />
      <div class="dag-tip ki-muted">提示：点击画布中的任意组件节点可编辑版本/形态/依赖或移除；从节点右侧输出端口拖线至另一节点左侧可可视化建立依赖。</div>
    </div>

    <div class="ki-grid-2 stack-bottom-grid">
      <div class="ki-card stack-card">
        <div class="ki-card__title">
          <span>声明式 Stack YAML</span>
          <div class="ki-flex-row">
            <label class="ki-upload-btn ki-upload-btn--sm">
              <Upload :size="13" /> 导入 YAML
              <input type="file" accept=".yaml,.yml" @change="onImportStackYaml" />
            </label>
            <el-button size="small" @click="exportStackYaml">
              <Download :size="13" class="btn-ico" /> 导出
            </el-button>
          </div>
        </div>
        <div class="editor-meta-row ki-mb-3">
          <el-input v-model="stackName" placeholder="应用栈名称" class="ki-mono stack-name-input" />
          <el-select
            :model-value="activePreset"
            clearable
            placeholder="套用内置场景模板"
            class="preset-select"
            @change="(k: string) => k && selectPreset(k)"
          >
            <el-option v-for="(p, key) in presets" :key="key" :label="p.label" :value="key" />
          </el-select>
        </div>
        <el-input v-model="specYaml" type="textarea" :rows="13" class="ki-mono" />
      </div>

      <div class="ki-card stack-card">
        <div class="ki-card__title">
          <span>已保存应用栈清单<span class="sub">共 {{ stacks.length }} 套 · 点击行载入编辑</span></span>
        </div>
        <el-table
          v-loading="loading"
          :data="stacks"
          size="small"
          highlight-current-row
          @row-click="loadStackIntoEditor"
        >
          <el-table-column prop="name" label="应用栈名称" min-width="145">
            <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
          </el-table-column>
          <el-table-column label="环境" width="76">
            <template #default="{ row }"><KiEnvBadge :env="String(row.env || ws.currentEnv)" compact /></template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'planned')" /></template>
          </el-table-column>
          <el-table-column label="操作" width="170" fixed="right">
            <template #default="{ row }">
              <div class="ki-row-actions">
                <el-button
                  v-if="auth.can('stack:write', ws.currentProjectId)"
                  link
                  type="primary"
                  @click.stop="applyStack(row)"
                >
                  <Play :size="12" class="btn-ico" /> Apply
                </el-button>
                <el-button
                  v-if="auth.can('stack:write', ws.currentProjectId)"
                  link
                  type="warning"
                  @click.stop="openDangerStack(row, 'uninstall')"
                >
                  卸载
                </el-button>
                <el-button
                  v-if="auth.can('stack:write', ws.currentProjectId)"
                  link
                  type="danger"
                  aria-label="删除应用栈"
                  @click.stop="openDangerStack(row, 'delete')"
                >
                  <Trash2 :size="13" />
                </el-button>
              </div>
            </template>
          </el-table-column>
          <template #empty><el-empty description="暂无已保存应用栈，点击右上角保存当前模板" :image-size="60" /></template>
        </el-table>
      </div>
    </div>

    <!-- 可视化添加节点弹窗 -->
    <el-dialog v-model="addNodeDialog.open" title="向应用栈画布添加组件节点" width="480px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="选择基础设施组件">
            <el-select v-model="addNodeDialog.product" filterable class="ki-w-full" @change="onAddProductChange">
              <el-option v-for="p in availableProducts" :key="p.product" :label="`${p.product} (${p.defaultVersion})`" :value="p.product" />
            </el-select>
          </el-form-item>
          <el-form-item label="节点标识 (Stack 内唯一 ID)">
            <el-input v-model="addNodeDialog.id" class="ki-mono" />
          </el-form-item>
          <el-form-item label="组件版本">
            <el-input v-model="addNodeDialog.productVersion" class="ki-mono" />
          </el-form-item>
          <el-form-item label="交付形态">
            <el-select v-model="addNodeDialog.deployTarget" class="ki-w-full">
              <el-option label="主机二进制 (host-binary)" value="host-binary" />
              <el-option label="单机容器 (host-container)" value="host-container" />
              <el-option label="Kubernetes 集群 (kubernetes)" value="kubernetes" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="上游依赖节点 (dependsOn)">
          <el-select v-model="addNodeDialog.dependsOn" multiple class="ki-w-full" placeholder="可选：选择启动前必须先就绪的上游节点">
            <el-option v-for="n in canvasNodes" :key="n.id" :label="`${n.id} (${n.product})`" :value="n.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="addNodeDialog.open = false">取消</el-button>
        <el-button type="primary" @click="submitAddNode">加入拓扑画布</el-button>
      </template>
    </el-dialog>

    <!-- 节点属性可视化编辑弹窗 -->
    <el-dialog v-model="editNodeDialog.open" :title="`编辑拓扑节点 · ${editNodeDialog.id} (${editNodeDialog.product})`" width="460px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="组件版本"><el-input v-model="editNodeDialog.version" class="ki-mono" /></el-form-item>
          <el-form-item label="交付形态">
            <el-select v-model="editNodeDialog.target" class="ki-w-full">
              <el-option label="主机二进制 (host-binary)" value="host-binary" />
              <el-option label="单机容器 (host-container)" value="host-container" />
              <el-option label="Kubernetes 集群 (kubernetes)" value="kubernetes" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="上游依赖 (dependsOn)">
          <el-select v-model="editNodeDialog.dependsOn" multiple class="ki-w-full">
            <el-option
              v-for="n in canvasNodes.filter((x) => x.id !== editNodeDialog.id)"
              :key="n.id"
              :label="`${n.id} (${n.product})`"
              :value="n.id"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button type="danger" plain @click="removeEditingNode">移除此节点</el-button>
        <el-button @click="editNodeDialog.open = false">取消</el-button>
        <el-button type="primary" @click="saveNodeEditor">保存节点属性</el-button>
      </template>
    </el-dialog>

    <KiDangerConfirmModal
      v-model="dangerModal.open"
      :env="String(dangerModal.stack?.env || ws.currentEnv)"
      :title="dangerModal.mode === 'delete' ? '确认删除应用栈定义' : '确认逆序卸载应用栈'"
      :resource-name="String(dangerModal.stack?.name || '')"
      :risk-summary="
        dangerModal.mode === 'delete'
          ? '将从控制面删除该应用栈定义及其未落地的草稿成员；若存在运行中实例请先执行逆序卸载。'
          : '将严格按依赖逆序分层卸载栈内自建实例；externalRefs 声明的外部托管服务绝不会被修改或删除。'
      "
      :confirm-label="dangerModal.mode === 'delete' ? '解锁并删除应用栈' : '解锁并逆序卸载'"
      :loading="dangerModal.loading"
      @confirm="confirmDangerStack"
    />
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.dag-tip {
  margin-top: 8px;
  font-size: var(--ki-fs-xs);
}

.stack-bottom-grid {
  align-items: stretch;
}

.stack-card {
  display: flex;
  flex-direction: column;
}

.editor-meta-row {
  display: flex;
  gap: var(--ki-space-2);
}

.stack-name-input {
  flex: 1;
}

.preset-select {
  flex: 1;
}

.dag-layers {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-xs);
}

.layer-pill {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.ext-pill {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}
</style>
