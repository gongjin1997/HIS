<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { PauseCircle, Play, Plus, RotateCcw, Sliders } from 'lucide-vue-next'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface ReleaseItem {
  id: string
  batchNo: number
  seqNo: number
  serviceName: string
  pluginName: string
  targetMode: string
  pauseAfter: boolean
  stage: string
  status: string
  probeUrl?: string
  probePort?: number
  lastOutput?: string
}

interface DeliveryRelease {
  id: string
  name: string
  env: string
  scheduleMode: string
  status: string
  currentBatch: number
  createdBy: string
  createdAt: string
  items: ReleaseItem[]
}

const ws = useWorkspaceStore()
const { formatRelTime } = useKiFormat()
const loading = ref(false)
const releases = ref<DeliveryRelease[]>([])
const namespaces = ref<Array<{ id: string; env: string; name: string }>>([])
const activeTab = ref<'releases' | 'preview'>('releases')

const createDialog = reactive({
  open: false,
  name: 'his-prod-batch-release',
  env: 'prod',
  scheduleMode: 'BATCH',
  configNamespaceId: '',
  items: [
    { batchNo: 0, serviceName: 'canary-gateway', pluginName: 'openresty-edge-waf', targetMode: 'host-container', pauseAfter: true, probeUrl: 'http://127.0.0.1:8080/healthz', probePort: 0 },
    { batchNo: 1, serviceName: 'his-core-api', pluginName: 'his-java-service', targetMode: 'host-container', pauseAfter: false, probeUrl: 'http://127.0.0.1:8080/healthz', probePort: 0 },
  ],
})

const mergePreview = reactive({
  globalJson: '{"IMAGE_REGISTRY":"harbor.internal/his","LOG_LEVEL":"info","PORT":8080}',
  serviceJson: '{"PORT":8088,"SPRING_PROFILE":"prod"}',
  serviceName: 'his-core-api',
  paramPrefix: 'HIS_',
  cpuApply: '500m',
  cpuLimit: '2',
  memApply: '1Gi',
  memLimit: '4Gi',
  replicas: 3,
  result: null as null | { trace: Array<{ key: string; value: unknown; source: string }> },
})

const modeLabels: Record<string, string> = {
  BATCH: 'BATCH 分批灰度 (支持人工暂停断点)',
  M1: 'M1 串行仅预装 (DEPLOY → DONE)',
  M2: 'M2 严格串行三阶段 (部署→启动→拨测)',
  M3: 'M3 并发三阶段极速发布',
}

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [rRes, nsRes] = await Promise.all([
      apiRequest<{ items: DeliveryRelease[] }>('GET', `/api/v1/projects/${pid}/delivery/releases`),
      apiRequest<{ items: Array<{ id: string; env: string; name: string }> }>('GET', `/api/v1/projects/${pid}/config/namespaces?env=all`),
    ])
    releases.value = rRes.data.items || []
    namespaces.value = nsRes.data.items || []
  } finally {
    loading.value = false
  }
}

function addItemRow() {
  createDialog.items.push({
    batchNo: createDialog.items.length,
    serviceName: `service-${createDialog.items.length + 1}`,
    pluginName: 'his-java-service',
    targetMode: 'host-container',
    pauseAfter: false,
    probeUrl: 'http://127.0.0.1:8080/healthz',
    probePort: 0,
  })
}

async function createRelease() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/delivery/releases`, {
      body: {
        name: createDialog.name,
        env: createDialog.env,
        scheduleMode: createDialog.scheduleMode,
        configNamespaceId: createDialog.configNamespaceId,
        items: createDialog.items,
      },
    })
    ElMessage.success('交付发布单已创建并冻结三级有效参数')
    createDialog.open = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function advanceRelease(rel: DeliveryRelease, action: 'start' | 'resume' | 'rollback') {
  try {
    const { data } = await apiRequest<DeliveryRelease>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/delivery/releases/${encodeURIComponent(rel.id)}:${action}`
    )
    ElMessage.success(`交付单「${data.name}」已推进至状态：${data.status}`)
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function runParamPreview() {
  try {
    const globalParams = JSON.parse(mergePreview.globalJson || '{}')
    const serviceParams = JSON.parse(mergePreview.serviceJson || '{}')
    const { data } = await apiRequest<{ trace: Array<{ key: string; value: unknown; source: string }> }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/config/params:preview`,
      {
        body: {
          globalParams,
          serviceParams,
          specs: [{
            serviceName: mergePreview.serviceName,
            paramPrefix: mergePreview.paramPrefix,
            cpuApply: mergePreview.cpuApply,
            cpuLimit: mergePreview.cpuLimit,
            memApply: mergePreview.memApply,
            memLimit: mergePreview.memLimit,
            replicas: mergePreview.replicas,
          }],
        },
      }
    )
    mergePreview.result = data
    ElMessage.success('三级参数合并与 K8s 毫核/MiB 规格校验通过')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function statusBadge(st: string) {
  if (st === 'SUCCEEDED' || st === 'DONE') return 'ok'
  if (st === 'PAUSED') return 'warn'
  if (st === 'FAILED') return 'failed'
  if (st === 'RUNNING') return 'running'
  return 'unprobed'
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="企业级分批交付与发布调度中心"
      subtitle="三级参数冻结、BATCH/M1-M3 调度、人工暂停断点与 HTTP/TCP 拨测验收"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="activeTab = 'preview'">
          <Sliders :size="14" class="btn-ico" /> 三级参数与规格计算器
        </el-button>
        <el-button type="primary" @click="createDialog.open = true">
          <Plus :size="14" class="btn-ico" /> 创建分批交付单
        </el-button>
      </template>
    </PageHeader>

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`交付发布单 (${releases.length})`" name="releases">
          <div v-if="releases.length === 0" class="empty-box ki-muted">
            暂无交付单。点击右上角「创建分批交付单」体验 Batch 灰度暂停断点、真实 HTTP/TCP 拨测验收与一键回滚。
          </div>

          <div v-for="rel in releases" :key="rel.id" class="rel-card ki-mb-3">
            <div class="rel-head">
              <div class="ki-flex-row">
                <b>{{ rel.name }}</b>
                <span class="ki-badge ki-mono mode-tag">{{ rel.scheduleMode }}</span>
                <span class="ki-badge ki-mono mode-tag">{{ rel.env.toUpperCase() }}</span>
                <KiStatusBadge :status="statusBadge(rel.status)" :label="rel.status" />
              </div>
              <div class="ki-flex-row">
                <span class="ki-muted rel-time">{{ rel.createdBy }} · {{ formatRelTime(rel.createdAt) }}</span>
                <el-button v-if="rel.status === 'PENDING'" type="primary" size="small" @click="advanceRelease(rel, 'start')">
                  <Play :size="12" class="btn-ico" /> 启动交付
                </el-button>
                <el-button v-if="rel.status === 'PAUSED'" type="warning" size="small" @click="advanceRelease(rel, 'resume')">
                  <PauseCircle :size="12" class="btn-ico" /> 审批通过 · 继续下一批
                </el-button>
                <el-button v-if="['SUCCEEDED','FAILED','PAUSED'].includes(rel.status)" size="small" @click="advanceRelease(rel, 'rollback')">
                  <RotateCcw :size="12" class="btn-ico" /> 回滚
                </el-button>
              </div>
            </div>

            <el-table :data="rel.items" size="small" class="ki-mt-2">
              <el-table-column label="批次" width="76">
                <template #default="{ row }"><span class="ki-mono">Batch {{ row.batchNo }}</span></template>
              </el-table-column>
              <el-table-column prop="serviceName" label="服务名" width="150" />
              <el-table-column prop="pluginName" label="声明式/内置插件" width="150">
                <template #default="{ row }"><span class="ki-mono">{{ row.pluginName || '—' }}</span></template>
              </el-table-column>
              <el-table-column label="当前阶段" width="124">
                <template #default="{ row }"><span class="ki-badge ki-mono mode-tag">{{ row.stage }}</span></template>
              </el-table-column>
              <el-table-column label="状态" width="104">
                <template #default="{ row }"><KiStatusBadge :status="statusBadge(row.status)" :label="row.status" /></template>
              </el-table-column>
              <el-table-column label="人工断点" width="84">
                <template #default="{ row }"><span class="ki-muted">{{ row.pauseAfter ? '批后暂停' : '自动' }}</span></template>
              </el-table-column>
              <el-table-column label="执行与拨测回执" min-width="240">
                <template #default="{ row }">
                  <pre class="out-log ki-mono">{{ row.lastOutput || '等待调度' }}</pre>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-tab-pane>

        <el-tab-pane label="三级参数合并与 K8s 规格预检器" name="preview">
          <div class="ki-grid-2 ki-mb-3">
            <el-form label-position="top">
              <el-form-item label="Layer 1: 全局参数 (Global JSON)">
                <el-input v-model="mergePreview.globalJson" type="textarea" :rows="3" class="ki-mono" />
              </el-form-item>
              <el-form-item label="Layer 2: 服务覆盖参数 (Service JSON)">
                <el-input v-model="mergePreview.serviceJson" type="textarea" :rows="3" class="ki-mono" />
              </el-form-item>
            </el-form>
            <el-form label-position="top">
              <div class="ki-grid-2">
                <el-form-item label="Layer 3: 规格前缀"><el-input v-model="mergePreview.paramPrefix" /></el-form-item>
                <el-form-item label="副本数 (Replicas)"><el-input-number v-model="mergePreview.replicas" :min="1" class="ki-w-full" /></el-form-item>
              </div>
              <div class="ki-grid-2">
                <el-form-item label="CPU Request / Limit">
                  <div class="ki-flex-row"><el-input v-model="mergePreview.cpuApply" /><el-input v-model="mergePreview.cpuLimit" /></div>
                </el-form-item>
                <el-form-item label="Mem Request / Limit">
                  <div class="ki-flex-row"><el-input v-model="mergePreview.memApply" /><el-input v-model="mergePreview.memLimit" /></div>
                </el-form-item>
              </div>
              <el-button type="primary" @click="runParamPreview">执行唯一三级合并预检 (Spec &gt; Service &gt; Global)</el-button>
            </el-form>
          </div>

          <el-table v-if="mergePreview.result" :data="mergePreview.result.trace" size="small">
            <el-table-column prop="key" label="最终生效参数键" width="220">
              <template #default="{ row }"><b class="ki-mono">{{ row.key }}</b></template>
            </el-table-column>
            <el-table-column prop="value" label="合并后生效值" min-width="200">
              <template #default="{ row }"><span class="ki-mono">{{ row.value }}</span></template>
            </el-table-column>
            <el-table-column prop="source" label="胜出层级" width="120">
              <template #default="{ row }"><span class="ki-badge ki-mono mode-tag">{{ row.source }}</span></template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="createDialog.open" title="创建企业级分批交付发布单" width="760px">
      <el-form label-position="top">
        <div class="ki-grid-3">
          <el-form-item label="交付单名称"><el-input v-model="createDialog.name" /></el-form-item>
          <el-form-item label="目标环境">
            <el-select v-model="createDialog.env" class="ki-w-full">
              <el-option v-for="e in ['prod','beta','uat','sit','dev']" :key="e" :label="e.toUpperCase()" :value="e" />
            </el-select>
          </el-form-item>
          <el-form-item label="调度模式">
            <el-select v-model="createDialog.scheduleMode" class="ki-w-full">
              <el-option v-for="(lbl, k) in modeLabels" :key="k" :label="lbl" :value="k" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="绑定配置中心命名空间（冻结全局参数）">
          <el-select v-model="createDialog.configNamespaceId" clearable class="ki-w-full" placeholder="可选：选择配置中心命名空间">
            <el-option v-for="ns in namespaces" :key="ns.id" :label="`${ns.env.toUpperCase()} / ${ns.name}`" :value="ns.id" />
          </el-select>
        </el-form-item>

        <div class="ki-flex-row ki-mb-2" style="justify-content: space-between;">
          <b>服务批次编排与拨测探针</b>
          <el-button size="small" @click="addItemRow">+ 添加批次服务</el-button>
        </div>
        <div v-for="(it, idx) in createDialog.items" :key="idx" class="item-row">
          <el-input-number v-model="it.batchNo" :min="0" :max="20" size="small" style="width: 82px" />
          <el-input v-model="it.serviceName" size="small" placeholder="服务名" style="width: 140px" />
          <el-input v-model="it.pluginName" size="small" placeholder="插件名" style="width: 140px" />
          <el-input v-model="it.probeUrl" size="small" placeholder="HTTP 拨测 URL" style="flex: 1" />
          <el-checkbox v-model="it.pauseAfter">批后暂停</el-checkbox>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="createDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createRelease">预检并创建交付单</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.empty-box {
  padding: 28px;
  text-align: center;
  font-size: var(--ki-fs-xs);
}

.rel-card {
  padding: 14px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
}

.rel-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.rel-time {
  font-size: var(--ki-fs-xs);
}

.mode-tag {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.out-log {
  margin: 0;
  font-size: 11px;
  white-space: pre-wrap;
  line-height: 1.4;
  color: var(--ki-text-secondary);
}

.item-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
</style>
