<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { VueFlow, type Edge, type Node } from '@xyflow/vue'
import '@xyflow/vue/dist/style.css'
import { Plus, Save, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'

interface ModelGroup {
  id: string
  code: string
  name: string
  sortOrder: number
}

interface ModelAttr {
  id: string
  modelCode: string
  attrKey: string
  name: string
  fieldType: string
  groupName: string
  required: boolean
  isUnique: boolean
  defaultValue: string
  options: string[]
}

interface CmdbModel {
  id: string
  groupCode: string
  code: string
  name: string
  icon: string
  description: string
  isBuiltin: boolean
  posX: number
  posY: number
  instanceCount: number
  attrs?: ModelAttr[]
}

interface ModelRelation {
  id: string
  sourceModel: string
  targetModel: string
  relationCode: string
  relationName: string
  cardinality: string
}

const loading = ref(false)
const activeTab = ref<'canvas' | 'schema'>('canvas')
const groups = ref<ModelGroup[]>([])
const models = ref<CmdbModel[]>([])
const relations = ref<ModelRelation[]>([])
const selectedCode = ref('m_host')

const flowNodes = ref<Node[]>([])
const flowEdges = ref<Edge[]>([])

const showModelDialog = ref(false)
const modelForm = reactive({ groupCode: 'application', code: '', name: '', description: '' })

const showAttrDialog = ref(false)
const attrForm = reactive({
  attrKey: '',
  name: '',
  fieldType: 'string',
  groupName: '扩展属性',
  required: false,
  isUnique: false,
  defaultValue: '',
  optionsRaw: '',
})

const showRelDialog = ref(false)
const relForm = reactive({
  sourceModel: 'm_app',
  targetModel: 'm_host',
  relationCode: 'depends_on',
  relationName: '依赖',
  cardinality: 'N:M',
})

const selectedModel = computed(() => models.value.find((m) => m.code === selectedCode.value) || null)

function syncFlow() {
  flowNodes.value = models.value.map((m) => ({
    id: m.code,
    position: { x: m.posX || 120, y: m.posY || 120 },
    data: { label: `${m.name} (${m.instanceCount})` },
    class: m.code === selectedCode.value ? 'ki-flow-node ki-flow-node--active' : 'ki-flow-node',
  }))
  flowEdges.value = relations.value.map((r) => ({
    id: r.id,
    source: r.sourceModel,
    target: r.targetModel,
    label: `${r.relationName} (${r.cardinality})`,
    animated: r.relationCode === 'runs_on',
  }))
}

async function loadAll() {
  loading.value = true
  try {
    const [gRes, mRes, rRes] = await Promise.all([
      apiRequest<{ items: ModelGroup[] }>('GET', '/api/v1/cmdb/model-groups'),
      apiRequest<{ items: CmdbModel[] }>('GET', '/api/v1/cmdb/models?includeAttrs=true'),
      apiRequest<{ items: ModelRelation[] }>('GET', '/api/v1/cmdb/model-relations'),
    ])
    groups.value = gRes.data.items || []
    models.value = mRes.data.items || []
    relations.value = rRes.data.items || []
    if (!models.value.some((m) => m.code === selectedCode.value) && models.value.length > 0) {
      selectedCode.value = models.value[0].code
    }
    syncFlow()
  } finally {
    loading.value = false
  }
}

async function saveLayout() {
  const positions = flowNodes.value.map((n) => ({
    code: n.id,
    posX: Math.round(n.position.x),
    posY: Math.round(n.position.y),
  }))
  await apiRequest('PUT', '/api/v1/cmdb/models/positions', { body: { positions } })
  ElMessage.success('元模型画布坐标已保存')
  await loadAll()
}

async function createModel() {
  try {
    await apiRequest('POST', '/api/v1/cmdb/models', { body: modelForm })
    ElMessage.success('自定义元模型已创建')
    showModelDialog.value = false
    selectedCode.value = modelForm.code.toLowerCase()
    Object.assign(modelForm, { groupCode: 'application', code: '', name: '', description: '' })
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建失败')
  }
}

async function deleteModel(m: CmdbModel) {
  await ElMessageBox.confirm(`确认删除自定义模型「${m.name}」？`, '删除模型', { type: 'warning' })
  try {
    await apiRequest('DELETE', `/api/v1/cmdb/models/${m.code}`)
    ElMessage.success('已删除模型')
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '删除失败')
  }
}

async function addAttr() {
  if (!selectedModel.value) return
  try {
    const options = attrForm.optionsRaw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    await apiRequest('POST', `/api/v1/cmdb/models/${selectedModel.value.code}/attrs`, {
      body: {
        attrKey: attrForm.attrKey,
        name: attrForm.name,
        fieldType: attrForm.fieldType,
        groupName: attrForm.groupName,
        required: attrForm.required,
        isUnique: attrForm.isUnique,
        defaultValue: attrForm.defaultValue,
        options,
      },
    })
    ElMessage.success('模型字段已保存')
    showAttrDialog.value = false
    Object.assign(attrForm, { attrKey: '', name: '', fieldType: 'string', groupName: '扩展属性', required: false, isUnique: false, defaultValue: '', optionsRaw: '' })
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存字段失败')
  }
}

async function deleteAttr(attrId: string) {
  await apiRequest('DELETE', `/api/v1/cmdb/attrs/${attrId}`)
  ElMessage.success('字段已移除')
  await loadAll()
}

async function createRelation() {
  try {
    await apiRequest('POST', '/api/v1/cmdb/model-relations', { body: relForm })
    ElMessage.success('模型关联关系已建立')
    showRelDialog.value = false
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '添加关联失败')
  }
}

async function deleteRelation(id: string) {
  await apiRequest('DELETE', `/api/v1/cmdb/model-relations/${id}`)
  ElMessage.success('已移除关联')
  await loadAll()
}

onMounted(loadAll)
</script>

<template>
  <div class="models-page">
    <PageHeader
      title="动态元模型与关系画布"
      subtitle="自定义 CI 模型分组、动态字段校验与可视化关联图谱"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="showRelDialog = true"><Plus :size="14" class="btn-ico" />新增关系</el-button>
        <el-button type="primary" @click="showModelDialog = true"><Plus :size="14" class="btn-ico" />新建 CI 模型</el-button>
      </template>
    </PageHeader>

    <div class="models-layout">
      <aside class="ki-card model-sidebar">
        <div class="ki-card__title">
          <span>模型目录</span>
          <span class="sub">{{ models.length }} 类</span>
        </div>
        <div v-for="g in groups" :key="g.code" class="grp-block">
          <div class="grp-head">{{ g.name }}</div>
          <button
            v-for="m in models.filter((x) => x.groupCode === g.code)"
            :key="m.code"
            type="button"
            class="mdl-item"
            :class="{ 'is-active': selectedCode === m.code }"
            @click="selectedCode = m.code; syncFlow()"
          >
            <span class="mdl-name">{{ m.name }}</span>
            <span class="mdl-cnt ki-num">{{ m.instanceCount }}</span>
          </button>
        </div>
      </aside>

      <main class="ki-card model-main">
        <el-tabs v-model="activeTab">
          <el-tab-pane label="可视化模型关系画布" name="canvas">
            <div class="canvas-bar">
              <span class="ki-muted">拖拽模型卡片调整架构位置，点击右侧保存即可持久化坐标</span>
              <el-button size="small" type="primary" plain @click="saveLayout">
                <Save :size="13" class="btn-ico" />保存画布布局
              </el-button>
            </div>
            <div class="flow-wrap">
              <VueFlow
                v-model:nodes="flowNodes"
                v-model:edges="flowEdges"
                fit-view-on-init
                @node-click="(ev) => { selectedCode = ev.node.id; syncFlow() }"
              />
            </div>
            <div class="section-title">已定义模型关系 ({{ relations.length }})</div>
            <el-table :data="relations" size="small">
              <el-table-column prop="sourceModel" label="源模型" width="140" />
              <el-table-column prop="relationName" label="关系名称" width="130" />
              <el-table-column prop="relationCode" label="关系标识" width="120" />
              <el-table-column prop="targetModel" label="目标模型" width="140" />
              <el-table-column prop="cardinality" label="基数" width="80" />
              <el-table-column label="操作" width="80" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="deleteRelation(row.id)">移除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane v-if="selectedModel" :label="`字段 Schema · ${selectedModel.name}`" name="schema">
            <div class="table-heading">
              <div>
                <strong>{{ selectedModel.name }}</strong>
                <span class="ki-mono ki-muted model-code-tag">{{ selectedModel.code }}</span>
                <p class="ki-muted schema-desc">{{ selectedModel.description }}</p>
              </div>
              <div class="table-actions">
                <el-button v-if="!selectedModel.isBuiltin" type="danger" plain size="small" @click="deleteModel(selectedModel)">
                  <Trash2 :size="13" class="btn-ico" />删除模型
                </el-button>
                <el-button type="primary" size="small" @click="showAttrDialog = true">
                  <Plus :size="13" class="btn-ico" />新增属性字段
                </el-button>
              </div>
            </div>

            <el-table :data="selectedModel.attrs || []" size="small">
              <el-table-column prop="attrKey" label="字段 Key" width="130">
                <template #default="{ row }"><code class="ki-mono">{{ row.attrKey }}</code></template>
              </el-table-column>
              <el-table-column prop="name" label="中文名称" width="130" />
              <el-table-column prop="fieldType" label="类型" width="90" />
              <el-table-column prop="groupName" label="属性分组" width="110" />
              <el-table-column label="约束" width="120">
                <template #default="{ row }">
                  <el-tag v-if="row.required" size="small" type="danger">必填</el-tag>
                  <el-tag v-if="row.isUnique" size="small" type="warning" class="tag-gap">唯一</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="默认值 / 枚举" min-width="180">
                <template #default="{ row }">
                  <span v-if="row.options?.length" class="ki-mono">{{ row.options.join(', ') }}</span>
                  <span v-else class="ki-muted">{{ row.defaultValue || '—' }}</span>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="80" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="deleteAttr(row.id)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </main>
    </div>

    <el-dialog v-model="showModelDialog" title="新建自定义 CI 元模型" width="460px">
      <el-form label-position="top">
        <el-form-item label="所属分组">
          <el-select v-model="modelForm.groupCode" style="width: 100%">
            <el-option v-for="g in groups" :key="g.code" :label="g.name" :value="g.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型编码 (如 m_loadbalancer)">
          <el-input v-model="modelForm.code" placeholder="m_custom_asset" />
        </el-form-item>
        <el-form-item label="模型名称">
          <el-input v-model="modelForm.name" placeholder="负载均衡实例" />
        </el-form-item>
        <el-form-item label="描述说明">
          <el-input v-model="modelForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showModelDialog = false">取消</el-button>
        <el-button type="primary" @click="createModel">确认创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showAttrDialog" title="新增模型动态属性" width="480px">
      <el-form label-position="top">
        <el-form-item label="属性键 (snake_case)">
          <el-input v-model="attrForm.attrKey" placeholder="rack_slot" />
        </el-form-item>
        <el-form-item label="显示名称">
          <el-input v-model="attrForm.name" placeholder="机柜 U 位" />
        </el-form-item>
        <el-form-item label="字段类型">
          <el-select v-model="attrForm.fieldType" style="width: 100%">
            <el-option label="文本 (string)" value="string" />
            <el-option label="整数/数值 (int)" value="int" />
            <el-option label="枚举下拉 (enum)" value="enum" />
            <el-option label="IP 地址 (ip)" value="ip" />
            <el-option label="布尔值 (bool)" value="bool" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="attrForm.fieldType === 'enum'" label="枚举选项 (英文逗号分隔)">
          <el-input v-model="attrForm.optionsRaw" placeholder="P0,P1,P2" />
        </el-form-item>
        <el-form-item label="默认值">
          <el-input v-model="attrForm.defaultValue" />
        </el-form-item>
        <div class="chk-row">
          <el-checkbox v-model="attrForm.required">必填校验</el-checkbox>
          <el-checkbox v-model="attrForm.isUnique">唯一性约束</el-checkbox>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="showAttrDialog = false">取消</el-button>
        <el-button type="primary" @click="addAttr">保存字段</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showRelDialog" title="新建模型关联关系" width="460px">
      <el-form label-position="top">
        <el-form-item label="源模型">
          <el-select v-model="relForm.sourceModel" style="width: 100%">
            <el-option v-for="m in models" :key="m.code" :label="`${m.name} (${m.code})`" :value="m.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标模型">
          <el-select v-model="relForm.targetModel" style="width: 100%">
            <el-option v-for="m in models" :key="m.code" :label="`${m.name} (${m.code})`" :value="m.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="关系标识 / 名称">
          <div class="two-col">
            <el-input v-model="relForm.relationCode" placeholder="runs_on" />
            <el-input v-model="relForm.relationName" placeholder="运行于" />
          </div>
        </el-form-item>
        <el-form-item label="映射基数">
          <el-select v-model="relForm.cardinality" style="width: 100%">
            <el-option label="1:1" value="1:1" />
            <el-option label="N:1" value="N:1" />
            <el-option label="N:M" value="N:M" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showRelDialog = false">取消</el-button>
        <el-button type="primary" @click="createRelation">确认关联</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.models-layout {
  display: grid;
  grid-template-columns: 248px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

.model-sidebar {
  max-height: calc(100vh - 170px);
  overflow-y: auto;
}

.grp-block {
  margin-bottom: 12px;
}

.grp-head {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
  padding: 4px 6px;
}

.mdl-item {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 7px 10px;
  margin-top: 2px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid transparent;
  background-color: transparent;
  color: var(--ki-text-secondary);
  font-family: var(--ki-font-sans);
  font-size: var(--ki-fs-sm);
  cursor: pointer;
}

.mdl-item:hover {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.mdl-item.is-active {
  background-color: var(--ki-brand-subtle);
  border-color: var(--ki-brand-border);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.mdl-cnt {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.canvas-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  font-size: var(--ki-fs-xs);
}

.flow-wrap {
  height: 360px;
  border: 1px solid var(--ki-border-default);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-canvas);
  overflow: hidden;
}

.model-code-tag {
  margin-left: 8px;
}

.schema-desc {
  margin: 4px 0 0;
  font-size: var(--ki-fs-xs);
}

.tag-gap {
  margin-left: 4px;
}

.chk-row {
  display: flex;
  gap: 16px;
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  width: 100%;
}

@media (max-width: 960px) {
  .models-layout {
    grid-template-columns: 1fr;
  }
}
</style>
