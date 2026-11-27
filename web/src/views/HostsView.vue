<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Activity,
  Cpu,
  Plus,
  Radar,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  Trash2,
  Upload,
  Wrench,
} from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'

interface ImportRow {
  rowNumber: number
  name: string
  address: string
  port: number
  user: string
  zone?: string
}

interface ImportRowError {
  rowNumber: number
  field: string
  code: string
  message: string
}

interface DriftFinding {
  category: string
  key: string
  expected: string
  observed: string
  severity: string
}

interface DiscoveredHost {
  address: string
  port: number
  reachable: boolean
  latencyMs: number
  banner?: string
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()

const loading = ref(false)
const hosts = ref<Array<Record<string, unknown>>>([])
const credentials = ref<Array<{ id: string; name: string; kind: string }>>([])
const profiles = ref<Record<string, { name: string; description: string }>>({})
const modules = ref<Array<Record<string, unknown>>>([])
const activeTab = ref('hosts')
const showAdd = ref(false)
const showImport = ref(false)
const selectedHost = ref<Record<string, unknown> | null>(null)
const drawerOpen = ref(false)
const checkingBaseline = ref(false)
const baselineDrifts = ref<DriftFinding[] | null>(null)
const baselineCompliant = ref<boolean | null>(null)

const filterArch = ref<string>('ALL')
const filterZone = ref<string>('ALL')
const keyword = ref('')

const newHost = reactive({
  name: '',
  address: '',
  sshPort: 22,
  sshUser: 'root',
  credentialId: '',
  faultZone: 'az-1',
  arch: 'amd64',
  pageSize: 4096,
})

const discoverDialog = reactive({
  open: false,
  cidr: '192.168.2.0/24',
  port: 22,
  scanning: false,
  scanned: false,
  items: [] as DiscoveredHost[],
})

const importState = reactive({
  format: 'csv' as 'csv' | 'yaml' | 'xlsx',
  data: '',
  fileName: '',
  busy: false,
  previewRows: [] as ImportRow[],
  previewErrors: [] as ImportRowError[],
  checked: false,
})

const initDialog = reactive({
  open: false,
  host: null as Record<string, unknown> | null,
  selectedProfiles: ['base'] as string[],
  submitting: false,
})

const deleteModal = reactive({
  open: false,
  host: null as Record<string, unknown> | null,
  loading: false,
})

const zones = computed(() => {
  const s = new Set<string>()
  for (const h of hosts.value) s.add(String(h.faultZone || 'default'))
  return Array.from(s)
})

const filteredHosts = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return hosts.value.filter((h) => {
    if (filterArch.value === '64k' && Number(h.pageSize || 4096) < 65536) return false
    if (filterArch.value === 'amd64' && String(h.arch || 'amd64') !== 'amd64') return false
    if (filterArch.value === 'arm64' && String(h.arch || '') !== 'arm64') return false
    if (filterZone.value !== 'ALL' && String(h.faultZone || 'default') !== filterZone.value) return false
    if (!q) return true
    return `${h.name} ${h.address} ${h.id} ${h.faultZone}`.toLowerCase().includes(q)
  })
})

const arm64kTotal = computed(() => hosts.value.filter((h) => Number(h.pageSize || 4096) >= 65536).length)
const readyTotal = computed(() => hosts.value.filter((h) => ['ready', 'healthy', 'online'].includes(String(h.state || '').toLowerCase())).length)

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [hRes, pRes, cRes] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/hosts`),
      apiRequest<{ profiles: Record<string, { name: string; description: string }>; modules: Array<Record<string, unknown>> }>('GET', '/api/v1/hostinit/profiles'),
      apiRequest<{ items: Array<{ id: string; name: string; kind: string }> }>('GET', `/api/v1/projects/${ws.currentProjectId}/credentials`).catch(() => ({ data: { items: [] } })),
    ])
    hosts.value = hRes.data.items || []
    profiles.value = pRes.data.profiles || {}
    modules.value = pRes.data.modules || []
    credentials.value = cRes.data.items || []
    if (!newHost.credentialId && credentials.value.length > 0) {
      newHost.credentialId = credentials.value[0].id
    }
  } finally {
    loading.value = false
  }
}

async function createHost() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/hosts`, {
      body: { ...newHost },
      idempotent: true,
    })
    ElMessage.success('主机已纳管至资产库')
    showAdd.value = false
    newHost.name = ''
    newHost.address = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function runDiscoverSubnet() {
  if (!discoverDialog.cidr.trim()) {
    ElMessage.warning('请输入要扫描的 CIDR 网段，例如 192.168.1.0/24')
    return
  }
  discoverDialog.scanning = true
  discoverDialog.scanned = false
  try {
    const { data } = await apiRequest<{ items: DiscoveredHost[] }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/hosts:discover`,
      { body: { cidr: discoverDialog.cidr.trim(), port: discoverDialog.port } }
    )
    discoverDialog.items = data.items || []
    discoverDialog.scanned = true
    ElMessage.success(`扫描完成：发现 ${discoverDialog.items.length} 台开放 SSH 端口的在线主机`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    discoverDialog.scanning = false
  }
}

function adoptDiscoveredHost(item: DiscoveredHost) {
  newHost.address = item.address
  newHost.sshPort = item.port || 22
  newHost.name = `node-${item.address.replace(/\./g, '-')}`
  discoverDialog.open = false
  showAdd.value = true
}

async function probeHost(hostId: string) {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/hosts/${hostId}:probe`)
    ElMessage.success('主机 Facts 信息采集完成')
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function checkHostBaseline(hostId: string) {
  checkingBaseline.value = true
  try {
    const { data } = await apiRequest<{ drifts: DriftFinding[]; compliant: boolean }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/hosts/${hostId}:check`
    )
    baselineDrifts.value = data.drifts || []
    baselineCompliant.value = data.compliant
    if (data.compliant) ElMessage.success('基线巡检通过：无内核与安全配置漂移')
    else ElMessage.warning(`发现 ${baselineDrifts.value.length} 项内核/基线漂移`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    checkingBaseline.value = false
  }
}

function openHostDetail(row: Record<string, unknown>) {
  selectedHost.value = row
  baselineDrifts.value = null
  baselineCompliant.value = null
  drawerOpen.value = true
}

function openInitDialog(row: Record<string, unknown>) {
  initDialog.host = row
  const cur = String(row.initProfile || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  initDialog.selectedProfiles = cur.length > 0 ? cur : ['base']
  initDialog.open = true
}

async function submitHostInit() {
  if (!initDialog.host) return
  initDialog.submitting = true
  try {
    const { data } = await apiRequest<{ id: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/hosts/${initDialog.host.id}:init`,
      { body: { profiles: initDialog.selectedProfiles }, idempotent: true }
    )
    initDialog.open = false
    ElMessage.success(`主机基线初始化任务 ${data.id} 已下发`)
    await router.push(`/operations?id=${data.id}`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    initDialog.submitting = false
  }
}

function openDeleteHost(row: Record<string, unknown>) {
  deleteModal.host = row
  deleteModal.open = true
}

async function confirmDeleteHost() {
  if (!deleteModal.host) return
  deleteModal.loading = true
  try {
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/hosts/${deleteModal.host.id}`)
    ElMessage.success(`主机 ${deleteModal.host.name} 已移除`)
    deleteModal.open = false
    drawerOpen.value = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    deleteModal.loading = false
  }
}

function fillSampleCSV() {
  importState.format = 'csv'
  importState.fileName = 'sample-hosts.csv'
  importState.data = 'name,address,port,user,zone,rack\nnode-app-01,10.20.1.21,22,root,az-1,rack-a\nnode-app-02,10.20.1.22,22,root,az-2,rack-b\n'
  importState.checked = false
}

function onImportFileSelected(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importState.fileName = file.name
  importState.checked = false
  const low = file.name.toLowerCase()
  if (low.endsWith('.xlsx')) {
    importState.format = 'xlsx'
    const reader = new FileReader()
    reader.onload = () => {
      const res = String(reader.result || '')
      importState.data = res.includes(',') ? res.split(',')[1] : res
    }
    reader.readAsDataURL(file)
  } else {
    importState.format = low.endsWith('.yaml') || low.endsWith('.yml') ? 'yaml' : 'csv'
    const reader = new FileReader()
    reader.onload = () => {
      importState.data = String(reader.result || '')
    }
    reader.readAsText(file)
  }
}

async function runBulkImport(dryRun: boolean) {
  if (!importState.data.trim()) {
    ElMessage.warning('请上传文件或粘贴主机清单内容')
    return
  }
  importState.busy = true
  try {
    const { data } = await apiRequest<{
      result: { rows?: ImportRow[]; errors?: ImportRowError[] }
      imported: number
      dryRun: boolean
    }>('POST', `/api/v1/projects/${ws.currentProjectId}/hosts:import`, {
      body: { format: importState.format, data: importState.data, dryRun },
      idempotent: !dryRun,
    })
    importState.previewRows = data.result?.rows || []
    importState.previewErrors = data.result?.errors || []
    importState.checked = true
    if (dryRun) {
      if (importState.previewErrors.length === 0) {
        ElMessage.success(`Dry-Run 预检通过：识别 ${importState.previewRows.length} 台主机，0 行错误`)
      } else {
        ElMessage.warning(`Dry-Run 检测到 ${importState.previewErrors.length} 行错误，请按行号修正`)
      }
    } else if (data.imported > 0) {
      ElMessage.success(`成功批量导入 ${data.imported} 台主机`)
      if (importState.previewErrors.length === 0) showImport.value = false
      await loadAll()
    } else {
      ElMessage.warning('未导入任何主机，请检查下方错误行')
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    importState.busy = false
  }
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div class="hosts-page">
    <PageHeader title="主机资产" :refresh="loadAll" :refreshing="loading" />

    <!-- 顶部可点击统计条 -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': filterArch === 'ALL' }" @click="filterArch = 'ALL'">
        <span class="ki-chip__bar" />
        <div>
          <div class="ki-chip__label">全部纳管主机</div>
          <div class="ki-chip__val ki-num">{{ hosts.length }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterArch === 'amd64' }" @click="filterArch = 'amd64'">
        <span class="ki-chip__bar ki-chip__bar--running" />
        <div>
          <div class="ki-chip__label">x86_64 (amd64)</div>
          <div class="ki-chip__val ki-num">{{ hosts.filter((h) => String(h.arch || 'amd64') === 'amd64').length }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterArch === '64k' }" @click="filterArch = '64k'">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div>
          <div class="ki-chip__label">ARM64-64K 节点</div>
          <div class="ki-chip__val ki-num">{{ arm64kTotal }}</div>
        </div>
      </div>
      <div class="ki-chip">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div>
          <div class="ki-chip__label">就绪节点 / 基线模块</div>
          <div class="ki-chip__val ki-num">{{ readyTotal }} / {{ modules.length }}</div>
        </div>
      </div>
    </div>

    <!-- 左右结构：左侧维度树 + 右侧资产主表 -->
    <div class="ledger-body">
      <aside class="ki-card ledger-nav">
        <div class="nav-root" :class="{ 'is-active': filterZone === 'ALL' && filterArch === 'ALL' }" @click="filterZone = 'ALL'; filterArch = 'ALL'">
          <Server :size="14" />
          <span>全部资产节点</span>
          <em class="ki-num">{{ hosts.length }}</em>
        </div>

        <div class="nav-section"><Cpu :size="13" /> CPU 架构与页大小</div>
        <div class="nav-item" :class="{ 'is-active': filterArch === 'amd64' }" @click="filterArch = 'amd64'">
          <span>amd64 · 4K</span>
          <em class="ki-num">{{ hosts.filter((h) => String(h.arch || 'amd64') === 'amd64').length }}</em>
        </div>
        <div class="nav-item" :class="{ 'is-active': filterArch === 'arm64' }" @click="filterArch = 'arm64'">
          <span>arm64 (鲲鹏 / 飞腾)</span>
          <em class="ki-num">{{ hosts.filter((h) => String(h.arch || '') === 'arm64').length }}</em>
        </div>
        <div class="nav-item" :class="{ 'is-active': filterArch === '64k' }" @click="filterArch = '64k'">
          <span>arm64 · 64K 页</span>
          <em class="ki-num">{{ arm64kTotal }}</em>
        </div>

        <div class="nav-section"><ShieldCheck :size="13" /> 故障域 / 机架</div>
        <div class="nav-item" :class="{ 'is-active': filterZone === 'ALL' }" @click="filterZone = 'ALL'">
          <span>全部故障域</span>
          <em class="ki-num">{{ hosts.length }}</em>
        </div>
        <div
          v-for="z in zones"
          :key="z"
          class="nav-item"
          :class="{ 'is-active': filterZone === z }"
          @click="filterZone = z"
        >
          <span class="ki-mono">{{ z }}</span>
          <em class="ki-num">{{ hosts.filter((h) => String(h.faultZone || 'default') === z).length }}</em>
        </div>
      </aside>

      <section class="ki-card ledger-main">
        <div class="table-heading">
          <span class="ki-secondary">支持单台录入、网段 SSH 自动发现、Excel/CSV 批量导入与 {{ Object.keys(profiles).length }} 套生产内核基线初始化</span>
          <div class="table-actions">
            <el-button @click="loadAll"><RefreshCw :size="14" class="btn-ico" /> 刷新</el-button>
            <el-button v-if="auth.can('host:probe', ws.currentProjectId)" @click="discoverDialog.open = true">
              <Radar :size="14" class="btn-ico" /> 网段发现
            </el-button>
            <el-button v-if="auth.can('host:write', ws.currentProjectId)" @click="showImport = true">
              <Upload :size="14" class="btn-ico" /> 批量导入
            </el-button>
            <el-button v-if="auth.can('host:write', ws.currentProjectId)" type="primary" @click="showAdd = true">
              <Plus :size="14" class="btn-ico" /> 新增纳管主机
            </el-button>
          </div>
        </div>

        <el-tabs v-model="activeTab">
          <el-tab-pane :label="`主机清单 (${filteredHosts.length})`" name="hosts">
            <div class="filter-bar">
              <el-input v-model="keyword" clearable placeholder="搜索主机名 / IP / 主机 ID / 故障域" class="search-kw">
                <template #prefix><Search :size="14" /></template>
              </el-input>
            </div>

            <el-table v-if="filteredHosts.length > 0 || loading" v-loading="loading" :data="filteredHosts" @row-click="openHostDetail">
              <el-table-column label="主机名称" min-width="175">
                <template #default="{ row }">
                  <div class="host-name ki-mono">{{ row.name }}</div>
                  <div class="ki-muted ki-mono host-sub">{{ row.id }}</div>
                </template>
              </el-table-column>
              <el-table-column label="SSH 端点" min-width="165">
                <template #default="{ row }">
                  <span class="ki-mono">{{ row.sshUser || 'root' }}@{{ row.address }}:{{ row.sshPort || 22 }}</span>
                </template>
              </el-table-column>
              <el-table-column label="架构 / 页大小" width="136">
                <template #default="{ row }">
                  <KiDomainTag kind="arch" :value="String(row.arch || 'amd64')" :page-size="Number(row.pageSize || 4096)" />
                </template>
              </el-table-column>
              <el-table-column prop="faultZone" label="故障域" width="88" />
              <el-table-column label="状态" width="108">
                <template #default="{ row }">
                  <KiStatusBadge :status="String(row.state || 'pending')" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="215" fixed="right">
                <template #default="{ row }">
                  <div class="ki-row-actions">
                    <el-button link type="primary" @click.stop="openHostDetail(row)">详情</el-button>
                    <el-button
                      v-if="auth.can('host:probe', ws.currentProjectId)"
                      link
                      type="primary"
                      @click.stop="probeHost(String(row.id))"
                    >
                      探活
                    </el-button>
                    <el-button
                      v-if="auth.can('host:write', ws.currentProjectId)"
                      link
                      type="primary"
                      @click.stop="openInitDialog(row)"
                    >
                      初始化
                    </el-button>
                    <el-button
                      v-if="auth.can('host:write', ws.currentProjectId)"
                      link
                      type="danger"
                      @click.stop="openDeleteHost(row)"
                    >
                      删除
                    </el-button>
                  </div>
                </template>
              </el-table-column>
            </el-table>

            <div v-else class="ki-empty-state">
              <div class="ki-empty-state__icon"><Server :size="20" /></div>
              <div class="ki-empty-state__title">{{ hosts.length === 0 ? '当前项目尚未纳管主机' : '未找到匹配的主机' }}</div>
              <p class="ki-empty-state__desc">
                {{ hosts.length === 0 ? '可扫描内网 CIDR 网段自动发现 SSH 主机，或通过 CSV / Excel 批量导入节点。' : '请调整左侧架构/故障域筛选条件或清空搜索关键字。' }}
              </p>
              <div v-if="hosts.length === 0 && auth.can('host:write', ws.currentProjectId)" class="ki-empty-state__actions">
                <el-button type="primary" size="small" @click="showAdd = true"><Plus :size="13" class="btn-ico" /> 新增纳管主机</el-button>
                <el-button size="small" @click="discoverDialog.open = true"><Radar :size="13" class="btn-ico" /> 内网网段发现</el-button>
                <el-button size="small" @click="showImport = true"><Upload :size="13" class="btn-ico" /> 批量导入</el-button>
              </div>
            </div>
          </el-tab-pane>

          <el-tab-pane :label="`主机初始化基线 (${modules.length} 模块)`" name="init">
            <div class="section-title">内置生产内核 Profile<span class="sub">按业务角色自动叠加参数并拦截互斥冲突</span></div>
            <div class="profile-list ki-mb-3">
              <span v-for="(p, name) in profiles" :key="name" class="ki-badge ki-mono profile-pill" :title="p.description">
                {{ name }}
              </span>
            </div>
            <el-table :data="modules" size="small">
              <el-table-column prop="order" label="#" width="56" />
              <el-table-column prop="name" label="模块标识" width="170">
                <template #default="{ row }"><span class="ki-mono">{{ row.name }}</span></template>
              </el-table-column>
              <el-table-column prop="description" label="功能与安全基线" />
              <el-table-column label="默认开启" width="96">
                <template #default="{ row }">
                  <KiStatusBadge :status="row.defaultOn ? 'ok' : 'pending'" :label="row.defaultOn ? '开启' : '按需'" />
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </section>
    </div>

    <!-- 内网网段扫描发现弹窗 -->
    <el-dialog v-model="discoverDialog.open" title="内网网段 SSH 主机自动发现" width="660px">
      <div class="filter-bar">
        <el-input v-model="discoverDialog.cidr" placeholder="CIDR 网段，如 192.168.2.0/24" class="ki-mono discover-cidr" />
        <el-input-number v-model="discoverDialog.port" :min="1" :max="65535" class="discover-port" />
        <el-button type="primary" :loading="discoverDialog.scanning" @click="runDiscoverSubnet">
          <Radar :size="14" class="btn-ico" /> 开始扫描
        </el-button>
      </div>
      <p class="ki-muted ki-mb-3 discover-tip">
        并发探测网段内开放目标端口的主机并抓取 SSH Banner（单次上限 1024 地址），扫描结果可一键填入纳管表单。
      </p>
      <el-table v-if="discoverDialog.items.length > 0" :data="discoverDialog.items" size="small" max-height="300">
        <el-table-column label="IP 地址" width="145">
          <template #default="{ row }"><b class="ki-mono">{{ row.address }}:{{ row.port }}</b></template>
        </el-table-column>
        <el-table-column label="延迟" width="84">
          <template #default="{ row }"><span class="ki-mono">{{ row.latencyMs }}ms</span></template>
        </el-table-column>
        <el-table-column label="SSH Banner" min-width="200">
          <template #default="{ row }"><span class="ki-mono ki-secondary">{{ row.banner || '—' }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="105" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="adoptDiscoveredHost(row)">一键纳管</el-button>
          </template>
        </el-table-column>
      </el-table>
      <KiBanner v-else-if="discoverDialog.scanned" variant="info" text="该网段未探测到开放目标端口的主机，请核对网段掩码或端口号。" />
      <template #footer>
        <el-button @click="discoverDialog.open = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 主机详情抽屉 -->
    <KiDrawer v-model="drawerOpen" :title="String(selectedHost?.name || '主机详情')" subtitle="主机资产信息 · 系统 Facts · 内核基线漂移巡检" size="md">
      <template v-if="selectedHost">
        <div class="section-title">连接与硬件档案</div>
        <div class="kv-grid ki-mb-4">
          <div class="k">主机 ID</div><div><KiMonoText :value="String(selectedHost.id || '')" /></div>
          <div class="k">运行状态</div><div><KiStatusBadge :status="String(selectedHost.state || 'pending')" /></div>
          <div class="k">SSH 地址</div><div class="ki-mono">{{ selectedHost.address }}:{{ selectedHost.sshPort || 22 }}</div>
          <div class="k">SSH 用户</div><div class="ki-mono">{{ selectedHost.sshUser || 'root' }}</div>
          <div class="k">凭据引用</div><div><KiMonoText :value="String(selectedHost.credentialId || '')" /></div>
          <div class="k">架构 / 页</div><div><KiDomainTag kind="arch" :value="String(selectedHost.arch || 'amd64')" :page-size="Number(selectedHost.pageSize || 4096)" /></div>
          <div class="k">故障域</div><div>{{ selectedHost.faultZone || '—' }}</div>
          <div class="k">基线 Profile</div><div class="ki-mono">{{ selectedHost.initProfile || 'base' }}</div>
        </div>

        <div class="section-title">内核与安全基线巡检</div>
        <div class="ki-mb-3">
          <el-button size="small" :loading="checkingBaseline" @click="checkHostBaseline(String(selectedHost.id))">
            <Activity :size="13" class="btn-ico" /> 检查基线漂移
          </el-button>
        </div>

        <KiBanner
          v-if="baselineCompliant === true"
          variant="info"
          text="巡检通过：该主机 Swap / THP / Sysctl 全部符合生产基线，0 项漂移。"
        />
        <el-table v-else-if="baselineDrifts && baselineDrifts.length > 0" :data="baselineDrifts" size="small" class="ki-mb-4">
          <el-table-column prop="category" label="类别" width="85" />
          <el-table-column prop="key" label="基线键" min-width="150">
            <template #default="{ row }"><span class="ki-mono">{{ row.key }}</span></template>
          </el-table-column>
          <el-table-column prop="expected" label="期望值" width="90" />
          <el-table-column prop="observed" label="当前值" width="90" />
          <el-table-column label="级别" width="90">
            <template #default="{ row }"><KiStatusBadge :status="row.severity" /></template>
          </el-table-column>
        </el-table>

        <template v-if="((selectedHost.facts as Record<string, unknown>)?.listeningPorts as Array<Record<string, unknown>>)?.length">
          <div class="section-title">实时监听端口与进程<span class="sub">1-RTT Facts 采集 · 用于 Plan 端口冲突预检</span></div>
          <el-table
            :data="((selectedHost.facts as Record<string, unknown>)?.listeningPorts as Array<Record<string, unknown>>)"
            size="small"
            max-height="220"
          >
            <el-table-column prop="proto" label="协议" width="70">
              <template #default="{ row }"><span class="ki-mono">{{ row.proto }}</span></template>
            </el-table-column>
            <el-table-column label="监听地址" min-width="150">
              <template #default="{ row }"><span class="ki-mono">{{ row.address }}:{{ row.port }}</span></template>
            </el-table-column>
            <el-table-column label="进程名称" min-width="120">
              <template #default="{ row }"><span class="ki-mono ki-secondary">{{ row.process || '—' }}</span></template>
            </el-table-column>
          </el-table>
        </template>
      </template>
      <template #footer>
        <el-button
          v-if="selectedHost && auth.can('host:write', ws.currentProjectId)"
          type="danger"
          plain
          @click="openDeleteHost(selectedHost)"
        >
          <Trash2 :size="13" class="btn-ico" /> 删除主机
        </el-button>
        <el-button
          v-if="selectedHost && auth.can('host:write', ws.currentProjectId)"
          @click="openInitDialog(selectedHost)"
        >
          <Wrench :size="13" class="btn-ico" /> 执行基线初始化
        </el-button>
        <el-button v-if="selectedHost && auth.can('host:probe', ws.currentProjectId)" type="primary" @click="probeHost(String(selectedHost.id))">
          采集 Facts 探活
        </el-button>
      </template>
    </KiDrawer>

    <!-- 批量导入抽屉 -->
    <KiDrawer v-model="showImport" title="批量导入主机资产" subtitle="支持 Excel (.xlsx) / CSV / YAML，提交前支持逐行 Dry-Run 字段预检" size="md">
      <div class="ki-flex-between ki-mb-3">
        <el-radio-group v-model="importState.format" size="small">
          <el-radio-button value="csv">CSV</el-radio-button>
          <el-radio-button value="yaml">YAML</el-radio-button>
          <el-radio-button value="xlsx">Excel (.xlsx)</el-radio-button>
        </el-radio-group>
        <div class="ki-flex-row">
          <el-button size="small" @click="fillSampleCSV">填入示例 CSV</el-button>
          <label class="file-upload-btn">
            <Upload :size="13" /> 选择文件
            <input type="file" accept=".xlsx,.csv,.yaml,.yml" class="hidden-file" @change="onImportFileSelected" />
          </label>
        </div>
      </div>

      <div v-if="importState.fileName" class="ki-muted ki-mono ki-mb-3">已加载文件：{{ importState.fileName }}</div>
      <el-input
        v-model="importState.data"
        type="textarea"
        :rows="7"
        class="ki-mono ki-mb-3"
        placeholder="name,address,port,user,zone,rack&#10;node-01,10.20.1.11,22,root,az-1,rack-1"
      />

      <KiBanner
        v-if="importState.previewErrors.length > 0"
        variant="danger"
        :text="`发现 ${importState.previewErrors.length} 处字段校验错误，请根据下方行号修正后重试`"
        class="ki-mb-3"
      />
      <el-table v-if="importState.previewErrors.length > 0" :data="importState.previewErrors" size="small" class="ki-mb-3">
        <el-table-column prop="rowNumber" label="行号" width="65" />
        <el-table-column prop="field" label="字段" width="100" />
        <el-table-column prop="code" label="错误码" width="130">
          <template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template>
        </el-table-column>
        <el-table-column prop="message" label="错误说明" />
      </el-table>

      <div v-if="importState.previewRows.length > 0">
        <div class="section-title">解析预览<span class="sub">共 {{ importState.previewRows.length }} 台</span></div>
        <el-table :data="importState.previewRows" size="small">
          <el-table-column prop="rowNumber" label="#" width="52" />
          <el-table-column prop="name" label="主机名" />
          <el-table-column prop="address" label="IP" />
          <el-table-column prop="zone" label="故障域" width="85" />
        </el-table>
      </div>

      <template #footer>
        <el-button @click="showImport = false">关闭</el-button>
        <el-button :loading="importState.busy" @click="runBulkImport(true)">预检 Dry-Run</el-button>
        <el-button type="primary" :loading="importState.busy" @click="runBulkImport(false)">确认批量入库</el-button>
      </template>
    </KiDrawer>

    <!-- 基线 Profile 选择弹窗 -->
    <el-dialog v-model="initDialog.open" :title="`主机基线初始化 · ${initDialog.host?.name || ''}`" width="520px">
      <p class="ki-secondary ki-mb-3">
        选择要叠加的内建内核 Profile，系统会自动校验冲突参数（如数据库与缓存不可混部）：
      </p>
      <el-checkbox-group v-model="initDialog.selectedProfiles" class="profile-check-list">
        <el-checkbox v-for="(p, name) in profiles" :key="name" :value="name">
          <b class="ki-mono">{{ name }}</b> — <span class="ki-secondary">{{ p.description }}</span>
        </el-checkbox>
      </el-checkbox-group>
      <template #footer>
        <el-button @click="initDialog.open = false">取消</el-button>
        <el-button type="primary" :loading="initDialog.submitting" @click="submitHostInit">下发初始化任务</el-button>
      </template>
    </el-dialog>

    <!-- 新增单台主机弹窗 -->
    <el-dialog v-model="showAdd" title="新增纳管主机" width="540px">
      <KiBanner
        v-if="credentials.length === 0"
        variant="warn"
        text="当前项目尚无加密凭据，请先在「平台设置 → 凭据保险库」创建 SSH 凭据。"
        class="ki-mb-3"
      />
      <el-form label-position="top">
        <el-form-item label="主机名 (DNS-1123)"><el-input v-model="newHost.name" placeholder="node-kunpeng-01" class="ki-mono" /></el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="IP / 主机地址"><el-input v-model="newHost.address" placeholder="10.20.1.11" class="ki-mono" /></el-form-item>
          <el-form-item label="SSH 端口"><el-input-number v-model="newHost.sshPort" :min="1" :max="65535" class="ki-w-full" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="SSH 用户名"><el-input v-model="newHost.sshUser" class="ki-mono" /></el-form-item>
          <el-form-item label="加密凭据引用">
            <el-select v-model="newHost.credentialId" class="ki-w-full" placeholder="选择项目凭据">
              <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
            </el-select>
          </el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="CPU 架构">
            <el-select v-model="newHost.arch" class="ki-w-full">
              <el-option label="amd64 (x86_64)" value="amd64" />
              <el-option label="arm64 (鲲鹏 / 飞腾)" value="arm64" />
            </el-select>
          </el-form-item>
          <el-form-item label="内核页大小">
            <el-select v-model="newHost.pageSize" class="ki-w-full">
              <el-option label="4096 (4K 标准页)" :value="4096" />
              <el-option label="65536 (64K 鲲鹏/麒麟V10)" :value="65536" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="故障域 / 机架"><el-input v-model="newHost.faultZone" placeholder="az-1" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAdd = false">取消</el-button>
        <el-button type="primary" @click="createHost">保存主机</el-button>
      </template>
    </el-dialog>

    <KiDangerConfirmModal
      v-model="deleteModal.open"
      :env="ws.currentEnv"
      title="确认移除纳管主机"
      :resource-name="String(deleteModal.host?.name || '')"
      risk-summary="移除后该主机将不再受平台纳管；若仍有活跃实例占用端口或目录，后端将自动拒绝删除。"
      confirm-label="解锁并移除主机"
      :loading="deleteModal.loading"
      @confirm="confirmDeleteHost"
    />
  </div>
</template>

<style scoped>
.ledger-body {
  display: flex;
  gap: var(--ki-space-4);
  align-items: flex-start;
}

.ledger-nav {
  width: 220px;
  flex: none;
  padding: 12px 10px;
}

.nav-root,
.nav-item {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 10px;
  border-radius: var(--ki-radius-sm);
  cursor: pointer;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.nav-root {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
  margin-bottom: 6px;
}

.nav-item {
  padding-left: 22px;
}

.nav-root:hover,
.nav-item:hover {
  background-color: var(--ki-bg-subtle);
}

.nav-root.is-active,
.nav-item.is-active {
  background-color: var(--ki-sider-active-bg);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.nav-root em,
.nav-item em {
  margin-left: auto;
  font-style: normal;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.nav-section {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 8px 4px;
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-muted);
}

.ledger-main {
  flex: 1;
  min-width: 0;
}

.search-kw {
  width: 290px;
}

.discover-cidr {
  width: 260px;
}

.discover-port {
  width: 120px;
}

.discover-tip {
  margin: 0;
  font-size: var(--ki-fs-xs);
}

.host-name {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.host-sub {
  font-size: 11px;
}

.btn-ico {
  margin-right: 4px;
}

.profile-list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ki-space-2);
}

.profile-pill {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.file-upload-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-control);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-primary);
  font-size: var(--ki-fs-xs);
  cursor: pointer;
}

.hidden-file {
  display: none;
}

.profile-check-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

@media (max-width: 900px) {
  .ledger-body {
    flex-direction: column;
    align-items: stretch;
  }

  .ledger-nav {
    width: auto;
  }
}
</style>
