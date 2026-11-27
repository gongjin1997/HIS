<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Background, Controls, Handle, Position, VueFlow, type Node, type Edge, type Connection, type NodeDragEvent } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { Boxes, Link2, Plus, Trash2 } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'

interface ModelGroup {
  id: string
  code: string
  name: string
  sortOrder: number
  isBuiltin: boolean
}

interface ModelAttr {
  id: string
  modelCode: string
  attrGroup: string
  propId: string
  propName: string
  fieldType: string
  unit: string
  required: boolean
  readonly: boolean
  isUnique: boolean
  display: boolean
  enumOptions: string[]
  sourceKind: string
  collectorPluginId: string
  sortOrder: number
}

interface CMDBModel {
  id: string
  groupCode: string
  code: string
  name: string
  icon: string
  description: string
  posX: number
  posY: number
  isBuiltin: boolean
  isMainline: boolean
  instanceCount: number
  attributes: ModelAttr[]
}

interface ModelRelation {
  id: string
  sourceModel: string
  targetModel: string
  relationType: string
  label: string
  mappingKind: string
}

const ws = useWorkspaceStore()
const loading = ref(false)
const activeTab = ref<'canvas' | 'schema'>('canvas')
const groups = ref<ModelGroup[]>([])
const models = ref<CMDBModel[]>([])
const relations = ref<ModelRelation[]>([])
const selectedModelCode = ref('m_host')

const modelDialog = reactive({
  open: false,
  groupCode: 't_app',
  code: '',
  name: '',
  description: '',
})

const attrDialog = reactive({
  open: false,
  attrGroup: '基础属性',
  propId: '',
  propName: '',
  fieldType: 'string',
  unit: '',
  required: false,
  readonly: false,
  isUnique: false,
  enumText: '',
  sourceKind: 'manual',
})

const relDialog = reactive({
  open: false,
  sourceModel: '',
  targetModel: '',
  relationType: 'depends_on',
  label: '依赖于',
  mappingKind: '1:N',
})

const selectedModel = computed(() => models.value.find((m) => m.code === selectedModelCode.value) || models.value[0] || null)

const flowNodes = computed<Node[]>(() =>
  models.value.map((m) => ({
    id: m.code,
    type: 'cmdbModel',
    position: { x: m.posX || 80, y: m.posY || 80 },
    data: {
      code: m.code,
      name: m.name,
      count: m.instanceCount,
      attrCount: m.attributes?.length || 0,
      isMainline: m.isMainline,
      isBuiltin: m.isBuiltin,
      selected: m.code === selectedModelCode.value,
    },
  }))
)

const flowEdges = computed<Edge[]>(() =>
  relations.value.map((r) => ({
    id: r.id,
    source: r.sourceModel,
    target: r.targetModel,
    label: `${r.label} (${r.mappingKind})`,
    type: 'smoothstep',
    animated: r.relationType === 'contains',
  }))
)

async function loadMeta() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ groups: ModelGroup[]; models: CMDBModel[]; relations: ModelRelation[] }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/cmdb/meta`
    )
    groups.value = data.groups || []
    models.value = data.models || []
    relations.value = data.relations || []
    if (!models.value.some((m) => m.code === selectedModelCode.value) && models.value.length > 0) {
      selectedModelCode.value = models.value[0].code
    }
  } finally {
    loading.value = false
  }
}

async function onNodeDragStop(ev: NodeDragEvent) {
  if (!ws.currentProjectId || !ev.node) return
  const code = ev.node.id
  const { x, y } = ev.node.position
  const m = models.value.find((it) => it.code === code)
  if (m) {
    m.posX = Math.round(x)
    m.posY = Math.round(y)
  }
  await apiRequest('PUT', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/models/${encodeURIComponent(code)}/position`, {
    body: { posX: Math.round(x), posY: Math.round(y) },
  }).catch(() => undefined)
}

function onConnect(conn: Connection) {
  if (!conn.source || !conn.target || conn.source === conn.target) return
  relDialog.sourceModel = conn.source
  relDialog.targetModel = conn.target
  relDialog.relationType = 'depends_on'
  relDialog.label = '依赖于'
  relDialog.mappingKind = '1:N'
  relDialog.open = true
}

function selectModelNode(code: string) {
  selectedModelCode.value = code
}

async function createModel() {
  if (!modelDialog.code.trim() || !modelDialog.name.trim()) {
    ElMessage.warning('请填写模型唯一编码与中文名称')
    return
  }
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/models`, {
      body: {
        groupCode: modelDialog.groupCode,
        code: modelDialog.code.trim(),
        name: modelDialog.name.trim(),
        description: modelDialog.description.trim(),
        posX: 160 + (models.value.length % 4) * 230,
        posY: 540,
      },
    })
    ElMessage.success('自定义元模型已创建')
    selectedModelCode.value = modelDialog.code.trim()
    modelDialog.open = false
    modelDialog.code = ''
    modelDialog.name = ''
    modelDialog.description = ''
    await loadMeta()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteModel(m: CMDBModel) {
  if (m.isBuiltin) {
    ElMessage.warning('内置核心元模型受保护，不可删除')
    return
  }
  try {
    await ElMessageBox.confirm(`确认删除自定义模型「${m.name} (${m.code})」吗？`, '删除模型', { type: 'warning' })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/models/${encodeURIComponent(m.code)}`)
    ElMessage.success('模型已删除')
    await loadMeta()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function saveAttribute() {
  if (!selectedModel.value) return
  if (!attrDialog.propId.trim() || !attrDialog.propName.trim()) {
    ElMessage.warning('请填写字段标识 (propId) 与显示名称')
    return
  }
  const enumOptions = attrDialog.enumText
    .split(/[,，\n]/)
    .map((s) => s.trim())
    .filter(Boolean)
  try {
    await apiRequest(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/models/${encodeURIComponent(selectedModel.value.code)}/attributes`,
      {
        body: {
          attrGroup: attrDialog.attrGroup || '基础属性',
          propId: attrDialog.propId.trim(),
          propName: attrDialog.propName.trim(),
          fieldType: attrDialog.fieldType,
          unit: attrDialog.unit.trim(),
          required: attrDialog.required,
          readonly: attrDialog.readonly,
          isUnique: attrDialog.isUnique,
          display: true,
          enumOptions,
          sourceKind: attrDialog.sourceKind,
          sortOrder: (selectedModel.value.attributes?.length || 0) + 1,
        },
      }
    )
    ElMessage.success('动态属性字段已保存，CI 表单与 Excel 模板将即时生效')
    attrDialog.open = false
    attrDialog.propId = ''
    attrDialog.propName = ''
    attrDialog.enumText = ''
    await loadMeta()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteAttribute(id: string) {
  try {
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/attributes/${encodeURIComponent(id)}`)
    ElMessage.success('属性字段已移除')
    await loadMeta()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function createRelation() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/relations`, {
      body: {
        sourceModel: relDialog.sourceModel,
        targetModel: relDialog.targetModel,
        relationType: relDialog.relationType,
        label: relDialog.label || relDialog.relationType,
        mappingKind: relDialog.mappingKind,
      },
    })
    ElMessage.success('模型关联关系已建立')
    relDialog.open = false
    await loadMeta()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteRelation(id: string) {
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/meta/relations/${encodeURIComponent(id)}`)
  ElMessage.success('关联关系已删除')
  await loadMeta()
}

watch(() => ws.currentProjectId, loadMeta)
onMounted(loadMeta)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="动态元模型与关系画布"
      subtitle="对标蓝鲸 bk-cmdb / VeOps：自定义 CI 模型分组、强类型 JSONB 属性 Schema、唯一键校验与拖拽式模型关联画布"
      :refresh="loadMeta"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="relDialog.open = true">
          <Link2 :size="14" class="btn-ico" /> 新建关联关系
        </el-button>
        <el-button type="primary" @click="modelDialog.open = true">
          <Plus :size="14" class="btn-ico" /> 新建自定义模型
        </el-button>
      </template>
    </PageHeader>

    <div class="meta-layout">
      <!-- 左侧模型目录树 -->
      <div class="ki-card model-rail">
        <div class="rail-title">模型分组 ({{ models.length }})</div>
        <div v-for="g in groups" :key="g.code" class="grp-block">
          <div class="grp-name">{{ g.name }}</div>
          <div
            v-for="m in models.filter((it) => it.groupCode === g.code)"
            :key="m.code"
            class="mdl-item"
            :class="{ 'mdl-item--active': selectedModelCode === m.code }"
            @click="selectModelNode(m.code)"
          >
            <Boxes :size="14" class="mdl-ico" />
            <span class="mdl-label">{{ m.name }}</span>
            <span class="ki-mono mdl-cnt">{{ m.instanceCount }}</span>
          </div>
        </div>
      </div>

      <!-- 右侧画布 + 属性 Schema 设计器 -->
      <div class="ki-card meta-main">
        <el-tabs v-model="activeTab">
          <el-tab-pane label="可视化模型关系画布" name="canvas">
            <div class="canvas-box">
              <VueFlow
                :nodes="flowNodes"
                :edges="flowEdges"
                fit-view-on-init
                @node-drag-stop="onNodeDragStop"
                @connect="onConnect"
              >
                <Background :gap="20" />
                <Controls />
                <template #node-cmdbModel="{ data }">
                  <div
                    class="mf-node"
                    :class="{ 'mf-node--active': data.selected, 'mf-node--mainline': data.isMainline }"
                    @click="selectModelNode(String(data.code || ''))"
                  >
                    <Handle type="target" :position="Position.Left" />
                    <div class="mf-node__top">
                      <b>{{ data.name }}</b>
                      <span class="ki-mono mf-cnt">{{ data.count }} CI</span>
                    </div>
                    <div class="mf-node__sub ki-mono">{{ data.code }} · {{ data.attrCount }} 字段</div>
                    <Handle type="source" :position="Position.Right" />
                  </div>
                </template>
              </VueFlow>
            </div>
            <div class="canvas-hint ki-muted">
              提示：拖拽模型卡片自动保存画布坐标；从任意模型右侧端口拖线到另一模型左侧可快速建立 1:1 / 1:N / N:N 关联关系。
            </div>
          </el-tab-pane>

          <el-tab-pane :label="`属性字段 Schema (${selectedModel?.name || ''})`" name="schema">
            <div v-if="selectedModel" class="schema-head ki-mb-3">
              <div>
                <b class="schema-title">{{ selectedModel.name }}</b>
                <span class="ki-mono ki-muted schema-code">{{ selectedModel.code }}</span>
                <p class="ki-muted schema-desc">{{ selectedModel.description }}</p>
              </div>
              <div class="ki-flex-row">
                <el-button v-if="!selectedModel.isBuiltin" type="danger" plain size="small" @click="deleteModel(selectedModel)">
                  <Trash2 :size="13" class="btn-ico" /> 删除模型
                </el-button>
                <el-button type="primary" size="small" @click="attrDialog.open = true">
                  <Plus :size="13" class="btn-ico" /> 新增属性字段
                </el-button>
              </div>
            </div>

            <el-table :data="selectedModel?.attributes || []" size="small">
              <el-table-column prop="attrGroup" label="属性分组" width="108" />
              <el-table-column prop="propName" label="显示名称" min-width="120" />
              <el-table-column prop="propId" label="字段标识" width="130">
                <template #default="{ row }"><span class="ki-mono">{{ row.propId }}</span></template>
              </el-table-column>
              <el-table-column prop="fieldType" label="数据类型" width="94">
                <template #default="{ row }"><span class="ki-badge type-pill ki-mono">{{ row.fieldType }}</span></template>
              </el-table-column>
              <el-table-column label="约束" width="146">
                <template #default="{ row }">
                  <span v-if="row.required" class="ki-badge req-pill">必填</span>
                  <span v-if="row.isUnique" class="ki-badge uniq-pill">唯一</span>
                  <span v-if="row.readonly" class="ki-badge ro-pill">只读</span>
                  <span v-if="!row.required && !row.isUnique && !row.readonly" class="ki-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="枚举 / 采集插件" min-width="160">
                <template #default="{ row }">
                  <span v-if="row.enumOptions?.length" class="ki-muted">{{ row.enumOptions.join(' / ') }}</span>
                  <span v-else-if="row.collectorPluginId" class="ki-mono ki-muted">采集: {{ row.collectorPluginId }}</span>
                  <span v-else class="ki-muted">—</span>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="76" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="deleteAttribute(row.id)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`关联关系清单 (${relations.length})`" name="relations">
            <el-table :data="relations" size="small">
              <el-table-column prop="sourceModel" label="源模型" width="140">
                <template #default="{ row }"><b class="ki-mono">{{ row.sourceModel }}</b></template>
              </el-table-column>
              <el-table-column prop="label" label="关系语义" min-width="130" />
              <el-table-column prop="relationType" label="关系类型" width="120">
                <template #default="{ row }"><span class="ki-mono">{{ row.relationType }}</span></template>
              </el-table-column>
              <el-table-column prop="mappingKind" label="基数" width="84" />
              <el-table-column prop="targetModel" label="目标模型" width="140">
                <template #default="{ row }"><b class="ki-mono">{{ row.targetModel }}</b></template>
              </el-table-column>
              <el-table-column label="操作" width="80" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="deleteRelation(row.id)">移除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </div>

    <!-- 新建模型弹窗 -->
    <el-dialog v-model="modelDialog.open" title="新建自定义 CI 元模型" width="480px">
      <el-form label-position="top">
        <el-form-item label="归属模型分组">
          <el-select v-model="modelDialog.groupCode" class="ki-w-full">
            <el-option v-for="g in groups" :key="g.code" :label="g.name" :value="g.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型唯一编码 (英文，如 m_lb / m_k8s_cluster)">
          <el-input v-model="modelDialog.code" placeholder="m_loadbalancer" />
        </el-form-item>
        <el-form-item label="模型显示名称">
          <el-input v-model="modelDialog.name" placeholder="负载均衡实例" />
        </el-form-item>
        <el-form-item label="模型说明">
          <el-input v-model="modelDialog.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="modelDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createModel">创建模型</el-button>
      </template>
    </el-dialog>

    <!-- 新增属性弹窗 -->
    <el-dialog v-model="attrDialog.open" :title="`为「${selectedModel?.name}」新增动态属性`" width="520px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="字段标识 (propId)">
            <el-input v-model="attrDialog.propId" placeholder="vip_addr" />
          </el-form-item>
          <el-form-item label="显示名称">
            <el-input v-model="attrDialog.propName" placeholder="虚拟 IP 地址" />
          </el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="数据类型">
            <el-select v-model="attrDialog.fieldType" class="ki-w-full">
              <el-option label="字符串 (string)" value="string" />
              <el-option label="整数 (int)" value="int" />
              <el-option label="小数 (double)" value="double" />
              <el-option label="布尔 (bool)" value="bool" />
              <el-option label="枚举 (enum)" value="enum" />
              <el-option label="IP 地址 (ip)" value="ip" />
              <el-option label="日期 (date)" value="date" />
            </el-select>
          </el-form-item>
          <el-form-item label="属性分组 / 单位">
            <el-input v-model="attrDialog.attrGroup" placeholder="基础属性" />
          </el-form-item>
        </div>
        <el-form-item v-if="attrDialog.fieldType === 'enum'" label="枚举可选值（逗号分隔）">
          <el-input v-model="attrDialog.enumText" placeholder="active, standby, maintenance" />
        </el-form-item>
        <div class="ki-flex-row">
          <el-checkbox v-model="attrDialog.required">必填校验</el-checkbox>
          <el-checkbox v-model="attrDialog.isUnique">唯一键约束</el-checkbox>
          <el-checkbox v-model="attrDialog.readonly">更新只读保护</el-checkbox>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="attrDialog.open = false">取消</el-button>
        <el-button type="primary" @click="saveAttribute">保存字段</el-button>
      </template>
    </el-dialog>

    <!-- 新建关联弹窗 -->
    <el-dialog v-model="relDialog.open" title="建立模型关联关系" width="480px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="源模型">
            <el-select v-model="relDialog.sourceModel" class="ki-w-full">
              <el-option v-for="m in models" :key="m.code" :label="`${m.name} (${m.code})`" :value="m.code" />
            </el-select>
          </el-form-item>
          <el-form-item label="目标模型">
            <el-select v-model="relDialog.targetModel" class="ki-w-full">
              <el-option v-for="m in models" :key="m.code" :label="`${m.name} (${m.code})`" :value="m.code" />
            </el-select>
          </el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="关系语义">
            <el-select v-model="relDialog.relationType" class="ki-w-full">
              <el-option label="运行于 (runs_on)" value="runs_on" />
              <el-option label="包含 (contains)" value="contains" />
              <el-option label="依赖于 (depends_on)" value="depends_on" />
              <el-option label="路由至 (routes_to)" value="routes_to" />
              <el-option label="上架于 (mounted_in)" value="mounted_in" />
              <el-option label="连接至 (connects_to)" value="connects_to" />
            </el-select>
          </el-form-item>
          <el-form-item label="映射基数">
            <el-select v-model="relDialog.mappingKind" class="ki-w-full">
              <el-option label="1:1" value="1:1" />
              <el-option label="1:N" value="1:N" />
              <el-option label="N:N" value="N:N" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="连线标签">
          <el-input v-model="relDialog.label" placeholder="运行于 / 依赖于" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="relDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createRelation">建立关联</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.meta-layout {
  display: grid;
  grid-template-columns: 248px 1fr;
  gap: 16px;
  align-items: start;
}

@media (width <= 1080px) {
  .meta-layout {
    grid-template-columns: 1fr;
  }
}

.rail-title {
  font-size: var(--ki-fs-sm);
  font-weight: 600;
  margin-bottom: 10px;
  color: var(--ki-text-primary);
}

.grp-block {
  margin-bottom: 12px;
}

.grp-name {
  font-size: 11px;
  font-weight: 600;
  color: var(--ki-text-muted);
  margin-bottom: 4px;
  padding: 0 6px;
}

.mdl-item {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 8px;
  border-radius: var(--ki-radius-sm);
  cursor: pointer;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.mdl-item:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.mdl-item--active {
  background-color: var(--ki-brand-primary-bg);
  color: var(--ki-brand-primary);
  font-weight: 600;
}

.mdl-ico {
  flex-shrink: 0;
}

.mdl-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mdl-cnt {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.canvas-box {
  height: 520px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  overflow: hidden;
}

.mf-node {
  min-width: 176px;
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-control);
  background-color: var(--ki-bg-surface);
  cursor: pointer;
  box-shadow: var(--ki-shadow-sm);
}

.mf-node--mainline {
  border-top: 3px solid var(--ki-brand-primary);
}

.mf-node--active {
  border-color: var(--ki-brand-primary);
  box-shadow: var(--ki-shadow-md);
}

.mf-node__top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-primary);
}

.mf-cnt {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 999px;
  background-color: var(--ki-bg-subtle);
  color: var(--ki-brand-primary);
}

.mf-node__sub {
  font-size: 11px;
  color: var(--ki-text-muted);
  margin-top: 4px;
}

.canvas-hint {
  margin-top: 8px;
  font-size: var(--ki-fs-xs);
}

.schema-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.schema-title {
  font-size: var(--ki-fs-md);
  margin-right: 8px;
}

.schema-code {
  font-size: var(--ki-fs-xs);
}

.schema-desc {
  margin: 4px 0 0;
  font-size: var(--ki-fs-xs);
}

.type-pill,
.req-pill,
.uniq-pill,
.ro-pill {
  margin-right: 4px;
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.req-pill {
  color: var(--ki-status-danger);
}

.uniq-pill {
  color: var(--ki-brand-primary);
}
</style>
