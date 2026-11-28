<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link2, Plus, Trash2, Unlink } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface TopoNode {
  id: string
  treeType: string
  parentId: string
  nodeType: string
  name: string
  code: string
  serviceTemplateId: string
  ciCount: number
  children: TopoNode[]
}

interface CIItem {
  id: string
  modelCode: string
  name: string
  uniqueKey: string
  env: string
  status: string
  owner: string
  host?: { ip: string; agentStatus: string; cpuCores: number; memMb: number }
}

interface ServiceTemplate {
  id: string
  code: string
  name: string
  category: string
  ports: number[]
  processes: string[]
  description: string
}

const loading = ref(false)
const treeType = ref<'business' | 'resource'>('business')
const treeData = ref<TopoNode[]>([])
const selectedNode = ref<TopoNode | null>(null)
const boundCIs = ref<CIItem[]>([])
const allHosts = ref<CIItem[]>([])
const templates = ref<ServiceTemplate[]>([])

const showNodeDialog = ref(false)
const nodeForm = reactive({ parentId: '', nodeType: 'module', name: '', code: '', serviceTemplateId: '' })

const showBindDialog = ref(false)
const bindCIId = ref('')

const showTplDialog = ref(false)
const tplForm = reactive({ code: '', name: '', category: 'application', portsRaw: '8080,9100', procsRaw: 'java', description: '' })

function findFirstLeaf(nodes: TopoNode[]): TopoNode | null {
  for (const n of nodes) {
    if (n.children && n.children.length > 0) {
      const leaf = findFirstLeaf(n.children)
      if (leaf) return leaf
    }
    return n
  }
  return null
}

async function loadTree() {
  loading.value = true
  try {
    const [tRes, hRes, tplRes] = await Promise.all([
      apiRequest<{ items: TopoNode[] }>('GET', `/api/v1/cmdb/topology?treeType=${treeType.value}`),
      apiRequest<{ items: CIItem[] }>('GET', '/api/v1/cmdb/cis?modelCode=m_host'),
      apiRequest<{ items: ServiceTemplate[] }>('GET', '/api/v1/cmdb/service-templates'),
    ])
    treeData.value = tRes.data.items || []
    allHosts.value = hRes.data.items || []
    templates.value = tplRes.data.items || []
    if (!selectedNode.value || selectedNode.value.treeType !== treeType.value) {
      selectedNode.value = findFirstLeaf(treeData.value)
    }
    if (selectedNode.value) {
      await loadBoundCIs(selectedNode.value.id)
    } else {
      boundCIs.value = []
    }
  } finally {
    loading.value = false
  }
}

async function loadBoundCIs(nodeId: string) {
  const { data } = await apiRequest<{ items: CIItem[] }>('GET', `/api/v1/cmdb/topology/${nodeId}/cis`)
  boundCIs.value = data.items || []
}

function onSelectNode(node: TopoNode) {
  selectedNode.value = node
  void loadBoundCIs(node.id)
}

function openCreateChild() {
  nodeForm.parentId = selectedNode.value?.id || ''
  nodeForm.nodeType = treeType.value === 'business' ? 'module' : 'zone'
  nodeForm.name = ''
  nodeForm.code = ''
  nodeForm.serviceTemplateId = ''
  showNodeDialog.value = true
}

async function createNode() {
  try {
    await apiRequest('POST', '/api/v1/cmdb/topology', {
      body: { treeType: treeType.value, ...nodeForm },
    })
    ElMessage.success('拓扑节点已创建')
    showNodeDialog.value = false
    await loadTree()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建节点失败')
  }
}

async function deleteSelectedNode() {
  if (!selectedNode.value) return
  await ElMessageBox.confirm(`确认删除节点「${selectedNode.value.name}」？`, '删除拓扑节点', { type: 'warning' })
  try {
    await apiRequest('DELETE', `/api/v1/cmdb/topology/${selectedNode.value.id}`)
    selectedNode.value = null
    ElMessage.success('已删除节点')
    await loadTree()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '删除失败')
  }
}

async function bindCI() {
  if (!selectedNode.value || !bindCIId.value) return
  await apiRequest('POST', `/api/v1/cmdb/topology/${selectedNode.value.id}/bind`, {
    body: { ciId: bindCIId.value },
  })
  ElMessage.success('资产已绑定到拓扑节点')
  showBindDialog.value = false
  bindCIId.value = ''
  await loadTree()
}

async function unbindCI(ciId: string) {
  if (!selectedNode.value) return
  await apiRequest('DELETE', `/api/v1/cmdb/topology/${selectedNode.value.id}/bind/${ciId}`)
  ElMessage.success('已安全解绑（资产本体保留在 CMDB 中）')
  await loadTree()
}

async function createTemplate() {
  try {
    const ports = tplForm.portsRaw.split(',').map((s) => Number(s.trim())).filter((n) => n > 0)
    const processes = tplForm.procsRaw.split(',').map((s) => s.trim()).filter(Boolean)
    await apiRequest('POST', '/api/v1/cmdb/service-templates', {
      body: {
        code: tplForm.code,
        name: tplForm.name,
        category: tplForm.category,
        ports,
        processes,
        description: tplForm.description,
      },
    })
    ElMessage.success('服务模板已创建')
    showTplDialog.value = false
    await loadTree()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建服务模板失败')
  }
}

onMounted(loadTree)
</script>

<template>
  <div class="topo-page">
    <PageHeader
      title="蓝鲸双拓扑树与服务模板"
      subtitle="业务→集群→模块主线树 + 组织→区域→安全域资源树"
      :refresh="loadTree"
      :refreshing="loading"
    >
      <template #actions>
        <el-radio-group v-model="treeType" @change="selectedNode = null; loadTree()">
          <el-radio-button value="business">业务拓扑树</el-radio-button>
          <el-radio-button value="resource">资源区域树</el-radio-button>
        </el-radio-group>
        <el-button type="primary" @click="openCreateChild"><Plus :size="14" class="btn-ico" />新增拓扑节点</el-button>
      </template>
    </PageHeader>

    <div class="topo-grid">
      <aside class="ki-card topo-tree-card">
        <div class="ki-card__title">
          <span>{{ treeType === 'business' ? '业务主线树' : '组织与安全域树' }}</span>
          <el-button v-if="selectedNode" link type="danger" size="small" @click="deleteSelectedNode">
            <Trash2 :size="13" />
          </el-button>
        </div>
        <el-tree
          :data="treeData"
          node-key="id"
          default-expand-all
          highlight-current
          :props="{ label: 'name', children: 'children' }"
          @node-click="onSelectNode"
        >
          <template #default="{ data }">
            <div class="tree-row">
              <span class="tree-type ki-mono">{{ data.nodeType }}</span>
              <span class="tree-name">{{ data.name }}</span>
              <span v-if="data.ciCount > 0" class="tree-badge ki-num">{{ data.ciCount }}</span>
            </div>
          </template>
        </el-tree>
      </aside>

      <main class="topo-main">
        <section class="ki-card">
          <div class="table-heading">
            <div>
              <strong>{{ selectedNode ? selectedNode.name : '请选择左侧拓扑节点' }}</strong>
              <span v-if="selectedNode" class="ki-mono ki-muted node-code">{{ selectedNode.code }}</span>
            </div>
            <div class="table-actions">
              <el-button v-if="selectedNode" type="primary" size="small" @click="showBindDialog = true">
                <Link2 :size="13" class="btn-ico" />绑定主机资产
              </el-button>
            </div>
          </div>

          <el-table :data="boundCIs" size="small" empty-text="当前拓扑节点尚未绑定主机资产，点击右上角即可多模块安全挂载">
            <el-table-column prop="name" label="主机名称" min-width="140" />
            <el-table-column label="管理 IP" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ row.host?.ip || row.uniqueKey }}</code></template>
            </el-table-column>
            <el-table-column prop="env" label="环境" width="80" />
            <el-table-column label="Agent 状态" width="110">
              <template #default="{ row }">
                <KiStatusBadge
                  :status="row.host?.agentStatus === 'ONLINE' ? 'ok' : 'cancelled'"
                  :label="row.host?.agentStatus || 'SSH'"
                />
              </template>
            </el-table-column>
            <el-table-column prop="owner" label="负责人" width="110" />
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ row }">
                <el-button link type="danger" size="small" @click="unbindCI(row.id)">
                  <Unlink :size="12" class="btn-ico" />解绑
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </section>

        <section class="ki-card">
          <div class="table-heading">
            <div class="section-title">标准化服务模板 ({{ templates.length }})</div>
            <el-button size="small" @click="showTplDialog = true"><Plus :size="13" class="btn-ico" />新建服务模板</el-button>
          </div>
          <el-table :data="templates" size="small">
            <el-table-column prop="name" label="模板名称" min-width="160" />
            <el-table-column prop="code" label="编码" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ row.code }}</code></template>
            </el-table-column>
            <el-table-column label="标准端口" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ (row.ports || []).join(', ') }}</code></template>
            </el-table-column>
            <el-table-column label="标准进程" min-width="180">
              <template #default="{ row }">{{ (row.processes || []).join(' / ') }}</template>
            </el-table-column>
          </el-table>
        </section>
      </main>
    </div>

    <el-dialog v-model="showNodeDialog" title="新增拓扑节点" width="440px">
      <el-form label-position="top">
        <el-form-item label="节点层级类型">
          <el-select v-model="nodeForm.nodeType" style="width: 100%">
            <el-option v-if="treeType === 'business'" label="业务 (biz)" value="biz" />
            <el-option v-if="treeType === 'business'" label="集群 (set)" value="set" />
            <el-option v-if="treeType === 'business'" label="模块 (module)" value="module" />
            <el-option v-if="treeType === 'resource'" label="组织 (org)" value="org" />
            <el-option v-if="treeType === 'resource'" label="区域 (region)" value="region" />
            <el-option v-if="treeType === 'resource'" label="安全域 (zone)" value="zone" />
          </el-select>
        </el-form-item>
        <el-form-item label="节点名称">
          <el-input v-model="nodeForm.name" placeholder="例如：急诊结算模块" />
        </el-form-item>
        <el-form-item label="节点编码">
          <el-input v-model="nodeForm.code" placeholder="mod-billing" />
        </el-form-item>
        <el-form-item v-if="treeType === 'business'" label="关联服务模板 (可选)">
          <el-select v-model="nodeForm.serviceTemplateId" clearable style="width: 100%">
            <el-option v-for="t in templates" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showNodeDialog = false">取消</el-button>
        <el-button type="primary" @click="createNode">确定</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showBindDialog" title="绑定主机资产到当前拓扑节点" width="440px">
      <el-select v-model="bindCIId" filterable placeholder="选择统一 CMDB 主机资产 (m_host)" style="width: 100%">
        <el-option
          v-for="h in allHosts"
          :key="h.id"
          :label="`${h.name} (${h.host?.ip || h.uniqueKey})`"
          :value="h.id"
        />
      </el-select>
      <template #footer>
        <el-button @click="showBindDialog = false">取消</el-button>
        <el-button type="primary" :disabled="!bindCIId" @click="bindCI">确认绑定</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showTplDialog" title="新建服务模板" width="460px">
      <el-form label-position="top">
        <el-form-item label="模板编码"><el-input v-model="tplForm.code" placeholder="tpl-nginx-edge" /></el-form-item>
        <el-form-item label="模板名称"><el-input v-model="tplForm.name" placeholder="边缘接入 Nginx 模板" /></el-form-item>
        <el-form-item label="开放端口 (逗号分隔)"><el-input v-model="tplForm.portsRaw" placeholder="80,443" /></el-form-item>
        <el-form-item label="常驻进程 (逗号分隔)"><el-input v-model="tplForm.procsRaw" placeholder="nginx,node_exporter" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="tplForm.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showTplDialog = false">取消</el-button>
        <el-button type="primary" @click="createTemplate">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.topo-grid {
  display: grid;
  grid-template-columns: 310px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

.topo-tree-card {
  min-height: 460px;
}

.topo-main {
  display: flex;
  flex-direction: column;
  gap: var(--ki-space-4);
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding-right: 6px;
}

.tree-type {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-muted);
}

.tree-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tree-badge {
  font-size: 11px;
  padding: 0 6px;
  border-radius: var(--ki-radius-pill);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.node-code {
  margin-left: 8px;
}

@media (max-width: 960px) {
  .topo-grid {
    grid-template-columns: 1fr;
  }
}
</style>
