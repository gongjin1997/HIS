<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { FolderTree, Layers, Link, Plus, Server, Trash2, Unlink } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'

interface TopoNode {
  id: string
  topoKind: 'business' | 'resource'
  parentId: string
  modelCode: string
  name: string
  serviceTemplateId?: string
  boundHostCount: number
  children?: TopoNode[]
}

interface HostItem {
  id: string
  name: string
  address: string
  arch?: string
  state?: string
  osId?: string
}

interface ServiceTemplate {
  id: string
  name: string
  category: string
  description: string
  processes: Array<{ name: string; binary: string; user: string; bindPort: number; protocol: string }>
  ports: number[]
}

const ws = useWorkspaceStore()
const loading = ref(false)
const topoKind = ref<'business' | 'resource'>('business')
const tree = ref<TopoNode[]>([])
const hosts = ref<HostItem[]>([])
const templates = ref<ServiceTemplate[]>([])
const selectedNode = ref<TopoNode | null>(null)
const boundHostIds = ref<string[]>([])
const rightTab = ref<'hosts' | 'templates'>('hosts')

const nodeDialog = reactive({
  open: false,
  parentId: '',
  parentName: '',
  modelCode: 'm_module',
  name: '',
})

const bindHostId = ref('')
const tplDialog = reactive({
  open: false,
  name: '',
  category: '业务服务',
  description: '',
  procName: 'app-server',
  binary: '/usr/local/bin/app',
  user: 'app',
  port: 8080,
})

const boundHosts = computed(() => hosts.value.filter((h) => boundHostIds.value.includes(h.id)))
const availableHosts = computed(() => hosts.value.filter((h) => !boundHostIds.value.includes(h.id)))
const appliedTemplate = computed(() =>
  templates.value.find((t) => t.id === selectedNode.value?.serviceTemplateId) || null
)

function flattenFirst(nodes: TopoNode[]): TopoNode | null {
  for (const n of nodes) {
    if (n.children && n.children.length > 0) {
      const leaf = flattenFirst(n.children)
      if (leaf) return leaf
    }
    return n
  }
  return null
}

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [tRes, hRes, tplRes] = await Promise.all([
      apiRequest<{ items: TopoNode[] }>('GET', `/api/v1/projects/${pid}/cmdb/topology?kind=${topoKind.value}`),
      apiRequest<{ items: HostItem[] }>('GET', `/api/v1/projects/${pid}/hosts`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: ServiceTemplate[] }>('GET', `/api/v1/projects/${pid}/cmdb/service-templates`),
    ])
    tree.value = tRes.data.items || []
    hosts.value = hRes.data.items || []
    templates.value = tplRes.data.items || []
    if (!selectedNode.value && tree.value.length > 0) {
      await selectNode(flattenFirst(tree.value) || tree.value[0])
    } else if (selectedNode.value) {
      await loadBoundHosts(selectedNode.value.id)
    }
  } finally {
    loading.value = false
  }
}

async function selectNode(node: TopoNode) {
  selectedNode.value = node
  await loadBoundHosts(node.id)
}

async function loadBoundHosts(nodeId: string) {
  if (!ws.currentProjectId || !nodeId) return
  const { data } = await apiRequest<{ hostIds: string[] }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes/${encodeURIComponent(nodeId)}/hosts`
  )
  boundHostIds.value = data.hostIds || []
}

function openAddChild(parent?: TopoNode) {
  nodeDialog.parentId = parent?.id || ''
  nodeDialog.parentName = parent?.name || '根节点'
  nodeDialog.name = ''
  if (topoKind.value === 'business') {
    nodeDialog.modelCode = !parent ? 'm_biz' : parent.modelCode === 'm_biz' ? 'm_set' : 'm_module'
  } else {
    nodeDialog.modelCode = !parent ? 'm_site' : 'm_rack'
  }
  nodeDialog.open = true
}

async function createTopoNode() {
  if (!nodeDialog.name.trim()) {
    ElMessage.warning('请输入拓扑节点名称')
    return
  }
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes`, {
      body: {
        topoKind: topoKind.value,
        parentId: nodeDialog.parentId,
        modelCode: nodeDialog.modelCode,
        name: nodeDialog.name.trim(),
      },
    })
    ElMessage.success('拓扑节点已创建')
    nodeDialog.open = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function removeTopoNode(node: TopoNode) {
  try {
    await ElMessageBox.confirm(`确认删除拓扑节点「${node.name}」及其子节点吗？（仅解除绑定，绝不删除主机资产）`, '删除节点', { type: 'warning' })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes/${encodeURIComponent(node.id)}`)
    if (selectedNode.value?.id === node.id) selectedNode.value = null
    ElMessage.success('拓扑节点已删除')
    await loadAll()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function bindHost() {
  if (!selectedNode.value || !bindHostId.value) return
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes/${encodeURIComponent(selectedNode.value.id)}/hosts`, {
      body: { hostId: bindHostId.value, action: 'bind' },
    })
    ElMessage.success('已安全挂载主机至当前拓扑节点')
    bindHostId.value = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function unbindHost(hostId: string) {
  if (!selectedNode.value) return
  await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes/${encodeURIComponent(selectedNode.value.id)}/hosts`, {
    body: { hostId, action: 'unbind' },
  })
  ElMessage.success('已解除主机与当前模块的关联（主机资产保持完整）')
  await loadAll()
}

async function applyTemplateToSelected(tplId: string) {
  if (!selectedNode.value) return
  await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/topology/nodes/${encodeURIComponent(selectedNode.value.id)}/template`, {
    body: { templateId: tplId },
  })
  selectedNode.value.serviceTemplateId = tplId
  ElMessage.success('已绑定标准化服务模板')
}

async function createTemplate() {
  if (!tplDialog.name.trim()) {
    ElMessage.warning('请输入服务模板名称')
    return
  }
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/cmdb/service-templates`, {
      body: {
        name: tplDialog.name.trim(),
        category: tplDialog.category,
        description: tplDialog.description.trim(),
        processes: [{ name: tplDialog.procName, binary: tplDialog.binary, user: tplDialog.user, bindPort: tplDialog.port, protocol: 'TCP' }],
        ports: [tplDialog.port],
      },
    })
    ElMessage.success('服务模板已创建')
    tplDialog.open = false
    tplDialog.name = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteTemplate(id: string) {
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/cmdb/service-templates/${encodeURIComponent(id)}`)
  ElMessage.success('服务模板已删除')
  await loadAll()
}

watch(topoKind, () => {
  selectedNode.value = null
  void loadAll()
})
watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="业务与资源双拓扑"
      subtitle="对标蓝鲸 bk-cmdb：业务→集群→模块主线拓扑 + 机房→区域→机柜资源树，支持主机多模块安全挂载与标准化服务模板"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="tplDialog.open = true">
          <Layers :size="14" class="btn-ico" /> 新建服务模板
        </el-button>
        <el-button type="primary" @click="openAddChild()">
          <Plus :size="14" class="btn-ico" /> 新建根拓扑节点
        </el-button>
      </template>
    </PageHeader>

    <div class="topo-layout">
      <!-- 左侧双拓扑树 -->
      <div class="ki-card topo-rail">
        <el-radio-group v-model="topoKind" size="small" class="ki-mb-3 ki-w-full">
          <el-radio-button value="business">业务主线树</el-radio-button>
          <el-radio-button value="resource">资源区域树</el-radio-button>
        </el-radio-group>

        <el-tree
          :data="tree"
          node-key="id"
          default-expand-all
          highlight-current
          :expand-on-click-node="false"
          @node-click="selectNode"
        >
          <template #default="{ data }">
            <div class="tree-row">
              <FolderTree :size="13" class="tree-ico" />
              <span class="tree-label">{{ data.name }}</span>
              <span class="ki-mono tree-cnt">{{ data.boundHostCount }}</span>
              <el-button link size="small" type="primary" @click.stop="openAddChild(data)">+</el-button>
              <el-button link size="small" type="danger" @click.stop="removeTopoNode(data)">×</el-button>
            </div>
          </template>
        </el-tree>
      </div>

      <!-- 右侧节点挂载主机 + 服务模板 -->
      <div class="ki-card">
        <el-tabs v-model="rightTab">
          <el-tab-pane label="节点挂载主机与模板" name="hosts">
            <div v-if="selectedNode" class="node-header ki-mb-3">
              <div>
                <div class="ki-flex-row">
                  <b class="node-title">{{ selectedNode.name }}</b>
                  <span class="ki-badge ki-mono node-code">{{ selectedNode.modelCode }}</span>
                </div>
                <p class="ki-muted node-sub">
                  支持同一台主机同时挂载到多个业务模块；解绑仅删除关系表记录，绝不会误删主机资产。
                </p>
              </div>
              <el-button size="small" @click="openAddChild(selectedNode)">
                <Plus :size="13" class="btn-ico" /> 添加下级节点
              </el-button>
            </div>

            <div v-if="selectedNode" class="tpl-bar ki-mb-3">
              <span class="tpl-bar__lbl">绑定服务模板：</span>
              <el-select
                :model-value="selectedNode.serviceTemplateId || ''"
                placeholder="选择标准化服务模板（进程/端口基线）"
                clearable
                size="small"
                class="tpl-select"
                @change="(v: string) => applyTemplateToSelected(v || '')"
              >
                <el-option v-for="t in templates" :key="t.id" :label="`${t.name} (${t.category})`" :value="t.id" />
              </el-select>
              <span v-if="appliedTemplate" class="ki-muted tpl-meta">
                标准进程 {{ appliedTemplate.processes.length }} 个 · 端口 {{ appliedTemplate.ports.join(', ') }}
              </span>
            </div>

            <div v-if="selectedNode" class="bind-bar ki-mb-3">
              <el-select v-model="bindHostId" filterable placeholder="选择 CMDB 纳管主机挂载至此节点…" class="bind-select">
                <el-option v-for="h in availableHosts" :key="h.id" :label="`${h.name} (${h.address})`" :value="h.id" />
              </el-select>
              <el-button type="primary" :disabled="!bindHostId" @click="bindHost">
                <Link :size="14" class="btn-ico" /> 挂载主机
              </el-button>
            </div>

            <el-table :data="boundHosts" size="small">
              <el-table-column prop="name" label="主机名" min-width="140">
                <template #default="{ row }">
                  <div class="ki-flex-row"><Server :size="13" /> <b>{{ row.name }}</b></div>
                </template>
              </el-table-column>
              <el-table-column prop="address" label="IP 地址" width="140">
                <template #default="{ row }"><span class="ki-mono">{{ row.address }}</span></template>
              </el-table-column>
              <el-table-column prop="osId" label="OS / 架构" width="140">
                <template #default="{ row }"><span class="ki-mono">{{ row.osId || 'linux' }} · {{ row.arch || 'amd64' }}</span></template>
              </el-table-column>
              <el-table-column label="操作" width="100" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="unbindHost(row.id)">
                    <Unlink :size="12" class="btn-ico" /> 解绑
                  </el-button>
                </template>
              </el-table-column>
              <template #empty>
                <span class="ki-muted">当前拓扑节点尚未挂载主机；先在「主机资产与采集」接入主机后即可在此一键挂载。</span>
              </template>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`服务模板库 (${templates.length})`" name="templates">
            <el-table :data="templates" size="small">
              <el-table-column prop="name" label="模板名称" min-width="180" />
              <el-table-column prop="category" label="分层分类" width="100" />
              <el-table-column label="定义进程与监听端口" min-width="220">
                <template #default="{ row }">
                  <div v-for="p in row.processes" :key="p.name" class="ki-mono proc-line">
                    {{ p.name }} ({{ p.binary }}) :{{ p.bindPort }}/{{ p.protocol }} [{{ p.user }}]
                  </div>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="说明" min-width="160" />
              <el-table-column label="操作" width="76" align="right">
                <template #default="{ row }">
                  <el-button link type="danger" size="small" @click="deleteTemplate(row.id)">
                    <Trash2 :size="12" />
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </div>

    <el-dialog v-model="nodeDialog.open" :title="`新增拓扑节点（上级：${nodeDialog.parentName}）`" width="440px">
      <el-form label-position="top">
        <el-form-item label="层级模型">
          <el-select v-model="nodeDialog.modelCode" class="ki-w-full">
            <el-option v-if="topoKind === 'business'" label="业务系统 (m_biz)" value="m_biz" />
            <el-option v-if="topoKind === 'business'" label="集群环境 (m_set)" value="m_set" />
            <el-option v-if="topoKind === 'business'" label="服务模块 (m_module)" value="m_module" />
            <el-option v-if="topoKind === 'resource'" label="机房站点 (m_site)" value="m_site" />
            <el-option v-if="topoKind === 'resource'" label="机柜/安全域 (m_rack)" value="m_rack" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点名称">
          <el-input v-model="nodeDialog.name" placeholder="例如：HIS 门诊核心 / 支付网关模块" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nodeDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createTopoNode">确认创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="tplDialog.open" title="新建蓝鲸式服务模板" width="480px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="模板名称"><el-input v-model="tplDialog.name" placeholder="例如：Java 微服务模板" /></el-form-item>
          <el-form-item label="服务分层"><el-input v-model="tplDialog.category" placeholder="接入层 / 应用层 / 数据层" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="进程名"><el-input v-model="tplDialog.procName" /></el-form-item>
          <el-form-item label="默认端口"><el-input-number v-model="tplDialog.port" :min="1" :max="65535" class="ki-w-full" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="二进制路径"><el-input v-model="tplDialog.binary" /></el-form-item>
          <el-form-item label="运行账号"><el-input v-model="tplDialog.user" /></el-form-item>
        </div>
        <el-form-item label="模板说明"><el-input v-model="tplDialog.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tplDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createTemplate">创建模板</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.topo-layout {
  display: grid;
  grid-template-columns: 310px 1fr;
  gap: 16px;
  align-items: start;
}

@media (width <= 1080px) {
  .topo-layout {
    grid-template-columns: 1fr;
  }
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  font-size: var(--ki-fs-xs);
}

.tree-ico {
  color: var(--ki-brand-primary);
  flex-shrink: 0;
}

.tree-label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree-cnt {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.node-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.node-title {
  font-size: var(--ki-fs-md);
}

.node-code {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.node-sub {
  margin: 4px 0 0;
  font-size: var(--ki-fs-xs);
}

.tpl-bar,
.bind-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.tpl-bar {
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  border: 1px solid var(--ki-border-subtle);
}

.tpl-bar__lbl,
.tpl-meta {
  font-size: var(--ki-fs-xs);
}

.tpl-select {
  width: 260px;
}

.bind-select {
  width: 300px;
}

.proc-line {
  font-size: 11px;
  color: var(--ki-text-secondary);
}
</style>
