<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Copy, KeyRound, Plus, RefreshCw, RotateCw, Server, ShieldAlert } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface NodeItem {
  node_id: string
  hostname: string
  ip_address: string
  os_arch: string
  agent_version: string
  status: string
  labels?: Record<string, string>
  cred_version: number
  last_seen_at: string
  enrolled_at: string
}

const scope = useScopeStore()
const nodes = ref<NodeItem[]>([])
const loading = ref(false)
const statusFilter = ref<'ALL' | 'ONLINE' | 'DEGRADED' | 'OFFLINE'>('ALL')
const keyword = ref('')

// Enroll wizard modal
const enrollModal = ref(false)
const enrollZone = ref('dc-a')
const enrollTTL = ref(15)
const issuedToken = ref('')
const issuing = ref(false)

// Detail drawer
const detailNode = ref<NodeItem | null>(null)
const drawerOpen = ref(false)
const rotatedCred = ref<{ nodeId: string; hostname: string; cred: string } | null>(null)

const counts = computed(() => {
  const all = nodes.value.length
  const online = nodes.value.filter((n) => n.status === 'ONLINE').length
  const degraded = nodes.value.filter((n) => n.status === 'DEGRADED' || n.status === 'ISOLATED').length
  const offline = nodes.value.filter((n) => n.status === 'OFFLINE' || n.status === 'REVOKED').length
  return { all, online, degraded, offline }
})

const filtered = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return nodes.value.filter((n) => {
    if (statusFilter.value === 'ONLINE' && n.status !== 'ONLINE') return false
    if (statusFilter.value === 'DEGRADED' && n.status !== 'DEGRADED' && n.status !== 'ISOLATED') return false
    if (statusFilter.value === 'OFFLINE' && n.status !== 'OFFLINE' && n.status !== 'REVOKED') return false
    if (!q) return true
    return n.hostname.toLowerCase().includes(q) || n.ip_address.toLowerCase().includes(q) || n.node_id.toLowerCase().includes(q)
  })
})

const enrollCommand = computed(() => {
  const origin = window.location.origin
  return `./bin/liveops-node --server ${origin} --enroll-token ${issuedToken.value}`
})

function statusMeta(st: string) {
  switch (st) {
    case 'ONLINE':
      return { label: '运行中', cls: 'ks-pill-ok' }
    case 'DEGRADED':
      return { label: '心跳降级', cls: 'ks-pill-warn' }
    case 'ISOLATED':
      return { label: '已隔离', cls: 'ks-pill-warn' }
    case 'REVOKED':
      return { label: '已吊销', cls: 'ks-pill-danger' }
    default:
      return { label: '已离线', cls: 'ks-pill-danger' }
  }
}

function fmtRelative(iso: string) {
  if (!iso) return '-'
  const sec = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (sec < 30) return '刚刚'
  if (sec < 60) return `${sec} 秒前`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`
  return new Date(iso).toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: NodeItem[] }>('/api/v1/nodes?allApps=true')
    nodes.value = data.items || []
    if (detailNode.value) {
      const updated = nodes.value.find((n) => n.node_id === detailNode.value?.node_id)
      if (updated) detailNode.value = updated
    }
  } finally {
    loading.value = false
  }
}

function openEnrollModal() {
  issuedToken.value = ''
  enrollModal.value = true
}

async function generateToken() {
  issuing.value = true
  try {
    const { data } = await api.post<{ token: string }>('/api/v1/nodes/enroll-tokens', {
      ttlMinutes: enrollTTL.value,
      labels: { zone: enrollZone.value },
    })
    issuedToken.value = data.token
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  } finally {
    issuing.value = false
  }
}

async function copyText(text: string, msg = '已复制到剪贴板') {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(msg)
  } catch {
    ElMessage.info('请手动选中并复制')
  }
}

function openDetail(node: NodeItem) {
  detailNode.value = node
  drawerOpen.value = true
}

async function rotateCred(node: NodeItem) {
  try {
    const { data } = await api.post<{ newCredential: string }>(`/api/v1/nodes/${node.node_id}/rotate-credential`)
    rotatedCred.value = { nodeId: node.node_id, hostname: node.hostname, cred: data.newCredential }
    ElMessage.success(`节点 ${node.hostname} 安全凭据已轮换（旧凭据 5 分钟内平滑过渡）`)
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function setAction(node: NodeItem, act: 'isolate' | 'unisolate' | 'revoke') {
  if (act === 'revoke') {
    try {
      await ElMessageBox.confirm(`吊销后节点「${node.hostname}」将立即断开控制面通信且无法恢复，确认继续？`, '吊销节点确认', {
        type: 'warning',
      })
    } catch {
      return
    }
  }
  try {
    await api.post(`/api/v1/nodes/${node.node_id}/${act}`)
    ElMessage.success('节点状态已更新')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

onMounted(load)
watch(() => [scope.tenantId, scope.appId], load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="节点管理"
      description="统一纳管数据中心与边缘主机节点，支持一次性令牌安全接入、实时心跳巡检、双轨凭据轮换与节点隔离管控。"
      :icon="Server"
      :badge="`${counts.online}/${counts.all} 在线`"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="openEnrollModal"><Plus :size="14" /><span class="ks-gap">接入新节点</span></el-button>
      </template>
    </KsBanner>

    <!-- KubeSphere Status Filter Chips -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': statusFilter === 'ALL' }" @click="statusFilter = 'ALL'">
        <div class="ki-chip__bar" />
        <div>
          <div class="ki-chip__label">全部节点</div>
          <div class="ki-chip__val ki-num">{{ counts.all }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': statusFilter === 'ONLINE' }" @click="statusFilter = 'ONLINE'">
        <div class="ki-chip__bar ki-chip__bar--ok" />
        <div>
          <div class="ki-chip__label">运行中</div>
          <div class="ki-chip__val ki-num">{{ counts.online }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': statusFilter === 'DEGRADED' }" @click="statusFilter = 'DEGRADED'">
        <div class="ki-chip__bar ki-chip__bar--warn" />
        <div>
          <div class="ki-chip__label">降级 / 已隔离</div>
          <div class="ki-chip__val ki-num">{{ counts.degraded }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': statusFilter === 'OFFLINE' }" @click="statusFilter = 'OFFLINE'">
        <div class="ki-chip__bar ki-chip__bar--danger" />
        <div>
          <div class="ki-chip__label">已离线 / 吊销</div>
          <div class="ki-chip__val ki-num">{{ counts.offline }}</div>
        </div>
      </div>
    </div>

    <div v-if="rotatedCred" class="ki-card ks-alert-card">
      <div class="ks-alert-head">
        <div class="ks-alert-title"><KeyRound :size="15" /><span>节点 {{ rotatedCred.hostname }} 新安全凭据（仅展示一次）</span></div>
        <el-button size="small" @click="copyText(rotatedCred.cred, '凭据已复制')"><Copy :size="12" /><span class="ks-gap-xs">复制凭据</span></el-button>
      </div>
      <code class="ki-mono ks-code-line">{{ rotatedCred.cred }}</code>
    </div>

    <div class="ki-card">
      <div class="ks-bar">
        <el-input v-model="keyword" placeholder="按主机名或内网 IP 检索..." clearable style="max-width: 300px" />
      </div>
      <el-table :data="filtered" size="small" empty-text="当前筛选条件下暂无主机节点，点击「接入新节点」获取一次性接入命令">
        <el-table-column label="主机名称" min-width="210">
          <template #default="{ row }">
            <button type="button" class="ks-link-btn" @click="openDetail(row)">{{ row.hostname }}</button>
            <div class="ks-sub ki-mono">{{ row.node_id }}</div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="115">
          <template #default="{ row }">
            <span class="ki-badge" :class="statusMeta(row.status).cls">{{ statusMeta(row.status).label }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="ip_address" label="IP 地址" width="135">
          <template #default="{ row }"><span class="ki-mono">{{ row.ip_address || '-' }}</span></template>
        </el-table-column>
        <el-table-column prop="os_arch" label="系统架构" width="120" />
        <el-table-column label="最近心跳" width="130">
          <template #default="{ row }"><span class="ki-muted">{{ fmtRelative(row.last_seen_at) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="240">
          <template #default="{ row }">
            <div class="ki-row-actions">
              <el-button size="small" @click="openDetail(row)">详情</el-button>
              <el-button size="small" :disabled="row.status === 'REVOKED'" @click="rotateCred(row)">
                <RotateCw :size="12" /><span class="ks-gap-xs">轮换凭据</span>
              </el-button>
              <el-button v-if="row.status !== 'ISOLATED'" size="small" :disabled="row.status === 'REVOKED'" @click="setAction(row, 'isolate')">隔离</el-button>
              <el-button v-else size="small" @click="setAction(row, 'unisolate')">恢复</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- Node Detail Drawer -->
    <el-drawer v-model="drawerOpen" :title="detailNode?.hostname || '节点详情'" size="520px">
      <div v-if="detailNode" class="ks-drawer-body">
        <div class="section-title">基础信息</div>
        <div class="kv-grid">
          <div class="k">节点状态</div>
          <div><span class="ki-badge" :class="statusMeta(detailNode.status).cls">{{ statusMeta(detailNode.status).label }}</span></div>
          <div class="k">凭据版本</div>
          <div class="ki-mono">v{{ detailNode.cred_version }}</div>
          <div class="k">内网 IP</div>
          <div class="ki-mono">{{ detailNode.ip_address || '-' }}</div>
          <div class="k">系统架构</div>
          <div class="ki-mono">{{ detailNode.os_arch }}</div>
          <div class="k">Agent 版本</div>
          <div class="ki-mono">{{ detailNode.agent_version }}</div>
          <div class="k">最近心跳</div>
          <div>{{ fmtRelative(detailNode.last_seen_at) }}</div>
          <div class="k">节点标识</div>
          <div class="full ki-mono">{{ detailNode.node_id }}</div>
        </div>

        <div class="section-title">节点标签</div>
        <div class="ks-tags">
          <span v-for="(v, k) in detailNode.labels || {}" :key="k" class="ki-badge ks-pill-blue ki-mono">{{ k }}={{ v }}</span>
          <span v-if="!detailNode.labels || Object.keys(detailNode.labels).length === 0" class="ki-muted">暂无自定义标签</span>
        </div>

        <div class="section-title">安全管控</div>
        <div class="ks-drawer-actions">
          <el-button :disabled="detailNode.status === 'REVOKED'" @click="rotateCred(detailNode)">轮换节点凭据</el-button>
          <el-button v-if="detailNode.status !== 'ISOLATED'" :disabled="detailNode.status === 'REVOKED'" @click="setAction(detailNode, 'isolate')">隔离节点</el-button>
          <el-button v-else @click="setAction(detailNode, 'unisolate')">取消隔离</el-button>
          <el-button type="danger" plain :disabled="detailNode.status === 'REVOKED'" @click="setAction(detailNode, 'revoke')">
            <ShieldAlert :size="13" /><span class="ks-gap-xs">永久吊销</span>
          </el-button>
        </div>
      </div>
    </el-drawer>

    <!-- Guided Enroll Modal -->
    <el-dialog v-model="enrollModal" title="接入新主机节点" width="580px">
      <el-form label-position="top">
        <div class="ks-form-row">
          <el-form-item label="归属可用区 / 机房标签">
            <el-input v-model="enrollZone" placeholder="例如: dc-a" />
          </el-form-item>
          <el-form-item label="令牌有效期（分钟）">
            <el-input-number v-model="enrollTTL" :min="5" :max="120" style="width: 100%" />
          </el-form-item>
        </div>
      </el-form>

      <div v-if="!issuedToken" class="ks-gen-wrap">
        <el-button type="primary" :loading="issuing" @click="generateToken">生成一次性接入指令</el-button>
      </div>

      <div v-else class="ks-cmd-section">
        <div class="ks-cmd-head">
          <span>在目标主机执行以下命令完成安全注册（一次性有效）：</span>
          <el-button size="small" @click="copyText(enrollCommand, '接入命令已复制')">
            <Copy :size="12" /><span class="ks-gap-xs">复制命令</span>
          </el-button>
        </div>
        <pre class="ks-cmd-box ki-mono">{{ enrollCommand }}</pre>
      </div>

      <template #footer>
        <el-button @click="enrollModal = false">关闭</el-button>
        <el-button v-if="issuedToken" type="primary" @click="load(); enrollModal = false">刷新节点列表</el-button>
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

.ks-bar {
  margin-bottom: var(--ki-space-3);
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

.ks-alert-card {
  margin-bottom: var(--ki-space-4);
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
}

.ks-alert-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}

.ks-alert-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-status-ok-text);
}

.ks-code-line {
  display: block;
  word-break: break-all;
  color: var(--ki-text-primary);
}

.ks-form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}

.ks-gen-wrap {
  padding: 8px 0 4px;
}

.ks-cmd-section {
  margin-top: 8px;
}

.ks-cmd-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
  margin-bottom: 8px;
}

.ks-cmd-box {
  padding: 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
}

.ks-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ks-drawer-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}
</style>
