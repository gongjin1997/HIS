<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Copy, Eye, History, Lock, Plus, RotateCcw, Send, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'

interface Namespace {
  id: string
  env: string
  appCode: string
  name: string
  format: string
  description: string
  activeVersion: number
  itemCount: number
}

interface ConfigItem {
  id: string
  key: string
  value: string
  isSecret: boolean
  valueType: string
  description: string
  updatedBy: string
  updatedAt: string
}

interface DiffEntry {
  key: string
  change: string
  isSecret: boolean
  oldValue: string
  newValue: string
}

interface ReleaseRow {
  id: string
  version: number
  etag: string
  comment: string
  itemCount: number
  releasedBy: string
  createdAt: string
}

const envs = ['prod', 'beta', 'uat', 'sit', 'dev']
const activeEnv = ref('prod')
const activeTab = ref<'items' | 'releases' | 'calc'>('items')
const loading = ref(false)

const namespaces = ref<Namespace[]>([])
const selectedNSId = ref('')
const items = ref<ConfigItem[]>([])
const diffs = ref<DiffEntry[]>([])
const releases = ref<ReleaseRow[]>([])
const revealedMap = reactive<Record<string, string>>({})

const showNSDialog = ref(false)
const nsForm = reactive({ env: 'prod', appCode: 'his-core', name: 'application', description: '' })

const showItemDialog = ref(false)
const itemForm = reactive({ key: '', value: '', isSecret: false, description: '' })

const showCloneDialog = ref(false)
const cloneForm = reactive({ targetEnv: 'uat', targetApp: 'his-core', targetName: 'application' })

const publishComment = ref('')

// 3-Level Merge & K8s Spec Calculator
const calcForm = reactive({
  globalRaw: 'LOG_LEVEL=info\nDB_POOL=50\nJVM_GC=G1GC',
  serviceRaw: 'DB_POOL=120\nPORT=8080',
  specRaw: 'PORT=18080\nCANARY=true',
  cpuCores: 2,
  memGb: 4,
  replicas: 2,
})
const calcResult = ref<{
  trace: Array<{ key: string; value: string; source: string }>
  k8sSpec: { requestCpuM: number; limitCpuM: number; requestMemMi: number; limitMemMi: number; jvmXms: string; jvmXmx: string; totalCpuCores: number; totalMemGb: number }
} | null>(null)

const selectedNS = computed(() => namespaces.value.find((n) => n.id === selectedNSId.value) || null)

async function loadNamespaces() {
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Namespace[] }>('GET', `/api/v1/config/namespaces?env=${activeEnv.value}`)
    namespaces.value = data.items || []
    if (!namespaces.value.some((n) => n.id === selectedNSId.value)) {
      selectedNSId.value = namespaces.value[0]?.id || ''
    }
    if (selectedNSId.value) {
      await loadNSDetail(selectedNSId.value)
    } else {
      items.value = []
      diffs.value = []
      releases.value = []
    }
  } finally {
    loading.value = false
  }
}

async function loadNSDetail(nsId: string) {
  selectedNSId.value = nsId
  Object.keys(revealedMap).forEach((k) => delete revealedMap[k])
  const [itRes, dfRes, rlRes] = await Promise.all([
    apiRequest<{ items: ConfigItem[] }>('GET', `/api/v1/config/namespaces/${nsId}/items`),
    apiRequest<{ items: DiffEntry[] }>('GET', `/api/v1/config/namespaces/${nsId}/diff`),
    apiRequest<{ items: ReleaseRow[] }>('GET', `/api/v1/config/namespaces/${nsId}/releases`),
  ])
  items.value = itRes.data.items || []
  diffs.value = dfRes.data.items || []
  releases.value = rlRes.data.items || []
}

async function createNamespace() {
  try {
    nsForm.env = activeEnv.value
    const { data } = await apiRequest<Namespace>('POST', '/api/v1/config/namespaces', { body: nsForm })
    ElMessage.success('配置命名空间已创建')
    showNSDialog.value = false
    selectedNSId.value = data.id
    await loadNamespaces()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建失败')
  }
}

async function saveItem() {
  if (!selectedNSId.value) return
  try {
    await apiRequest('POST', `/api/v1/config/namespaces/${selectedNSId.value}/items`, { body: itemForm })
    ElMessage.success(itemForm.isSecret ? 'KMS 密钥已加密落库' : '配置项已保存')
    showItemDialog.value = false
    Object.assign(itemForm, { key: '', value: '', isSecret: false, description: '' })
    await loadNamespaces()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  }
}

async function deleteItem(key: string) {
  if (!selectedNSId.value) return
  await apiRequest('DELETE', `/api/v1/config/namespaces/${selectedNSId.value}/items/${encodeURIComponent(key)}`)
  ElMessage.success('配置项已删除')
  await loadNamespaces()
}

async function revealSecret(key: string) {
  if (!selectedNSId.value) return
  const { value: reason } = await ElMessageBox.prompt('显密操作将被强制写入 SHA-256 防篡改审计链，请填写事由：', `审计显密 · ${key}`, {
    inputValue: '生产配置核对',
    confirmButtonText: '确认解密查看',
    cancelButtonText: '取消',
  })
  const { data } = await apiRequest<{ key: string; value: string }>('POST', `/api/v1/config/namespaces/${selectedNSId.value}/reveal`, {
    body: { key, reason },
  })
  revealedMap[key] = data.value
  ElMessage.warning('已解密显示明文并记录审计哈希链')
}

async function publishRelease() {
  if (!selectedNSId.value) return
  const { data } = await apiRequest<ReleaseRow>('POST', `/api/v1/config/namespaces/${selectedNSId.value}/publish`, {
    body: { comment: publishComment.value || '控制台常规发布' },
  })
  ElMessage.success(`已发布不可变快照 v${data.version} (ETag: ${data.etag})`)
  publishComment.value = ''
  await loadNamespaces()
}

async function rollbackVersion(ver: number) {
  if (!selectedNSId.value) return
  await ElMessageBox.confirm(`确认将配置回滚至历史版本 v${ver}？（将生成新单调递增版本号）`, '版本回滚', { type: 'warning' })
  const { data } = await apiRequest<ReleaseRow>('POST', `/api/v1/config/namespaces/${selectedNSId.value}/rollback`, {
    body: { targetVersion: ver },
  })
  ElMessage.success(`回滚成功，已生成新基线版本 v${data.version}`)
  await loadNamespaces()
}

async function cloneNamespace() {
  if (!selectedNSId.value) return
  try {
    const { data } = await apiRequest<{ blankedSecrets: number }>('POST', `/api/v1/config/namespaces/${selectedNSId.value}/clone`, {
      body: cloneForm,
    })
    ElMessage.success(`已安全克隆至 ${cloneForm.targetEnv}，自动置空 ${data.blankedSecrets} 个密钥项防泄露`)
    showCloneDialog.value = false
    activeEnv.value = cloneForm.targetEnv
    await loadNamespaces()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '克隆失败')
  }
}

function parseKV(raw: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of raw.split('\n')) {
    const idx = line.indexOf('=')
    if (idx > 0) out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
  }
  return out
}

async function runPreviewCalc() {
  const { data } = await apiRequest<typeof calcResult.value>('POST', '/api/v1/config/preview-params', {
    body: {
      globalParams: parseKV(calcForm.globalRaw),
      serviceParams: parseKV(calcForm.serviceRaw),
      specParams: parseKV(calcForm.specRaw),
      cpuCores: calcForm.cpuCores,
      memGb: calcForm.memGb,
      replicas: calcForm.replicas,
    },
  })
  calcResult.value = data
}

onMounted(async () => {
  await loadNamespaces()
  await runPreviewCalc()
})
</script>

<template>
  <div class="cfg-page">
    <PageHeader
      title="多环境配置与 KMS 密钥中心"
      subtitle="五环境隔离 · AES-256/SM4 信封加密 · 审计显密与快照回滚"
      :refresh="loadNamespaces"
      :refreshing="loading"
    >
      <template #actions>
        <el-button v-if="selectedNS" @click="showCloneDialog = true"><Copy :size="14" class="btn-ico" />安全跨环境克隆</el-button>
        <el-button type="primary" @click="showNSDialog = true"><Plus :size="14" class="btn-ico" />新建配置集</el-button>
      </template>
    </PageHeader>

    <div class="ki-pill-tabs env-bar">
      <button
        v-for="e in envs"
        :key="e"
        type="button"
        class="ki-pill-tab"
        :class="{ 'ki-pill-tab--active': activeEnv === e }"
        @click="activeEnv = e; loadNamespaces()"
      >
        {{ e.toUpperCase() }}
      </button>
    </div>

    <div class="cfg-layout">
      <aside class="ki-card">
        <div class="ki-card__title">
          <span>{{ activeEnv.toUpperCase() }} 命名空间</span>
          <span class="sub">{{ namespaces.length }}</span>
        </div>
        <div v-if="namespaces.length === 0" class="ki-muted empty-ns">当前环境暂无配置集</div>
        <button
          v-for="n in namespaces"
          :key="n.id"
          type="button"
          class="ns-btn"
          :class="{ 'is-active': n.id === selectedNSId }"
          @click="loadNSDetail(n.id)"
        >
          <div class="ns-top">
            <b>{{ n.appCode }} / {{ n.name }}</b>
            <span class="ki-mono ns-ver">v{{ n.activeVersion }}</span>
          </div>
          <span class="ki-muted ns-sub">{{ n.itemCount }} 项 · {{ n.description || 'KV 配置' }}</span>
        </button>
      </aside>

      <main class="ki-card">
        <el-tabs v-model="activeTab">
          <el-tab-pane label="KV 与 KMS 密钥项" name="items">
            <div class="table-heading">
              <div>
                <strong>{{ selectedNS ? `${selectedNS.env.toUpperCase()} / ${selectedNS.appCode} / ${selectedNS.name}` : '未选择命名空间' }}</strong>
                <el-tag v-if="selectedNS" size="small" class="tag-gap">基线 v{{ selectedNS.activeVersion }}</el-tag>
              </div>
              <el-button v-if="selectedNS" type="primary" size="small" @click="showItemDialog = true">
                <Plus :size="13" class="btn-ico" />新增/更新配置项
              </el-button>
            </div>

            <el-table :data="items" size="small">
              <el-table-column prop="key" label="配置键 (Key)" width="175">
                <template #default="{ row }">
                  <code class="ki-mono">{{ row.key }}</code>
                </template>
              </el-table-column>
              <el-table-column label="配置值 / KMS 密文" min-width="210">
                <template #default="{ row }">
                  <template v-if="row.isSecret">
                    <el-tag size="small" type="warning"><Lock :size="11" class="btn-ico" />KMS</el-tag>
                    <code class="ki-mono sec-val">{{ revealedMap[row.key] !== undefined ? revealedMap[row.key] : row.value }}</code>
                  </template>
                  <code v-else class="ki-mono">{{ row.value }}</code>
                </template>
              </el-table-column>
              <el-table-column prop="description" label="说明" min-width="150" show-overflow-tooltip />
              <el-table-column label="操作" width="130" align="right">
                <template #default="{ row }">
                  <div class="ki-row-actions">
                    <el-button v-if="row.isSecret" link type="primary" size="small" @click="revealSecret(row.key)">
                      <Eye :size="12" class="btn-ico" />显密
                    </el-button>
                    <el-button link type="danger" size="small" @click="deleteItem(row.key)"><Trash2 :size="12" /></el-button>
                  </div>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`草稿 Diff (${diffs.length}) 与版本快照`" name="releases">
            <div class="pub-bar">
              <el-input v-model="publishComment" placeholder="填写本次快照发布说明…" style="flex: 1" />
              <el-button type="primary" :disabled="!selectedNS" @click="publishRelease">
                <Send :size="13" class="btn-ico" />发布不可变快照
              </el-button>
            </div>

            <div class="section-title">待发布草稿差异 ({{ diffs.length }})</div>
            <el-table v-if="diffs.length > 0" :data="diffs" size="small">
              <el-table-column prop="key" label="配置键" width="160" />
              <el-table-column prop="change" label="变更类型" width="100" />
              <el-table-column prop="oldValue" label="基线值" min-width="130" />
              <el-table-column prop="newValue" label="草稿值" min-width="130" />
            </el-table>
            <div v-else class="ki-muted empty-ns">当前草稿与最新已发布基线完全一致。</div>

            <div class="section-title"><History :size="14" /> 不可变历史快照 ({{ releases.length }})</div>
            <el-table :data="releases" size="small">
              <el-table-column label="版本" width="80">
                <template #default="{ row }"><strong class="ki-mono">v{{ row.version }}</strong></template>
              </el-table-column>
              <el-table-column prop="etag" label="SHA-256 ETag" width="150">
                <template #default="{ row }"><code class="ki-mono">{{ row.etag }}</code></template>
              </el-table-column>
              <el-table-column prop="itemCount" label="项数" width="70" />
              <el-table-column prop="comment" label="发布备注" min-width="160" />
              <el-table-column prop="releasedBy" label="发布人" width="90" />
              <el-table-column label="操作" width="90" align="right">
                <template #default="{ row }">
                  <el-button link type="primary" size="small" @click="rollbackVersion(row.version)">
                    <RotateCcw :size="12" class="btn-ico" />回滚
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane label="三级参数合并与 K8s 规格计算" name="calc">
            <div class="calc-grid">
              <div>
                <div class="section-title">Spec &gt; Service &gt; Global 三级合并</div>
                <el-form label-position="top" size="small">
                  <el-form-item label="Global 全局参数 (KEY=VAL)"><el-input v-model="calcForm.globalRaw" type="textarea" :rows="3" /></el-form-item>
                  <el-form-item label="Service 组件参数 (覆盖 Global)"><el-input v-model="calcForm.serviceRaw" type="textarea" :rows="2" /></el-form-item>
                  <el-form-item label="Spec 实例参数 (最高优先级)"><el-input v-model="calcForm.specRaw" type="textarea" :rows="2" /></el-form-item>
                  <div class="three-col">
                    <el-form-item label="单副本 CPU (核)"><el-input-number v-model="calcForm.cpuCores" :step="0.5" :min="0.5" style="width: 100%" /></el-form-item>
                    <el-form-item label="单副本内存 (GB)"><el-input-number v-model="calcForm.memGb" :step="1" :min="0.5" style="width: 100%" /></el-form-item>
                    <el-form-item label="副本数"><el-input-number v-model="calcForm.replicas" :min="1" :max="64" style="width: 100%" /></el-form-item>
                  </div>
                  <el-button type="primary" @click="runPreviewCalc">重新计算生效值与 K8s 规格</el-button>
                </el-form>
              </div>
              <div v-if="calcResult">
                <div class="section-title">K8s 毫核 / MiB 与 JVM 推荐规格</div>
                <div class="kv-grid">
                  <div class="k">CPU Request</div><div class="ki-mono">{{ calcResult.k8sSpec.requestCpuM }}m</div>
                  <div class="k">CPU Limit</div><div class="ki-mono">{{ calcResult.k8sSpec.limitCpuM }}m</div>
                  <div class="k">Mem Request</div><div class="ki-mono">{{ calcResult.k8sSpec.requestMemMi }}Mi</div>
                  <div class="k">Mem Limit</div><div class="ki-mono">{{ calcResult.k8sSpec.limitMemMi }}Mi</div>
                  <div class="k">JVM 推荐</div><div class="ki-mono">-Xms{{ calcResult.k8sSpec.jvmXms }} -Xmx{{ calcResult.k8sSpec.jvmXmx }}</div>
                  <div class="k">集群总配额</div><div class="ki-mono">{{ calcResult.k8sSpec.totalCpuCores }}C / {{ calcResult.k8sSpec.totalMemGb }}GB</div>
                </div>
                <div class="section-title">最终生效参数与来源追踪</div>
                <el-table :data="calcResult.trace" size="small">
                  <el-table-column prop="key" label="Key" width="130" />
                  <el-table-column prop="value" label="最终生效值" min-width="110" />
                  <el-table-column prop="source" label="命中层级" width="90" />
                </el-table>
              </div>
            </div>
          </el-tab-pane>
        </el-tabs>
      </main>
    </div>

    <el-dialog v-model="showNSDialog" title="新建配置命名空间" width="440px">
      <el-form label-position="top">
        <el-form-item label="应用编码"><el-input v-model="nsForm.appCode" placeholder="his-billing" /></el-form-item>
        <el-form-item label="配置集名称"><el-input v-model="nsForm.name" placeholder="application" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="nsForm.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showNSDialog = false">取消</el-button>
        <el-button type="primary" @click="createNamespace">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showItemDialog" title="新增/修改配置项" width="460px">
      <el-form label-position="top">
        <el-form-item label="配置键 (Key)"><el-input v-model="itemForm.key" placeholder="DB_PASSWORD" /></el-form-item>
        <el-form-item label="配置值"><el-input v-model="itemForm.value" :type="itemForm.isSecret ? 'password' : 'text'" show-password /></el-form-item>
        <el-form-item><el-checkbox v-model="itemForm.isSecret">启用 KMS 信封加密（列表脱敏 + 审计显密）</el-checkbox></el-form-item>
        <el-form-item label="备注"><el-input v-model="itemForm.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showItemDialog = false">取消</el-button>
        <el-button type="primary" @click="saveItem">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showCloneDialog" title="跨环境安全克隆（自动置空全部密钥）" width="440px">
      <el-form label-position="top">
        <el-form-item label="目标环境">
          <el-select v-model="cloneForm.targetEnv" style="width: 100%">
            <el-option v-for="e in envs" :key="e" :label="e.toUpperCase()" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标应用编码"><el-input v-model="cloneForm.targetApp" /></el-form-item>
        <el-form-item label="目标配置集名称"><el-input v-model="cloneForm.targetName" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCloneDialog = false">取消</el-button>
        <el-button type="primary" @click="cloneNamespace">确认安全克隆</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.env-bar {
  margin-bottom: var(--ki-space-3);
}

.cfg-layout {
  display: grid;
  grid-template-columns: 270px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

.ns-btn {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 10px 12px;
  margin-bottom: 8px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-subtle);
  text-align: left;
  cursor: pointer;
  font-family: var(--ki-font-sans);
}

.ns-btn.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.ns-top {
  display: flex;
  justify-content: space-between;
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-sm);
}

.ns-ver {
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.ns-sub,
.empty-ns {
  font-size: var(--ki-fs-xs);
}

.tag-gap {
  margin-left: 8px;
}

.sec-val {
  margin-left: 8px;
}

.pub-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.calc-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--ki-space-4);
}

.three-col {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
}

@media (max-width: 960px) {
  .cfg-layout,
  .calc-grid {
    grid-template-columns: 1fr;
  }
}
</style>
