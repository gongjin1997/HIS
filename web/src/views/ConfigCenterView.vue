<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Copy, Eye, GitCompare, History, Lock, Plus, RotateCcw, Send, Trash2 } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'

interface Namespace {
  id: string
  env: string
  name: string
  formatKind: string
  description: string
  currentReleaseVer: number
  itemCount: number
}

interface ConfigItem {
  id: string
  itemKey: string
  itemType: string
  value: string
  hasSecret: boolean
  description: string
  version: number
  updatedBy: string
  updatedAt: string
}

interface Release {
  id: string
  releaseVer: number
  etag: string
  comment: string
  orcTicketNo: string
  publishedBy: string
  publishedAt: string
  items: Array<{ key: string; type: string; valuePlain: string }>
}

interface DiffEntry {
  key: string
  changeType: string
  oldValue?: string
  newValue?: string
}

const ws = useWorkspaceStore()
const { formatRelTime } = useKiFormat()
const loading = ref(false)
const envFilter = ref('prod')
const namespaces = ref<Namespace[]>([])
const selectedNsId = ref('')
const items = ref<ConfigItem[]>([])
const releases = ref<Release[]>([])
const diffs = ref<DiffEntry[]>([])
const activeTab = ref<'items' | 'diff' | 'releases'>('items')

const nsDialog = reactive({ open: false, env: 'prod', name: '', formatKind: 'kv', description: '' })
const itemDialog = reactive({ open: false, itemKey: '', itemType: 'string', value: '', description: '' })
const pubDialog = reactive({ open: false, comment: '', orcTicketNo: '' })
const cloneDialog = reactive({ open: false, targetEnv: 'uat', targetName: '' })
const revealDialog = reactive({ open: false, key: '', reason: '变更核查', plaintext: '' })

const currentNs = computed(() => namespaces.value.find((n) => n.id === selectedNsId.value) || namespaces.value[0] || null)
const changedCount = computed(() => diffs.value.filter((d) => d.changeType !== 'unchanged').length)

async function loadNamespaces() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Namespace[] }>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/config/namespaces?env=${encodeURIComponent(envFilter.value)}`
    )
    namespaces.value = data.items || []
    if (!namespaces.value.some((n) => n.id === selectedNsId.value)) {
      selectedNsId.value = namespaces.value[0]?.id || ''
    }
    if (selectedNsId.value) {
      await loadNsDetails(selectedNsId.value)
    } else {
      items.value = []
      releases.value = []
      diffs.value = []
    }
  } finally {
    loading.value = false
  }
}

async function loadNsDetails(nsId: string) {
  if (!ws.currentProjectId || !nsId) return
  const pid = ws.currentProjectId
  const [iRes, dRes, rRes] = await Promise.all([
    apiRequest<{ items: ConfigItem[] }>('GET', `/api/v1/projects/${pid}/config/namespaces/${encodeURIComponent(nsId)}/items`),
    apiRequest<{ items: DiffEntry[] }>('GET', `/api/v1/projects/${pid}/config/namespaces/${encodeURIComponent(nsId)}/diff`),
    apiRequest<{ items: Release[] }>('GET', `/api/v1/projects/${pid}/config/namespaces/${encodeURIComponent(nsId)}/releases`),
  ])
  items.value = iRes.data.items || []
  diffs.value = dRes.data.items || []
  releases.value = rRes.data.items || []
}

async function createNamespace() {
  try {
    const { data } = await apiRequest<Namespace>('POST', `/api/v1/projects/${ws.currentProjectId}/config/namespaces`, { body: nsDialog })
    ElMessage.success(`命名空间 ${data.env}/${data.name} 已创建`)
    envFilter.value = data.env
    selectedNsId.value = data.id
    nsDialog.open = false
    nsDialog.name = ''
    await loadNamespaces()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openItemEdit(row?: ConfigItem) {
  itemDialog.itemKey = row?.itemKey || ''
  itemDialog.itemType = row?.itemType || 'string'
  itemDialog.value = row?.value || ''
  itemDialog.description = row?.description || ''
  itemDialog.open = true
}

async function saveItem() {
  if (!currentNs.value) return
  try {
    await apiRequest('PUT', `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/items`, {
      body: itemDialog,
    })
    ElMessage.success('配置草稿已保存（密钥项已强制 KMS 信封加密）')
    itemDialog.open = false
    await loadNsDetails(currentNs.value.id)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function deleteItem(key: string) {
  if (!currentNs.value) return
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/items/${encodeURIComponent(key)}`)
  ElMessage.success(`已删除配置项 ${key}`)
  await loadNsDetails(currentNs.value.id)
}

function openReveal(key: string) {
  revealDialog.key = key
  revealDialog.reason = '生产发布核验'
  revealDialog.plaintext = ''
  revealDialog.open = true
}

async function doReveal() {
  if (!currentNs.value) return
  try {
    const { data } = await apiRequest<{ value: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/items/${encodeURIComponent(revealDialog.key)}/reveal`,
      { body: { reason: revealDialog.reason } }
    )
    revealDialog.plaintext = data.value
    ElMessage.success('已解密明文并写入只追加防篡改审计链')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function publishRelease() {
  if (!currentNs.value) return
  try {
    const { data } = await apiRequest<Release>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/publish`,
      { body: pubDialog }
    )
    ElMessage.success(`已固化不可变版本快照 v${data.releaseVer} (ETag: ${data.etag.slice(0, 12)})`)
    pubDialog.open = false
    pubDialog.comment = ''
    await loadNamespaces()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function rollbackTo(ver: number) {
  if (!currentNs.value) return
  try {
    await ElMessageBox.confirm(`确认以历史快照 v${ver} 生成全新版本并回滚当前命名空间吗？`, '不可变快照回滚', { type: 'warning' })
    const { data } = await apiRequest<Release>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/rollback`,
      { body: { targetVer: ver, comment: `rollback to v${ver}` } }
    )
    ElMessage.success(`已生成回滚新版本 v${data.releaseVer}`)
    await loadNamespaces()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function cloneNs() {
  if (!currentNs.value) return
  try {
    const { data } = await apiRequest<Namespace>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/config/namespaces/${encodeURIComponent(currentNs.value.id)}/clone`,
      { body: cloneDialog }
    )
    ElMessage.success(`已克隆至 ${data.env}/${data.name}（所有密钥项已安全置空防跨环境泄露）`)
    cloneDialog.open = false
    envFilter.value = data.env
    selectedNsId.value = data.id
    await loadNamespaces()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(envFilter, loadNamespaces)
watch(selectedNsId, (id) => {
  if (id) void loadNsDetails(id)
})
watch(() => ws.currentProjectId, loadNamespaces)
onMounted(loadNamespaces)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="多环境版本化配置与密钥中心"
      subtitle="对标 LiveOps 03.4：dev/sit/uat/beta/prod 五环境物理隔离、KMS 信封密钥脱敏与审计显密、草稿 Diff 与不可变快照一键回滚"
      :refresh="loadNamespaces"
      :refreshing="loading"
    >
      <template #actions>
        <el-button v-if="currentNs" @click="cloneDialog.targetName = currentNs.name; cloneDialog.open = true">
          <Copy :size="14" class="btn-ico" /> 跨环境安全克隆
        </el-button>
        <el-button v-if="currentNs" type="primary" @click="pubDialog.open = true">
          <Send :size="14" class="btn-ico" /> 发布不可变快照 ({{ changedCount }})
        </el-button>
      </template>
    </PageHeader>

    <div class="env-bar ki-mb-3">
      <el-radio-group v-model="envFilter" size="small">
        <el-radio-button value="prod">PROD 生产</el-radio-button>
        <el-radio-button value="beta">BETA 灰度</el-radio-button>
        <el-radio-button value="uat">UAT 预发</el-radio-button>
        <el-radio-button value="sit">SIT 联调</el-radio-button>
        <el-radio-button value="dev">DEV 开发</el-radio-button>
      </el-radio-group>
      <el-button size="small" @click="nsDialog.env = envFilter; nsDialog.open = true">
        <Plus :size="13" class="btn-ico" /> 新建命名空间
      </el-button>
    </div>

    <div class="cfg-layout">
      <div class="ki-card ns-rail">
        <div class="rail-title">{{ envFilter.toUpperCase() }} 命名空间 ({{ namespaces.length }})</div>
        <div
          v-for="ns in namespaces"
          :key="ns.id"
          class="ns-item"
          :class="{ 'ns-item--active': currentNs?.id === ns.id }"
          @click="selectedNsId = ns.id"
        >
          <div class="ns-item__top">
            <b class="ki-mono">{{ ns.name }}</b>
            <span class="ki-mono ns-ver">v{{ ns.currentReleaseVer }}</span>
          </div>
          <div class="ki-muted ns-desc">{{ ns.description || ns.formatKind }}</div>
        </div>
        <div v-if="namespaces.length === 0" class="ki-muted empty-ns">该环境暂无命名空间</div>
      </div>

      <div class="ki-card">
        <el-tabs v-model="activeTab">
          <el-tab-pane :label="`配置项草稿 (${items.length})`" name="items">
            <div class="tab-bar ki-mb-3">
              <span class="ki-muted">敏感项落库强制加密，列表固定返回 <code>***MASKED***</code>，显密必须登记审计。</span>
              <el-button v-if="currentNs" type="primary" size="small" @click="openItemEdit()">
                <Plus :size="13" class="btn-ico" /> 新增配置 / 密钥项
              </el-button>
            </div>
            <el-table :data="items" size="small">
              <el-table-column prop="itemKey" label="键名 (Key)" min-width="170">
                <template #default="{ row }">
                  <div class="ki-flex-row">
                    <Lock v-if="row.itemType === 'secret'" :size="12" class="sec-ico" />
                    <b class="ki-mono">{{ row.itemKey }}</b>
                  </div>
                </template>
              </el-table-column>
              <el-table-column prop="itemType" label="类型" width="84">
                <template #default="{ row }"><span class="ki-badge ki-mono type-tag">{{ row.itemType }}</span></template>
              </el-table-column>
              <el-table-column prop="value" label="配置值（密钥自动脱敏）" min-width="200">
                <template #default="{ row }"><span class="ki-mono">{{ row.value || '(待注入)' }}</span></template>
              </el-table-column>
              <el-table-column prop="description" label="说明" min-width="150" />
              <el-table-column label="操作" width="160" align="right">
                <template #default="{ row }">
                  <el-button v-if="row.itemType === 'secret' && row.hasSecret" link type="warning" size="small" @click="openReveal(row.itemKey)">
                    <Eye :size="12" class="btn-ico" /> 显密
                  </el-button>
                  <el-button link type="primary" size="small" @click="openItemEdit(row)">编辑</el-button>
                  <el-button link type="danger" size="small" @click="deleteItem(row.itemKey)">
                    <Trash2 :size="12" />
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`草稿 vs 线上 Diff (${changedCount})`" name="diff">
            <el-table :data="diffs" size="small">
              <el-table-column prop="key" label="配置键" width="180">
                <template #default="{ row }"><b class="ki-mono">{{ row.key }}</b></template>
              </el-table-column>
              <el-table-column prop="changeType" label="变更类型" width="100">
                <template #default="{ row }">
                  <span class="ki-badge ki-mono" :class="`diff-${row.changeType}`">
                    <GitCompare :size="11" class="btn-ico" /> {{ row.changeType }}
                  </span>
                </template>
              </el-table-column>
              <el-table-column prop="oldValue" label="线上版本值" min-width="170">
                <template #default="{ row }"><span class="ki-mono ki-muted">{{ row.oldValue || '—' }}</span></template>
              </el-table-column>
              <el-table-column prop="newValue" label="当前草稿值" min-width="170">
                <template #default="{ row }"><span class="ki-mono">{{ row.newValue || '—' }}</span></template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`不可变发布历史 (${releases.length})`" name="releases">
            <el-table :data="releases" size="small">
              <el-table-column label="版本" width="84">
                <template #default="{ row }"><b class="ki-mono">v{{ row.releaseVer }}</b></template>
              </el-table-column>
              <el-table-column label="规范化 ETag (SHA-256)" width="170">
                <template #default="{ row }"><KiMonoText :value="row.etag" :head="10" :tail="6" /></template>
              </el-table-column>
              <el-table-column prop="comment" label="发布说明" min-width="170" />
              <el-table-column prop="orcTicketNo" label="变更单号" width="110" />
              <el-table-column prop="publishedBy" label="发布人" width="96" />
              <el-table-column label="时间" width="96">
                <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(row.publishedAt) }}</span></template>
              </el-table-column>
              <el-table-column label="操作" width="110" align="right">
                <template #default="{ row }">
                  <el-button link type="primary" size="small" @click="rollbackTo(row.releaseVer)">
                    <RotateCcw :size="12" class="btn-ico" /> 回滚至此版
                  </el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </div>
    </div>

    <el-dialog v-model="nsDialog.open" title="新建环境配置命名空间" width="460px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="目标环境">
            <el-select v-model="nsDialog.env" class="ki-w-full">
              <el-option v-for="e in ['prod','beta','uat','sit','dev']" :key="e" :label="e.toUpperCase()" :value="e" />
            </el-select>
          </el-form-item>
          <el-form-item label="格式类型">
            <el-select v-model="nsDialog.formatKind" class="ki-w-full">
              <el-option label="KV 键值对" value="kv" />
              <el-option label="YAML 文档" value="yaml" />
              <el-option label="Properties" value="properties" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="命名空间名称"><el-input v-model="nsDialog.name" placeholder="his-outpatient-config" /></el-form-item>
        <el-form-item label="用途说明"><el-input v-model="nsDialog.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nsDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createNamespace">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="itemDialog.open" title="编辑配置 / 加密密钥项" width="520px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="键名 (Key) *"><el-input v-model="itemDialog.itemKey" placeholder="DB_PASSWORD" /></el-form-item>
          <el-form-item label="值类型">
            <el-select v-model="itemDialog.itemType" class="ki-w-full">
              <el-option label="字符串 (string)" value="string" />
              <el-option label="加密密钥 (secret · KMS 信封)" value="secret" />
              <el-option label="JSON 文档 (json)" value="json" />
              <el-option label="YAML 文档 (yaml)" value="yaml" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="配置值">
          <el-input v-model="itemDialog.value" :type="itemDialog.itemType === 'secret' ? 'password' : 'textarea'" :rows="4" show-password />
        </el-form-item>
        <el-form-item label="备注说明"><el-input v-model="itemDialog.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="itemDialog.open = false">取消</el-button>
        <el-button type="primary" @click="saveItem">保存草稿</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="pubDialog.open" title="发布不可变配置快照" width="460px">
      <el-form label-position="top">
        <el-form-item label="变更说明"><el-input v-model="pubDialog.comment" placeholder="例如：更新内网镜像仓库地址与连接池参数" /></el-form-item>
        <el-form-item label="关联变更单号 (ORC Ticket)"><el-input v-model="pubDialog.orcTicketNo" placeholder="CHG-2026-001" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pubDialog.open = false">取消</el-button>
        <el-button type="primary" @click="publishRelease"><History :size="13" class="btn-ico" /> 固化发布快照</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="cloneDialog.open" title="跨环境安全克隆命名空间" width="440px">
      <p class="ki-muted clone-warn">安全红线：克隆至目标环境时，所有 <code>secret</code> 密钥项将自动置空，严禁将测试环境或生产密钥互相泄露。</p>
      <el-form label-position="top">
        <el-form-item label="目标环境">
          <el-select v-model="cloneDialog.targetEnv" class="ki-w-full">
            <el-option v-for="e in ['prod','beta','uat','sit','dev']" :key="e" :label="e.toUpperCase()" :value="e" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标命名空间名称"><el-input v-model="cloneDialog.targetName" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cloneDialog.open = false">取消</el-button>
        <el-button type="primary" @click="cloneNs">确认安全克隆</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="revealDialog.open" :title="`审计级密钥显密 · ${revealDialog.key}`" width="460px">
      <el-form label-position="top">
        <el-form-item label="显密原因（将不可篡改记录至审计链）">
          <el-input v-model="revealDialog.reason" />
        </el-form-item>
        <el-form-item v-if="revealDialog.plaintext" label="内存实时解密明文">
          <el-input :model-value="revealDialog.plaintext" readonly class="ki-mono" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="revealDialog.open = false">关闭</el-button>
        <el-button type="warning" @click="doReveal">登记审计并显密</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.env-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.cfg-layout {
  display: grid;
  grid-template-columns: 268px 1fr;
  gap: 16px;
  align-items: start;
}

@media (width <= 1080px) {
  .cfg-layout {
    grid-template-columns: 1fr;
  }
}

.rail-title {
  font-size: var(--ki-fs-xs);
  font-weight: 600;
  color: var(--ki-text-muted);
  margin-bottom: 10px;
}

.ns-item {
  padding: 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  margin-bottom: 8px;
  cursor: pointer;
}

.ns-item--active {
  border-color: var(--ki-brand-primary);
  background-color: var(--ki-brand-primary-bg);
}

.ns-item__top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--ki-fs-xs);
}

.ns-ver {
  font-size: 11px;
  color: var(--ki-brand-primary);
}

.ns-desc {
  font-size: 11px;
  margin-top: 4px;
}

.empty-ns {
  padding: 16px 0;
  text-align: center;
  font-size: var(--ki-fs-xs);
}

.tab-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  font-size: var(--ki-fs-xs);
}

.sec-ico {
  color: var(--ki-status-warn);
}

.type-tag {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
}

.diff-added {
  color: var(--ki-status-ok);
  background-color: var(--ki-bg-subtle);
}

.diff-modified {
  color: var(--ki-status-warn);
  background-color: var(--ki-bg-subtle);
}

.diff-removed {
  color: var(--ki-status-danger);
  background-color: var(--ki-bg-subtle);
}

.diff-unchanged {
  color: var(--ki-text-muted);
  background-color: var(--ki-bg-subtle);
}

.clone-warn {
  margin: 0 0 12px;
  font-size: var(--ki-fs-xs);
}
</style>
