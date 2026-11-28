<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Play, Plus, RotateCcw, Terminal, Trash2 } from 'lucide-vue-next'
import { apiRequest, openSSEStream, type SSELogLine } from '@/api/client'
import { detectDagCycle } from '@/utils/dag'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface PluginItem {
  code: string
  name: string
  category: string
}

interface HostCI {
  id: string
  name: string
  host?: { ip: string; agentStatus: string }
}

interface ReleaseItem {
  id: string
  nodeKey: string
  pluginCode: string
  serviceName: string
  targetHostCiId: string
  batchIndex: number
  dependsOn: string[]
  pauseAfter: boolean
  dialUrl: string
  status: string
  syncedCiId: string
  summary: string
  k8sSpec: { limitCpuM: number; limitMemMi: number; jvmXmx: string }
}

interface Release {
  id: string
  name: string
  env: string
  mode: string
  status: string
  createdBy: string
  createdAt: string
  plan?: { order: string[]; layers: string[][] }
  items: ReleaseItem[]
}

const route = useRoute()
const loading = ref(false)
const plugins = ref<PluginItem[]>([])
const hosts = ref<HostCI[]>([])
const releases = ref<Release[]>([])
const activeReleaseId = ref('')
const sseLines = ref<SSELogLine[]>([])
let closeSSE: (() => void) | null = null

interface DraftNode {
  nodeKey: string
  pluginCode: string
  serviceName: string
  targetHostCiId: string
  batchIndex: number
  dependsOn: string[]
  pauseAfter: boolean
  dialUrl: string
}

const form = reactive({
  name: 'HIS 核心栈灰度发布单',
  env: 'prod',
  mode: 'DAG',
  cpuCores: '2',
  memGb: '4',
  nodes: [
    { nodeKey: 'pg-primary', pluginCode: 'postgresql', serviceName: 'his-pg-prod', targetHostCiId: '', batchIndex: 0, dependsOn: [], pauseAfter: true, dialUrl: '' },
    { nodeKey: 'valkey-cache', pluginCode: 'valkey', serviceName: 'his-valkey-prod', targetHostCiId: '', batchIndex: 0, dependsOn: [], pauseAfter: false, dialUrl: '' },
    { nodeKey: 'his-gateway', pluginCode: 'his-java-app', serviceName: 'his-gateway-prod', targetHostCiId: '', batchIndex: 1, dependsOn: ['pg-primary', 'valkey-cache'], pauseAfter: false, dialUrl: '' },
  ] as DraftNode[],
})

const cycleCheck = computed(() =>
  detectDagCycle(form.nodes.map((n) => ({ id: n.nodeKey, dependsOn: n.dependsOn })))
)

const activeRelease = computed(() => releases.value.find((r) => r.id === activeReleaseId.value) || null)

async function loadAll() {
  loading.value = true
  try {
    const [pRes, hRes, rRes] = await Promise.all([
      apiRequest<{ items: PluginItem[] }>('GET', '/api/v1/delivery/plugins'),
      apiRequest<{ items: HostCI[] }>('GET', '/api/v1/cmdb/cis?modelCode=m_host'),
      apiRequest<{ items: Release[] }>('GET', '/api/v1/delivery/releases'),
    ])
    plugins.value = pRes.data.items || []
    hosts.value = hRes.data.items || []
    releases.value = rRes.data.items || []
    if (!activeReleaseId.value && releases.value.length > 0) {
      activeReleaseId.value = releases.value[0].id
    }
    const qPlg = route.query.plugin as string
    if (qPlg && plugins.value.some((p) => p.code === qPlg)) {
      form.nodes = [
        { nodeKey: `${qPlg}-01`, pluginCode: qPlg, serviceName: `${qPlg}-prod`, targetHostCiId: '', batchIndex: 0, dependsOn: [], pauseAfter: false, dialUrl: '' },
      ]
    }
  } finally {
    loading.value = false
  }
}

function addDraftNode() {
  const idx = form.nodes.length + 1
  form.nodes.push({
    nodeKey: `svc-${idx}`,
    pluginCode: plugins.value[0]?.code || 'nginx',
    serviceName: `service-${idx}`,
    targetHostCiId: '',
    batchIndex: idx - 1,
    dependsOn: [],
    pauseAfter: false,
    dialUrl: '',
  })
}

function removeDraftNode(idx: number) {
  form.nodes.splice(idx, 1)
}

function attachLogs(relId: string) {
  if (closeSSE) closeSSE()
  sseLines.value = []
  activeReleaseId.value = relId
  closeSSE = openSSEStream(`release:${relId}`, (line) => {
    sseLines.value.push(line)
  })
}

async function createAndRun(autoStart: boolean) {
  if (cycleCheck.value.hasCycle) {
    ElMessage.error(`检测到 DAG 循环依赖: ${cycleCheck.value.cyclePath.join(' -> ')}`)
    return
  }
  try {
    const { data } = await apiRequest<Release>('POST', '/api/v1/delivery/releases', {
      body: {
        name: form.name,
        env: form.env,
        mode: form.mode,
        globalParams: { CPU_CORES: form.cpuCores, MEM_GB: form.memGb },
        items: form.nodes,
        autoStart,
      },
    })
    ElMessage.success(autoStart ? `发布单已启动，当前状态: ${data.status}` : '发布编排计划已创建')
    await loadAll()
    attachLogs(data.id)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建发布单失败')
  }
}

async function executeOrResume(rel: Release) {
  attachLogs(rel.id)
  try {
    const { data } = await apiRequest<Release>('POST', `/api/v1/delivery/releases/${rel.id}/execute`)
    ElMessage.success(`执行完成，状态: ${data.status}`)
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '执行失败')
  }
}

async function rollbackRel(rel: Release) {
  attachLogs(rel.id)
  const { data } = await apiRequest<Release>('POST', `/api/v1/delivery/releases/${rel.id}/rollback`)
  ElMessage.warning(`发布单已回滚: ${data.status}`)
  await loadAll()
}

onMounted(loadAll)
onBeforeUnmount(() => {
  if (closeSSE) closeSSE()
})
</script>

<template>
  <div class="deliv-page">
    <PageHeader
      title="DAG 与分批灰度发布流水线"
      subtitle="Kahn 拓扑排程 · 人工灰度暂停断点 · 真实执行与 CMDB 自动登记"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="createAndRun(false)">仅生成编排单</el-button>
        <el-button type="primary" :disabled="cycleCheck.hasCycle" @click="createAndRun(true)">
          <Play :size="14" class="btn-ico" />创建并立即发布
        </el-button>
      </template>
    </PageHeader>

    <section class="ki-card composer-card">
      <div class="ki-card__title">
        <span>编排计划设计器 (DAG / BATCH / M1 / M2 / M3)</span>
        <el-button size="small" @click="addDraftNode"><Plus :size="13" class="btn-ico" />添加组件节点</el-button>
      </div>

      <el-alert
        v-if="cycleCheck.hasCycle"
        :title="`DAG 存在循环依赖，已阻断提交: ${cycleCheck.cyclePath.join(' → ')}`"
        type="error"
        show-icon
        :closable="false"
        class="cycle-alert"
      />

      <div class="meta-row">
        <el-input v-model="form.name" placeholder="发布单名称" style="flex: 2" />
        <el-select v-model="form.env" style="width: 110px">
          <el-option v-for="e in ['prod', 'beta', 'uat', 'sit', 'dev']" :key="e" :label="e.toUpperCase()" :value="e" />
        </el-select>
        <el-select v-model="form.mode" style="width: 120px">
          <el-option label="DAG 拓扑" value="DAG" />
          <el-option label="BATCH 分批" value="BATCH" />
          <el-option label="M1 灰度" value="M1" />
          <el-option label="M2 双活" value="M2" />
          <el-option label="M3 蓝绿" value="M3" />
        </el-select>
        <el-input v-model="form.cpuCores" placeholder="CPU核" style="width: 90px"><template #append>C</template></el-input>
        <el-input v-model="form.memGb" placeholder="内存GB" style="width: 96px"><template #append>G</template></el-input>
      </div>

      <el-table :data="form.nodes" size="small">
        <el-table-column label="节点 Key" width="130">
          <template #default="{ row }"><el-input v-model="row.nodeKey" size="small" /></template>
        </el-table-column>
        <el-table-column label="YAML 插件" width="160">
          <template #default="{ row }">
            <el-select v-model="row.pluginCode" size="small" style="width: 100%">
              <el-option v-for="p in plugins" :key="p.code" :label="p.name" :value="p.code" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="目标 CMDB 主机 (留空预演)" min-width="175">
          <template #default="{ row }">
            <el-select v-model="row.targetHostCiId" clearable placeholder="预演 / 选择主机" size="small" style="width: 100%">
              <el-option v-for="h in hosts" :key="h.id" :label="`${h.name} (${h.host?.ip || ''})`" :value="h.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="上游依赖 (DAG)" min-width="165">
          <template #default="{ row }">
            <el-select v-model="row.dependsOn" multiple collapse-tags size="small" placeholder="无前置" style="width: 100%">
              <el-option
                v-for="other in form.nodes.filter((x) => x.nodeKey !== row.nodeKey)"
                :key="other.nodeKey"
                :label="other.nodeKey"
                :value="other.nodeKey"
              />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="批次" width="76">
          <template #default="{ row }"><el-input-number v-model="row.batchIndex" :min="0" :max="9" size="small" controls-position="right" style="width: 60px" /></template>
        </el-table-column>
        <el-table-column label="灰度暂停" width="82" align="center">
          <template #default="{ row }"><el-checkbox v-model="row.pauseAfter" /></template>
        </el-table-column>
        <el-table-column label="HTTP 拨测 URL (可选)" min-width="160">
          <template #default="{ row }"><el-input v-model="row.dialUrl" placeholder="http://127.0.0.1:8080/healthz" size="small" /></template>
        </el-table-column>
        <el-table-column width="54" align="center">
          <template #default="{ $index }">
            <el-button link type="danger" size="small" @click="removeDraftNode($index)"><Trash2 :size="13" /></el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <div class="rel-grid">
      <section class="ki-card">
        <div class="ki-card__title">
          <span>发布单执行台账 ({{ releases.length }})</span>
        </div>
        <el-table :data="releases" size="small" highlight-current-row @row-click="(r: Release) => attachLogs(r.id)">
          <el-table-column prop="name" label="发布单" min-width="150" show-overflow-tooltip />
          <el-table-column prop="env" label="环境" width="66" />
          <el-table-column prop="mode" label="模式" width="70" />
          <el-table-column label="状态" width="115">
            <template #default="{ row }">
              <KiStatusBadge
                :status="row.status === 'SUCCEEDED' ? 'ok' : row.status === 'FAILED' ? 'failed' : row.status === 'PAUSED' ? 'warn' : 'running'"
                :label="row.status"
              />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="145" align="right">
            <template #default="{ row }">
              <div class="ki-row-actions">
                <el-button
                  v-if="row.status === 'DRAFT' || row.status === 'PAUSED'"
                  link
                  type="primary"
                  size="small"
                  @click.stop="executeOrResume(row)"
                >
                  {{ row.status === 'PAUSED' ? '继续发布' : '执行' }}
                </el-button>
                <el-button
                  v-if="row.status === 'SUCCEEDED' || row.status === 'PAUSED' || row.status === 'FAILED'"
                  link
                  type="warning"
                  size="small"
                  @click.stop="rollbackRel(row)"
                >
                  <RotateCcw :size="12" class="btn-ico" />回滚
                </el-button>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <template v-if="activeRelease">
          <div class="section-title">节点流水线明细 · {{ activeRelease.name }}</div>
          <el-table :data="activeRelease.items" size="small">
            <el-table-column prop="nodeKey" label="节点" width="110" />
            <el-table-column prop="pluginCode" label="插件" width="110" />
            <el-table-column label="K8s / JVM 规格" width="145">
              <template #default="{ row }">
                <code class="ki-mono">{{ row.k8sSpec?.limitCpuM }}m / {{ row.k8sSpec?.limitMemMi }}Mi</code>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="110">
              <template #default="{ row }">
                <KiStatusBadge :status="row.status === 'SUCCEEDED' ? 'ok' : row.status === 'FAILED' ? 'failed' : 'running'" :label="row.status" />
              </template>
            </el-table-column>
            <el-table-column prop="summary" label="执行与 CMDB 同步结果" min-width="180" show-overflow-tooltip />
          </el-table>
        </template>
      </section>

      <section class="ki-card sse-card">
        <div class="ki-card__title">
          <span class="sse-title"><Terminal :size="15" /> 实时发布 SSE 日志终端</span>
          <span class="sub">{{ activeRelease ? activeRelease.id : '点击左侧发布单查看日志' }}</span>
        </div>
        <div class="sse-console ki-mono">
          <div v-if="sseLines.length === 0" class="sse-empty">等待发布流水线事件流输出…</div>
          <div v-for="l in sseLines" :key="l.seq" class="sse-line" :class="`sse-line--${l.level}`">
            <span class="sse-seq">#{{ l.seq }}</span>
            <span>{{ l.message }}</span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.composer-card {
  margin-bottom: var(--ki-space-4);
}

.cycle-alert {
  margin-bottom: 12px;
}

.meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.rel-grid {
  display: grid;
  grid-template-columns: 1.25fr 1fr;
  gap: var(--ki-space-4);
  align-items: start;
}

.sse-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.sse-console {
  height: 360px;
  overflow-y: auto;
  padding: 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-text-terminal);
  font-size: 12px;
  line-height: 1.55;
}

.sse-empty {
  color: var(--ki-text-muted);
}

.sse-line {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  word-break: break-all;
}

.sse-seq {
  color: var(--ki-text-muted);
  flex: none;
}

.sse-line--error {
  color: var(--ki-status-danger);
}

.sse-line--warn {
  color: var(--ki-status-warn);
}

.sse-line--ok {
  color: var(--ki-status-ok);
}

@media (max-width: 1080px) {
  .rel-grid {
    grid-template-columns: 1fr;
  }
}
</style>
