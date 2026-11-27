<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  Activity,
  ChevronDown,
  Cpu,
  Download,
  Edit3,
  FileSpreadsheet,
  FolderTree,
  Layers,
  Plus,
  Radar,
  Search,
  Server,
  ShieldCheck,
  Tags,
  Trash2,
  Upload,
  Wrench,
} from 'lucide-vue-next'
import { apiDownload, apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'
import CmdbImportDrawer from '@/components/cmdb/CmdbImportDrawer.vue'
import CmdbDynamicGroupsPanel, { type DynamicGroupItem } from '@/components/cmdb/CmdbDynamicGroupsPanel.vue'
import CmdbSyncSourcesPanel, { type CMDBSyncSourceItem } from '@/components/cmdb/CmdbSyncSourcesPanel.vue'
import CmdbCiImpactPanel, {
  type HostImpactSummary,
  type ProcessInventoryItem,
} from '@/components/cmdb/CmdbCiImpactPanel.vue'

interface TopologyTreeNode {
  key: string
  label: string
  level: string
  count: number
  hostIds: string[]
  children?: TopologyTreeNode[]
}

interface CMDBOverview {
  totalHosts: number
  readyHosts: number
  probedHosts: number
  totalInstances: number
  totalStacks: number
  shadowPorts: number
  bizTree: TopologyTreeNode[]
  physicalTree: TopologyTreeNode[]
  hostImpacts: HostImpactSummary[]
  processes: ProcessInventoryItem[]
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

interface HostRecord {
  id: string
  name: string
  address: string
  sshPort?: number
  sshUser?: string
  credentialId?: string
  state?: string
  faultZone?: string
  faultRack?: string
  arch?: string
  pageSize?: number
  initProfile?: string
  osId?: string
  osVersion?: string
  labels?: Record<string, string>
  facts?: Record<string, unknown>
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()

const loading = ref(false)
const hosts = ref<HostRecord[]>([])
const credentials = ref<Array<{ id: string; name: string; kind: string }>>([])
const profiles = ref<Record<string, { name: string; description: string }>>({})
const modules = ref<Array<Record<string, unknown>>>([])
const overview = ref<CMDBOverview | null>(null)
const dynamicGroups = ref<DynamicGroupItem[]>([])
const syncSources = ref<CMDBSyncSourceItem[]>([])

const activeTab = ref('hosts')
const navMode = ref<'biz' | 'physical' | 'arch'>('biz')
const activeTreeKey = ref('ALL')
const activeTreeHostIds = ref<Set<string> | null>(null)
const activeGroupId = ref('')
const filterArch = ref<string>('ALL')
const keyword = ref('')

const showAdd = ref(false)
const showImport = ref(false)
const selectedHosts = ref<HostRecord[]>([])
const selectedHost = ref<HostRecord | null>(null)
const selectedHostImpact = ref<HostImpactSummary | null>(null)
const drawerOpen = ref(false)
const drawerTab = ref('profile')
const checkingBaseline = ref(false)
const baselineDrifts = ref<DriftFinding[] | null>(null)
const baselineCompliant = ref<boolean | null>(null)
const batchBusy = ref(false)

const newHost = reactive({
  name: '',
  address: '',
  sshPort: 22,
  sshUser: 'root',
  credentialId: '',
  useInlineCred: false,
  inlineCredName: '',
  inlineCredKind: 'ssh_password' as 'ssh_password' | 'ssh_key',
  inlineCredSecret: '',
  faultZone: 'az-1',
  faultRack: '',
  arch: 'amd64',
  pageSize: 4096,
  biz: '',
  set: '',
  module: '',
  region: '',
  securityArea: '',
  owner: '',
  assetTag: '',
})

const editHostForm = reactive({
  editing: false,
  saving: false,
  address: '',
  sshPort: 22,
  sshUser: 'root',
  credentialId: '',
  state: 'ready',
  arch: 'amd64',
  pageSize: 4096,
  faultZone: '',
  faultRack: '',
  biz: '',
  set: '',
  module: '',
  region: '',
  securityArea: '',
  owner: '',
  assetTag: '',
})

const batchTopoDialog = reactive({
  open: false,
  biz: '',
  set: '',
  module: '',
  region: '',
  securityArea: '',
  faultZone: '',
  faultRack: '',
  owner: '',
})

const batchCredDialog = reactive({
  open: false,
  credentialId: '',
})

const discoverDialog = reactive({
  open: false,
  cidr: '192.168.2.0/24',
  port: 22,
  scanning: false,
  scanned: false,
  items: [] as DiscoveredHost[],
})

const initDialog = reactive({
  open: false,
  hostIds: [] as string[],
  titleName: '',
  selectedProfiles: ['base'] as string[],
  submitting: false,
})

const deleteModal = reactive({
  open: false,
  host: null as HostRecord | null,
  batchIds: [] as string[],
  loading: false,
})

const impactByHostId = computed(() => {
  const m = new Map<string, HostImpactSummary>()
  for (const imp of overview.value?.hostImpacts || []) {
    m.set(imp.hostId, imp)
  }
  return m
})

const filteredHosts = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return hosts.value.filter((h) => {
    if (activeTreeHostIds.value && !activeTreeHostIds.value.has(h.id)) return false
    if (filterArch.value === '64k' && Number(h.pageSize || 4096) < 65536) return false
    if (filterArch.value === 'amd64' && String(h.arch || 'amd64') !== 'amd64') return false
    if (filterArch.value === 'arm64' && String(h.arch || '') !== 'arm64') return false
    if (!q) return true
    const lbl = h.labels || {}
    const hay = `${h.name} ${h.address} ${h.id} ${h.faultZone || ''} ${lbl['cmdb.biz'] || ''} ${lbl['cmdb.set'] || ''} ${lbl['cmdb.module'] || ''} ${lbl['cmdb.region'] || ''} ${lbl['cmdb.security_area'] || ''} ${lbl['cmdb.asset_tag'] || ''}`
    return hay.toLowerCase().includes(q)
  })
})

const arm64kTotal = computed(() => hosts.value.filter((h) => Number(h.pageSize || 4096) >= 65536).length)
const readyTotal = computed(() =>
  hosts.value.filter((h) => ['ready', 'active', 'healthy', 'online'].includes(String(h.state || '').toLowerCase())).length
)
const activeFilterBadge = computed(() => {
  if (activeGroupId.value) {
    const g = dynamicGroups.value.find((x) => x.id === activeGroupId.value)
    return g ? `动态分组：${g.name}` : ''
  }
  if (activeTreeKey.value !== 'ALL') {
    return `拓扑筛选：${activeTreeKey.value.replace(/^[a-z_]+:/, '').replace(/\/[a-z_]+:/g, ' / ')}`
  }
  if (filterArch.value !== 'ALL') return `架构筛选：${filterArch.value}`
  return ''
})

function hostLabel(h: HostRecord, key: string): string {
  return h.labels?.[key] || ''
}

function clearAllFilters() {
  activeTreeKey.value = 'ALL'
  activeTreeHostIds.value = null
  activeGroupId.value = ''
  filterArch.value = 'ALL'
}

function selectTreeNode(node: TopologyTreeNode) {
  activeGroupId.value = ''
  filterArch.value = 'ALL'
  activeTreeKey.value = node.key
  activeTreeHostIds.value = new Set(node.hostIds || [])
  activeTab.value = 'hosts'
}

function selectDynamicGroup(g: DynamicGroupItem) {
  activeTreeKey.value = 'ALL'
  filterArch.value = 'ALL'
  activeGroupId.value = g.id
  activeTreeHostIds.value = new Set(g.matchedHostIds || [])
  activeTab.value = 'hosts'
}

function selectArchFilter(arch: string) {
  activeTreeKey.value = 'ALL'
  activeTreeHostIds.value = null
  activeGroupId.value = ''
  filterArch.value = arch
  activeTab.value = 'hosts'
}

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [hRes, pRes, cRes, ovRes, grpRes, syncRes] = await Promise.all([
      apiRequest<{ items: HostRecord[] }>('GET', `/api/v1/projects/${pid}/hosts`),
      apiRequest<{ profiles: Record<string, { name: string; description: string }>; modules: Array<Record<string, unknown>> }>(
        'GET',
        '/api/v1/hostinit/profiles'
      ),
      apiRequest<{ items: Array<{ id: string; name: string; kind: string }> }>('GET', `/api/v1/projects/${pid}/credentials`).catch(
        () => ({ data: { items: [] } })
      ),
      apiRequest<CMDBOverview>('GET', `/api/v1/projects/${pid}/cmdb/overview`).catch(() => ({ data: null as unknown as CMDBOverview })),
      apiRequest<{ items: DynamicGroupItem[] }>('GET', `/api/v1/projects/${pid}/cmdb/groups`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: CMDBSyncSourceItem[] }>('GET', `/api/v1/projects/${pid}/cmdb/sync-sources`).catch(() => ({ data: { items: [] } })),
    ])
    hosts.value = hRes.data.items || []
    profiles.value = pRes.data.profiles || {}
    modules.value = pRes.data.modules || []
    credentials.value = cRes.data.items || []
    overview.value = ovRes.data || null
    dynamicGroups.value = grpRes.data.items || []
    syncSources.value = syncRes.data.items || []
    if (!newHost.credentialId && credentials.value.length > 0) {
      newHost.credentialId = credentials.value[0].id
    }
    if (selectedHost.value) {
      const refreshed = hosts.value.find((x) => x.id === selectedHost.value?.id)
      if (refreshed) selectedHost.value = refreshed
    }
  } finally {
    loading.value = false
  }
}

async function downloadTemplate(fmt: 'xlsx' | 'csv') {
  try {
    const res = await apiDownload(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/hosts:template?format=${fmt}`,
      `kinstall-cmdb-hosts-template.${fmt}`
    )
    ElMessage.success(`已下载导入模板 ${res.filename}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function exportLedger(fmt: 'xlsx' | 'csv') {
  try {
    const res = await apiDownload(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/hosts:export?format=${fmt}`,
      `kinstall-cmdb-hosts.${fmt}`
    )
    ElMessage.success(`已导出 CMDB 资产台账 ${res.filename}`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function createHost() {
  if (!newHost.name.trim() || !newHost.address.trim()) {
    ElMessage.warning('请填写主机名与内网 IP 地址')
    return
  }
  const labels: Record<string, string> = {}
  if (newHost.biz.trim()) labels['cmdb.biz'] = newHost.biz.trim()
  if (newHost.set.trim()) labels['cmdb.set'] = newHost.set.trim()
  if (newHost.module.trim()) labels['cmdb.module'] = newHost.module.trim()
  if (newHost.region.trim()) labels['cmdb.region'] = newHost.region.trim()
  if (newHost.securityArea.trim()) labels['cmdb.security_area'] = newHost.securityArea.trim()
  if (newHost.owner.trim()) labels['cmdb.owner'] = newHost.owner.trim()
  if (newHost.assetTag.trim()) labels['cmdb.asset_tag'] = newHost.assetTag.trim()

  const payload: Record<string, unknown> = {
    name: newHost.name.trim(),
    address: newHost.address.trim(),
    sshPort: newHost.sshPort,
    sshUser: newHost.sshUser.trim() || 'root',
    credentialId: newHost.useInlineCred ? '' : newHost.credentialId,
    faultZone: newHost.faultZone,
    faultRack: newHost.faultRack,
    arch: newHost.arch,
    pageSize: newHost.pageSize,
    labels,
  }
  if (newHost.useInlineCred) {
    if (!newHost.inlineCredSecret.trim()) {
      ElMessage.warning('请输入 SSH 密码或私钥内容')
      return
    }
    payload.inlineCredential = {
      name: newHost.inlineCredName.trim() || `ssh-${newHost.name.trim()}`,
      kind: newHost.inlineCredKind,
      plaintext: newHost.inlineCredSecret,
    }
  }
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/hosts`, {
      body: payload,
      idempotent: true,
    })
    ElMessage.success('主机已纳管至 CMDB 资产库')
    showAdd.value = false
    newHost.name = ''
    newHost.address = ''
    newHost.inlineCredSecret = ''
    newHost.useInlineCred = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function runDiscoverSubnet() {
  if (!discoverDialog.cidr.trim()) {
    ElMessage.warning('请输入要扫描的 CIDR 网段，例如 192.168.2.0/24')
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
    ElMessage.success('主机 Facts 与监听端口采集完成')
    await loadAll()
    if (selectedHost.value?.id === hostId) {
      await loadSelectedHostImpact(hostId)
    }
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

async function loadSelectedHostImpact(hostId: string) {
  selectedHostImpact.value = null
  try {
    const { data } = await apiRequest<HostImpactSummary>(
      'GET',
      `/api/v1/projects/${ws.currentProjectId}/hosts/${hostId}/impact`
    )
    selectedHostImpact.value = data
  } catch {
    selectedHostImpact.value = impactByHostId.value.get(hostId) || null
  }
}

function fillEditFormFromHost(h: HostRecord) {
  editHostForm.address = h.address || ''
  editHostForm.sshPort = h.sshPort || 22
  editHostForm.sshUser = h.sshUser || 'root'
  editHostForm.credentialId = h.credentialId || ''
  editHostForm.state = h.state || 'ready'
  editHostForm.arch = h.arch || 'amd64'
  editHostForm.pageSize = h.pageSize || 4096
  editHostForm.faultZone = h.faultZone || ''
  editHostForm.faultRack = h.faultRack || ''
  editHostForm.biz = hostLabel(h, 'cmdb.biz')
  editHostForm.set = hostLabel(h, 'cmdb.set')
  editHostForm.module = hostLabel(h, 'cmdb.module')
  editHostForm.region = hostLabel(h, 'cmdb.region')
  editHostForm.securityArea = hostLabel(h, 'cmdb.security_area')
  editHostForm.owner = hostLabel(h, 'cmdb.owner')
  editHostForm.assetTag = hostLabel(h, 'cmdb.asset_tag')
}

function openHostDetail(row: HostRecord, initialTab = 'profile') {
  selectedHost.value = row
  baselineDrifts.value = null
  baselineCompliant.value = null
  editHostForm.editing = false
  fillEditFormFromHost(row)
  drawerTab.value = initialTab
  drawerOpen.value = true
  void loadSelectedHostImpact(row.id)
}

function openHostById(hostId: string) {
  const h = hosts.value.find((x) => x.id === hostId)
  if (h) openHostDetail(h, 'impact')
}

async function saveHostEdit() {
  if (!selectedHost.value) return
  editHostForm.saving = true
  try {
    await apiRequest('PUT', `/api/v1/projects/${ws.currentProjectId}/hosts/${selectedHost.value.id}`, {
      body: {
        address: editHostForm.address,
        sshPort: editHostForm.sshPort,
        sshUser: editHostForm.sshUser,
        credentialId: editHostForm.credentialId || undefined,
        state: editHostForm.state,
        arch: editHostForm.arch,
        pageSize: editHostForm.pageSize,
        faultZone: editHostForm.faultZone,
        faultRack: editHostForm.faultRack,
        biz: editHostForm.biz,
        set: editHostForm.set,
        module: editHostForm.module,
        region: editHostForm.region,
        securityArea: editHostForm.securityArea,
        owner: editHostForm.owner,
        assetTag: editHostForm.assetTag,
      },
    })
    ElMessage.success('CMDB 主机档案与拓扑归属已更新')
    editHostForm.editing = false
    await loadAll()
    await loadSelectedHostImpact(selectedHost.value.id)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    editHostForm.saving = false
  }
}

function openInitDialog(row: HostRecord) {
  initDialog.hostIds = [row.id]
  initDialog.titleName = row.name
  const cur = String(row.initProfile || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  initDialog.selectedProfiles = cur.length > 0 ? cur : ['base']
  initDialog.open = true
}

function openBatchInitDialog() {
  if (selectedHosts.value.length === 0) return
  initDialog.hostIds = selectedHosts.value.map((h) => h.id)
  initDialog.titleName = `批量 ${selectedHosts.value.length} 台主机`
  initDialog.selectedProfiles = ['base']
  initDialog.open = true
}

async function submitHostInit() {
  if (initDialog.hostIds.length === 0) return
  initDialog.submitting = true
  try {
    if (initDialog.hostIds.length === 1) {
      const { data } = await apiRequest<{ id: string }>(
        'POST',
        `/api/v1/projects/${ws.currentProjectId}/hosts/${initDialog.hostIds[0]}:init`,
        { body: { profiles: initDialog.selectedProfiles }, idempotent: true }
      )
      initDialog.open = false
      ElMessage.success(`主机基线初始化任务 ${data.id} 已下发`)
      await router.push(`/operations?id=${data.id}`)
    } else {
      const { data } = await apiRequest<{ succeeded: string[]; failed: Record<string, string>; operationIds?: string[] }>(
        'POST',
        `/api/v1/projects/${ws.currentProjectId}/hosts:batch`,
        { body: { hostIds: initDialog.hostIds, action: 'init', profiles: initDialog.selectedProfiles } }
      )
      initDialog.open = false
      ElMessage.success(`已为 ${data.succeeded.length} 台主机下发基线初始化任务`)
      await loadAll()
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    initDialog.submitting = false
  }
}

async function runBatchAction(action: string, extra?: Record<string, unknown>) {
  if (selectedHosts.value.length === 0) return
  batchBusy.value = true
  try {
    const { data } = await apiRequest<{ succeeded: string[]; failed: Record<string, string> }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/hosts:batch`,
      { body: { hostIds: selectedHosts.value.map((h) => h.id), action, ...(extra || {}) } }
    )
    const failCount = Object.keys(data.failed || {}).length
    if (failCount === 0) {
      ElMessage.success(`批量操作完成：成功 ${data.succeeded.length} 台`)
    } else {
      ElMessage.warning(`批量操作部分完成：成功 ${data.succeeded.length} 台，失败 ${failCount} 台`)
    }
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    batchBusy.value = false
  }
}

async function submitBatchTopology() {
  const payload: Record<string, unknown> = {}
  if (batchTopoDialog.biz.trim()) payload.biz = batchTopoDialog.biz.trim()
  if (batchTopoDialog.set.trim()) payload.set = batchTopoDialog.set.trim()
  if (batchTopoDialog.module.trim()) payload.module = batchTopoDialog.module.trim()
  if (batchTopoDialog.region.trim()) payload.region = batchTopoDialog.region.trim()
  if (batchTopoDialog.securityArea.trim()) payload.securityArea = batchTopoDialog.securityArea.trim()
  if (batchTopoDialog.faultZone.trim()) payload.faultZone = batchTopoDialog.faultZone.trim()
  if (batchTopoDialog.faultRack.trim()) payload.faultRack = batchTopoDialog.faultRack.trim()
  if (batchTopoDialog.owner.trim()) payload.owner = batchTopoDialog.owner.trim()
  batchTopoDialog.open = false
  await runBatchAction('update_topology', payload)
}

async function submitBatchCredential() {
  if (!batchCredDialog.credentialId) {
    ElMessage.warning('请选择目标凭据')
    return
  }
  batchCredDialog.open = false
  await runBatchAction('bind_credential', { credentialId: batchCredDialog.credentialId })
}

function openDeleteHost(row: HostRecord) {
  deleteModal.host = row
  deleteModal.batchIds = []
  deleteModal.open = true
}

function openBatchDelete() {
  if (selectedHosts.value.length === 0) return
  deleteModal.host = null
  deleteModal.batchIds = selectedHosts.value.map((h) => h.id)
  deleteModal.open = true
}

async function confirmDeleteHost() {
  deleteModal.loading = true
  try {
    if (deleteModal.batchIds.length > 0) {
      await runBatchAction('delete')
      deleteModal.open = false
    } else if (deleteModal.host) {
      await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/hosts/${deleteModal.host.id}`)
      ElMessage.success(`主机 ${deleteModal.host.name} 已移除`)
      deleteModal.open = false
      drawerOpen.value = false
      await loadAll()
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    deleteModal.loading = false
  }
}

function onSelectionChange(rows: HostRecord[]) {
  selectedHosts.value = rows
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div class="hosts-page">
    <PageHeader
      title="CMDB 资产中心"
      subtitle="三级业务/物理拓扑 · 模板导入导出 · 动态分组 · 外部同步"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button v-if="auth.can('host:write', ws.currentProjectId)" type="primary" @click="showImport = true">
          <Upload :size="14" class="btn-ico" /> 批量导入
        </el-button>
        <el-dropdown trigger="click" @command="(fmt: 'xlsx' | 'csv') => downloadTemplate(fmt)">
          <el-button>
            <FileSpreadsheet :size="14" class="btn-ico" /> 下载模板 <ChevronDown :size="13" class="btn-ico-r" />
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="xlsx">Excel 标准导入模板 (.xlsx)</el-dropdown-item>
              <el-dropdown-item command="csv">CSV 导入模板 (UTF-8 BOM)</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown trigger="click" @command="(fmt: 'xlsx' | 'csv') => exportLedger(fmt)">
          <el-button>
            <Download :size="14" class="btn-ico" /> 导出台账 <ChevronDown :size="13" class="btn-ico-r" />
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="xlsx">导出 CMDB Excel 台账 (.xlsx)</el-dropdown-item>
              <el-dropdown-item command="csv">导出 CSV 台账 (.csv)</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button v-if="auth.can('host:probe', ws.currentProjectId)" @click="discoverDialog.open = true">
          <Radar :size="14" class="btn-ico" /> 网段发现
        </el-button>
        <el-button v-if="auth.can('host:write', ws.currentProjectId)" @click="showAdd = true">
          <Plus :size="14" class="btn-ico" /> 新增主机
        </el-button>
      </template>
    </PageHeader>

    <!-- 顶部可点击指标条 -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': !activeFilterBadge }" @click="clearAllFilters">
        <span class="ki-chip__bar" />
        <div>
          <div class="ki-chip__label">纳管主机总计</div>
          <div class="ki-chip__val ki-num">{{ hosts.length }}</div>
        </div>
      </div>
      <div class="ki-chip" @click="activeTab = 'hosts'">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div>
          <div class="ki-chip__label">就绪 / 服役节点</div>
          <div class="ki-chip__val ki-num">{{ readyTotal }} / {{ hosts.length }}</div>
        </div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterArch === '64k' }" @click="selectArchFilter('64k')">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div>
          <div class="ki-chip__label">ARM64 · 64K 页节点</div>
          <div class="ki-chip__val ki-num">{{ arm64kTotal }}</div>
        </div>
      </div>
      <div class="ki-chip" @click="activeTab = 'ci'">
        <span class="ki-chip__bar ki-chip__bar--running" />
        <div>
          <div class="ki-chip__label">动态分组 / 影子端口</div>
          <div class="ki-chip__val ki-num">{{ dynamicGroups.length }} / {{ overview?.shadowPorts || 0 }}</div>
        </div>
      </div>
    </div>

    <!-- 左右结构：左侧 CMDB 业务/物理拓扑树 + 动态分组 | 右侧资产主工作区 -->
    <div class="ledger-body">
      <aside class="ki-card ledger-nav">
        <div class="nav-mode-tabs">
          <button type="button" class="nav-mode-btn" :class="{ 'is-active': navMode === 'biz' }" @click="navMode = 'biz'">
            业务拓扑
          </button>
          <button
            type="button"
            class="nav-mode-btn"
            :class="{ 'is-active': navMode === 'physical' }"
            @click="navMode = 'physical'"
          >
            物理安全域
          </button>
          <button type="button" class="nav-mode-btn" :class="{ 'is-active': navMode === 'arch' }" @click="navMode = 'arch'">
            架构
          </button>
        </div>

        <div class="nav-root" :class="{ 'is-active': !activeFilterBadge }" @click="clearAllFilters">
          <Server :size="14" />
          <span>全部 CMDB 节点</span>
          <em class="ki-num">{{ hosts.length }}</em>
        </div>

        <!-- 业务拓扑树：业务 → 集群 → 模块 -->
        <template v-if="navMode === 'biz'">
          <div class="nav-section"><FolderTree :size="13" /> 业务 → 集群 → 模块</div>
          <template v-if="overview?.bizTree?.length">
            <div v-for="biz in overview.bizTree" :key="biz.key" class="tree-group">
              <div class="nav-item nav-lvl-1" :class="{ 'is-active': activeTreeKey === biz.key }" @click="selectTreeNode(biz)">
                <span class="ki-truncate" :title="biz.label">{{ biz.label }}</span>
                <em class="ki-num">{{ biz.count }}</em>
              </div>
              <template v-for="setNode in biz.children || []" :key="setNode.key">
                <div
                  class="nav-item nav-lvl-2"
                  :class="{ 'is-active': activeTreeKey === setNode.key }"
                  @click="selectTreeNode(setNode)"
                >
                  <span class="ki-truncate" :title="setNode.label">{{ setNode.label }}</span>
                  <em class="ki-num">{{ setNode.count }}</em>
                </div>
                <div
                  v-for="mod in setNode.children || []"
                  :key="mod.key"
                  class="nav-item nav-lvl-3"
                  :class="{ 'is-active': activeTreeKey === mod.key }"
                  @click="selectTreeNode(mod)"
                >
                  <span class="ki-truncate" :title="mod.label">{{ mod.label }}</span>
                  <em class="ki-num">{{ mod.count }}</em>
                </div>
              </template>
            </div>
          </template>
          <div v-else class="nav-empty">导入主机或编辑业务归属后自动生成三级拓扑</div>
        </template>

        <!-- 物理安全域树：区域机房 → 安全域 → 故障域 -->
        <template v-else-if="navMode === 'physical'">
          <div class="nav-section"><ShieldCheck :size="13" /> 机房 → 安全域 → 故障域</div>
          <template v-if="overview?.physicalTree?.length">
            <div v-for="reg in overview.physicalTree" :key="reg.key" class="tree-group">
              <div class="nav-item nav-lvl-1" :class="{ 'is-active': activeTreeKey === reg.key }" @click="selectTreeNode(reg)">
                <span class="ki-truncate" :title="reg.label">{{ reg.label }}</span>
                <em class="ki-num">{{ reg.count }}</em>
              </div>
              <template v-for="sec in reg.children || []" :key="sec.key">
                <div
                  class="nav-item nav-lvl-2"
                  :class="{ 'is-active': activeTreeKey === sec.key }"
                  @click="selectTreeNode(sec)"
                >
                  <span class="ki-truncate" :title="sec.label">{{ sec.label }}</span>
                  <em class="ki-num">{{ sec.count }}</em>
                </div>
                <div
                  v-for="az in sec.children || []"
                  :key="az.key"
                  class="nav-item nav-lvl-3"
                  :class="{ 'is-active': activeTreeKey === az.key }"
                  @click="selectTreeNode(az)"
                >
                  <span class="ki-mono ki-truncate" :title="az.label">{{ az.label }}</span>
                  <em class="ki-num">{{ az.count }}</em>
                </div>
              </template>
            </div>
          </template>
          <div v-else class="nav-empty">暂无物理机房维度数据</div>
        </template>

        <!-- 架构视图 -->
        <template v-else>
          <div class="nav-section"><Cpu :size="13" /> CPU 架构与页大小</div>
          <div class="nav-item nav-lvl-1" :class="{ 'is-active': filterArch === 'amd64' }" @click="selectArchFilter('amd64')">
            <span>amd64 · 4K</span>
            <em class="ki-num">{{ hosts.filter((h) => String(h.arch || 'amd64') === 'amd64').length }}</em>
          </div>
          <div class="nav-item nav-lvl-1" :class="{ 'is-active': filterArch === 'arm64' }" @click="selectArchFilter('arm64')">
            <span>arm64 (鲲鹏 / 飞腾)</span>
            <em class="ki-num">{{ hosts.filter((h) => String(h.arch || '') === 'arm64').length }}</em>
          </div>
          <div class="nav-item nav-lvl-1" :class="{ 'is-active': filterArch === '64k' }" @click="selectArchFilter('64k')">
            <span>arm64 · 64K 页</span>
            <em class="ki-num">{{ arm64kTotal }}</em>
          </div>
        </template>

        <!-- 动态分组快捷列表 -->
        <div class="nav-section"><Layers :size="13" /> 动态分组</div>
        <template v-if="dynamicGroups.length > 0">
          <div
            v-for="g in dynamicGroups"
            :key="g.id"
            class="nav-item nav-lvl-1"
            :class="{ 'is-active': activeGroupId === g.id }"
            @click="selectDynamicGroup(g)"
          >
            <span class="ki-truncate" :title="g.name">{{ g.name }}</span>
            <em class="ki-num">{{ g.matchedCount }}</em>
          </div>
        </template>
        <div v-else class="nav-empty" @click="activeTab = 'groups'">点击「动态分组」页签按规则圈选主机</div>
      </aside>

      <section class="ki-card ledger-main">
        <el-tabs v-model="activeTab">
          <!-- Tab 1: CMDB 主机台账 -->
          <el-tab-pane :label="`主机台账 (${filteredHosts.length})`" name="hosts">
            <div class="filter-bar ki-mb-3">
              <el-input v-model="keyword" clearable placeholder="搜索主机名 / IP / 业务 / 模块 / 机房 / 资产编号" class="search-kw">
                <template #prefix><Search :size="14" /></template>
              </el-input>
              <div v-if="activeFilterBadge" class="active-filter-chip">
                <span>{{ activeFilterBadge }}</span>
                <button type="button" class="clear-chip-btn" @click="clearAllFilters">清除</button>
              </div>
            </div>

            <!-- 批量运维操作栏 -->
            <div v-if="selectedHosts.length > 0 && auth.can('host:write', ws.currentProjectId)" class="batch-bar ki-mb-3">
              <span class="batch-bar__count">已勾选 <b class="ki-num">{{ selectedHosts.length }}</b> 台主机</span>
              <div class="ki-flex-row ki-flex-wrap">
                <el-button size="small" :loading="batchBusy" @click="batchTopoDialog.open = true">
                  <Tags :size="13" class="btn-ico" /> 批量设业务拓扑
                </el-button>
                <el-button size="small" :loading="batchBusy" @click="batchCredDialog.open = true">批量改绑凭据</el-button>
                <el-dropdown trigger="click" @command="(st: string) => runBatchAction('set_state', { state: st })">
                  <el-button size="small" :loading="batchBusy">
                    切换生命周期 <ChevronDown :size="12" class="btn-ico-r" />
                  </el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="ready">设为就绪 (ready)</el-dropdown-item>
                      <el-dropdown-item command="active">设为在线服役 (active)</el-dropdown-item>
                      <el-dropdown-item command="draining">设为维护排空 (draining)</el-dropdown-item>
                      <el-dropdown-item command="retired">设为下线回收 (retired)</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
                <el-button size="small" :loading="batchBusy" @click="runBatchAction('probe')">批量探活</el-button>
                <el-button size="small" :loading="batchBusy" @click="openBatchInitDialog">批量初始化</el-button>
                <el-button size="small" type="danger" plain :loading="batchBusy" @click="openBatchDelete">批量删除</el-button>
              </div>
            </div>

            <el-table
              v-if="filteredHosts.length > 0 || loading"
              v-loading="loading"
              :data="filteredHosts"
              @selection-change="onSelectionChange"
              @row-click="(row: HostRecord) => openHostDetail(row)"
            >
              <el-table-column type="selection" width="42" />
              <el-table-column label="主机名称 / 资产号" min-width="165">
                <template #default="{ row }">
                  <div class="host-name ki-mono">{{ row.name }}</div>
                  <div class="ki-muted ki-mono host-sub">{{ hostLabel(row, 'cmdb.asset_tag') || row.id }}</div>
                </template>
              </el-table-column>
              <el-table-column label="SSH 端点" min-width="155">
                <template #default="{ row }">
                  <span class="ki-mono">{{ row.sshUser || 'root' }}@{{ row.address }}:{{ row.sshPort || 22 }}</span>
                </template>
              </el-table-column>
              <el-table-column label="业务拓扑 (业务 / 集群 / 模块)" min-width="180">
                <template #default="{ row }">
                  <div class="topo-main">{{ hostLabel(row, 'cmdb.biz') || '未归类业务' }}</div>
                  <div class="ki-muted host-sub">
                    {{ [hostLabel(row, 'cmdb.set'), hostLabel(row, 'cmdb.module')].filter(Boolean).join(' / ') || '空闲池' }}
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="机房 / 安全域 / 故障域" min-width="145">
                <template #default="{ row }">
                  <div>{{ hostLabel(row, 'cmdb.region') || '默认机房' }}</div>
                  <div class="ki-muted host-sub">
                    {{ [hostLabel(row, 'cmdb.security_area') || '内网区', row.faultZone || 'default'].join(' · ') }}
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="架构 / 页" width="128">
                <template #default="{ row }">
                  <KiDomainTag kind="arch" :value="String(row.arch || 'amd64')" :page-size="Number(row.pageSize || 4096)" />
                </template>
              </el-table-column>
              <el-table-column label="承载实例" width="84">
                <template #default="{ row }">
                  <span class="ki-num">{{ impactByHostId.get(row.id)?.instanceCount || 0 }} 个</span>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="102">
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
              <div class="ki-empty-state__title">{{ hosts.length === 0 ? '当前项目尚未纳管主机资产' : '未找到匹配的主机' }}</div>
              <p class="ki-empty-state__desc">
                {{
                  hosts.length === 0
                    ? '可通过上方「批量导入」上传 Excel/CSV/LiveOps/Ansible 清单，或点击「网段发现」与「新增主机」快速接入。'
                    : '请清除左侧拓扑/动态分组筛选或调整搜索关键字。'
                }}
              </p>
              <div v-if="hosts.length === 0 && auth.can('host:write', ws.currentProjectId)" class="ki-empty-state__actions">
                <el-button type="primary" @click="showImport = true">
                  <Upload :size="14" class="btn-ico" /> 立即导入主机
                </el-button>
                <el-button @click="activeTab = 'sync'">配置外部 CMDB 同步</el-button>
              </div>
            </div>
          </el-tab-pane>

          <!-- Tab 2: 动态分组 -->
          <el-tab-pane :label="`动态分组 (${dynamicGroups.length})`" name="groups">
            <CmdbDynamicGroupsPanel
              :project-id="ws.currentProjectId"
              :can-write="auth.can('host:write', ws.currentProjectId)"
              :groups="dynamicGroups"
              @refresh="loadAll"
              @select-group="selectDynamicGroup"
            />
          </el-tab-pane>

          <!-- Tab 3: CI 影响面与全网进程端口 -->
          <el-tab-pane :label="`CI 影响面与进程 (${overview?.processes?.length || 0})`" name="ci">
            <CmdbCiImpactPanel
              :impacts="overview?.hostImpacts || []"
              :processes="overview?.processes || []"
              @open-host="openHostById"
            />
          </el-tab-pane>

          <!-- Tab 4: 外部 CMDB 同步 -->
          <el-tab-pane :label="`外部 CMDB 同步 (${syncSources.length})`" name="sync">
            <CmdbSyncSourcesPanel
              :project-id="ws.currentProjectId"
              :can-write="auth.can('host:write', ws.currentProjectId)"
              :sources="syncSources"
              :credentials="credentials"
              @refresh="loadAll"
            />
          </el-tab-pane>

          <!-- Tab 5: 主机初始化基线 (22 模块) -->
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

    <!-- 批量导入抽屉 (全能导入 + 模板下载 + 快捷建凭据 + 列映射) -->
    <CmdbImportDrawer
      v-model="showImport"
      :project-id="ws.currentProjectId"
      :credentials="credentials"
      @imported="loadAll"
    />

    <!-- 内网网段扫描发现弹窗 -->
    <el-dialog v-model="discoverDialog.open" title="内网网段 SSH 主机自动发现" width="660px">
      <div class="filter-bar ki-mb-3">
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

    <!-- 主机详情与 CI 影响面抽屉 -->
    <KiDrawer
      v-model="drawerOpen"
      :title="String(selectedHost?.name || '主机详情')"
      subtitle="CMDB 资产档案 · CI 影响面分析 · 监听端口与影子服务 · 内核基线漂移巡检"
      size="lg"
    >
      <template v-if="selectedHost">
        <el-tabs v-model="drawerTab">
          <el-tab-pane label="档案与编辑" name="profile">
            <div class="ki-flex-between ki-mb-3">
              <div class="section-title">资产档案与拓扑归属</div>
              <el-button
                v-if="auth.can('host:write', ws.currentProjectId)"
                size="small"
                @click="editHostForm.editing = !editHostForm.editing"
              >
                <Edit3 :size="13" class="btn-ico" /> {{ editHostForm.editing ? '取消编辑' : '编辑属性' }}
              </el-button>
            </div>

            <template v-if="!editHostForm.editing">
              <div class="kv-grid ki-mb-4">
                <div class="k">主机 ID</div><div><KiMonoText :value="String(selectedHost.id || '')" /></div>
                <div class="k">生命周期状态</div><div><KiStatusBadge :status="String(selectedHost.state || 'pending')" /></div>
                <div class="k">SSH 端点</div><div class="ki-mono">{{ selectedHost.sshUser || 'root' }}@{{ selectedHost.address }}:{{ selectedHost.sshPort || 22 }}</div>
                <div class="k">凭据引用</div><div><KiMonoText :value="String(selectedHost.credentialId || '')" /></div>
                <div class="k">业务拓扑</div><div>{{ [hostLabel(selectedHost, 'cmdb.biz'), hostLabel(selectedHost, 'cmdb.set'), hostLabel(selectedHost, 'cmdb.module')].filter(Boolean).join(' / ') || '未归类' }}</div>
                <div class="k">机房 / 安全域</div><div>{{ [hostLabel(selectedHost, 'cmdb.region') || '默认机房', hostLabel(selectedHost, 'cmdb.security_area') || '内网区'].join(' · ') }}</div>
                <div class="k">故障域 / 机架</div><div>{{ selectedHost.faultZone || '—' }} / {{ selectedHost.faultRack || '—' }}</div>
                <div class="k">架构 / 页大小</div><div><KiDomainTag kind="arch" :value="String(selectedHost.arch || 'amd64')" :page-size="Number(selectedHost.pageSize || 4096)" /></div>
                <div class="k">负责人 / 资产号</div><div>{{ hostLabel(selectedHost, 'cmdb.owner') || '—' }} · {{ hostLabel(selectedHost, 'cmdb.asset_tag') || '—' }}</div>
                <div class="k">基线 Profile</div><div class="ki-mono">{{ selectedHost.initProfile || 'base' }}</div>
              </div>
            </template>

            <el-form v-else label-position="top">
              <div class="ki-grid-3">
                <el-form-item label="业务系统"><el-input v-model="editHostForm.biz" /></el-form-item>
                <el-form-item label="集群环境"><el-input v-model="editHostForm.set" /></el-form-item>
                <el-form-item label="业务模块"><el-input v-model="editHostForm.module" /></el-form-item>
                <el-form-item label="区域机房"><el-input v-model="editHostForm.region" /></el-form-item>
                <el-form-item label="安全域"><el-input v-model="editHostForm.securityArea" /></el-form-item>
                <el-form-item label="故障域"><el-input v-model="editHostForm.faultZone" /></el-form-item>
                <el-form-item label="机架"><el-input v-model="editHostForm.faultRack" /></el-form-item>
                <el-form-item label="负责人"><el-input v-model="editHostForm.owner" /></el-form-item>
                <el-form-item label="资产编号"><el-input v-model="editHostForm.assetTag" class="ki-mono" /></el-form-item>
                <el-form-item label="生命周期状态">
                  <el-select v-model="editHostForm.state" class="ki-w-full">
                    <el-option label="待探活 (pending)" value="pending" />
                    <el-option label="就绪 (ready)" value="ready" />
                    <el-option label="服役中 (active)" value="active" />
                    <el-option label="维护排空 (draining)" value="draining" />
                    <el-option label="已下线 (retired)" value="retired" />
                  </el-select>
                </el-form-item>
                <el-form-item label="CPU 架构">
                  <el-select v-model="editHostForm.arch" class="ki-w-full">
                    <el-option label="amd64" value="amd64" />
                    <el-option label="arm64" value="arm64" />
                  </el-select>
                </el-form-item>
                <el-form-item label="内核页大小">
                  <el-select v-model="editHostForm.pageSize" class="ki-w-full">
                    <el-option label="4K (4096)" :value="4096" />
                    <el-option label="64K (65536)" :value="65536" />
                  </el-select>
                </el-form-item>
              </div>
              <el-button type="primary" :loading="editHostForm.saving" @click="saveHostEdit">保存 CMDB 变更</el-button>
            </el-form>
          </el-tab-pane>

          <el-tab-pane label="CI 影响面" name="impact">
            <template v-if="selectedHostImpact">
              <KiBanner
                v-if="selectedHostImpact.instanceCount > 0"
                variant="warn"
                :text="`变更影响面预警：该主机承载 ${selectedHostImpact.instanceCount} 个活跃实例、关联 ${selectedHostImpact.stackCount} 套应用栈，维护重启前请确认主从切主与排空。`"
                class="ki-mb-3"
              />
              <KiBanner
                v-else
                variant="info"
                text="当前主机暂无活跃实例占用（空闲节点），可直接用于部署或基线初始化。"
                class="ki-mb-3"
              />

              <el-table v-if="selectedHostImpact.instances?.length" :data="selectedHostImpact.instances" size="small">
                <el-table-column prop="instanceName" label="实例名称" min-width="140">
                  <template #default="{ row }"><b class="ki-mono">{{ row.instanceName || row.instanceId }}</b></template>
                </el-table-column>
                <el-table-column prop="product" label="组件" width="110" />
                <el-table-column prop="role" label="角色" width="90" />
                <el-table-column prop="stackName" label="所属应用栈" min-width="120">
                  <template #default="{ row }"><span class="ki-mono">{{ row.stackName || '独立实例' }}</span></template>
                </el-table-column>
                <el-table-column label="占用内存" width="90">
                  <template #default="{ row }"><span class="ki-num">{{ row.memMb || 0 }} MB</span></template>
                </el-table-column>
              </el-table>
            </template>
          </el-tab-pane>

          <el-tab-pane label="监听端口与进程" name="ports">
            <el-table
              v-if="selectedHostImpact?.listeningEntries?.length"
              :data="selectedHostImpact.listeningEntries"
              size="small"
              max-height="320"
            >
              <el-table-column prop="proto" label="协议" width="66" />
              <el-table-column label="监听地址" min-width="140">
                <template #default="{ row }"><span class="ki-mono">{{ row.address }}:{{ row.port }}</span></template>
              </el-table-column>
              <el-table-column prop="process" label="进程" min-width="110">
                <template #default="{ row }"><span class="ki-mono">{{ row.process || '—' }}</span></template>
              </el-table-column>
              <el-table-column label="分类" width="110">
                <template #default="{ row }">
                  <KiStatusBadge
                    :status="row.classification === 'managed' ? 'ok' : row.classification === 'shadow' ? 'warn' : 'pending'"
                    :label="row.classification === 'managed' ? '已纳管' : row.classification === 'shadow' ? '影子服务' : '系统端口'"
                  />
                </template>
              </el-table-column>
              <el-table-column prop="instanceName" label="关联实例" min-width="120">
                <template #default="{ row }"><span class="ki-mono">{{ row.instanceName || '—' }}</span></template>
              </el-table-column>
            </el-table>
            <div v-else class="ki-muted">尚未采集到监听端口，请点击下方「采集 Facts 探活」。</div>
          </el-tab-pane>

          <el-tab-pane label="基线漂移巡检" name="baseline">
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
            <el-table v-else-if="baselineDrifts && baselineDrifts.length > 0" :data="baselineDrifts" size="small">
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
          </el-tab-pane>
        </el-tabs>
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

    <!-- 批量设置业务拓扑弹窗 -->
    <el-dialog v-model="batchTopoDialog.open" :title="`批量设置业务与物理拓扑 (${selectedHosts.length} 台)`" width="560px">
      <p class="ki-secondary ki-mb-3">仅填写需要更新的字段，留空字段将保持原值不变。</p>
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="业务系统 (biz)"><el-input v-model="batchTopoDialog.biz" placeholder="如 HIS核心门诊" /></el-form-item>
          <el-form-item label="集群环境 (set)"><el-input v-model="batchTopoDialog.set" placeholder="如 prod-交易集群" /></el-form-item>
          <el-form-item label="业务模块 (module)"><el-input v-model="batchTopoDialog.module" placeholder="如 关系数据库" /></el-form-item>
          <el-form-item label="区域机房 (region)"><el-input v-model="batchTopoDialog.region" placeholder="如 北京主中心" /></el-form-item>
          <el-form-item label="安全域 (security_area)"><el-input v-model="batchTopoDialog.securityArea" placeholder="如 核心内网" /></el-form-item>
          <el-form-item label="故障域 (zone)"><el-input v-model="batchTopoDialog.faultZone" placeholder="如 az-1" /></el-form-item>
          <el-form-item label="机架 (rack)"><el-input v-model="batchTopoDialog.faultRack" placeholder="如 rack-a01" /></el-form-item>
          <el-form-item label="运维负责人 (owner)"><el-input v-model="batchTopoDialog.owner" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="batchTopoDialog.open = false">取消</el-button>
        <el-button type="primary" @click="submitBatchTopology">批量应用拓扑</el-button>
      </template>
    </el-dialog>

    <!-- 批量改绑凭据弹窗 -->
    <el-dialog v-model="batchCredDialog.open" :title="`批量改绑 SSH 凭据 (${selectedHosts.length} 台)`" width="440px">
      <el-form label-position="top">
        <el-form-item label="目标加密凭据">
          <el-select v-model="batchCredDialog.credentialId" class="ki-w-full" placeholder="选择项目凭据">
            <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="batchCredDialog.open = false">取消</el-button>
        <el-button type="primary" @click="submitBatchCredential">确认改绑</el-button>
      </template>
    </el-dialog>

    <!-- 基线 Profile 选择弹窗 -->
    <el-dialog v-model="initDialog.open" :title="`主机基线初始化 · ${initDialog.titleName}`" width="520px">
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
    <el-dialog v-model="showAdd" title="新增纳管主机" width="620px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="主机名 (DNS-1123)"><el-input v-model="newHost.name" placeholder="node-kunpeng-01" class="ki-mono" /></el-form-item>
          <el-form-item label="资产编号 (可选)"><el-input v-model="newHost.assetTag" placeholder="HIS-SRV-001" class="ki-mono" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="IP / 主机地址"><el-input v-model="newHost.address" placeholder="10.20.1.11" class="ki-mono" /></el-form-item>
          <el-form-item label="SSH 端口"><el-input-number v-model="newHost.sshPort" :min="1" :max="65535" class="ki-w-full" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="SSH 用户名"><el-input v-model="newHost.sshUser" class="ki-mono" /></el-form-item>
          <el-form-item label="凭据绑定方式">
            <div class="ki-flex-row ki-w-full">
              <el-select
                v-if="!newHost.useInlineCred"
                v-model="newHost.credentialId"
                class="ki-flex-1"
                :placeholder="credentials.length ? '选择已有凭据' : '暂无凭据，请切右侧新建'"
              >
                <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
              </el-select>
              <el-button size="small" @click="newHost.useInlineCred = !newHost.useInlineCred">
                {{ newHost.useInlineCred ? '选用已有凭据' : '直接填密码/私钥' }}
              </el-button>
            </div>
          </el-form-item>
        </div>
        <div v-if="newHost.useInlineCred || credentials.length === 0" class="inline-cred-box ki-mb-3">
          <div class="ki-grid-3">
            <el-input v-model="newHost.inlineCredName" size="small" placeholder="凭据名称（留空自动生成）" class="ki-mono" />
            <el-select v-model="newHost.inlineCredKind" size="small" class="ki-w-full">
              <el-option label="SSH 密码" value="ssh_password" />
              <el-option label="SSH 私钥" value="ssh_key" />
            </el-select>
            <el-input
              v-model="newHost.inlineCredSecret"
              size="small"
              type="password"
              show-password
              placeholder="输入 SSH 密码（信封加密入库）"
              @focus="newHost.useInlineCred = true"
            />
          </div>
        </div>
        <div class="ki-grid-3">
          <el-form-item label="业务系统"><el-input v-model="newHost.biz" placeholder="HIS核心门诊" /></el-form-item>
          <el-form-item label="集群环境"><el-input v-model="newHost.set" placeholder="prod-交易集群" /></el-form-item>
          <el-form-item label="业务模块"><el-input v-model="newHost.module" placeholder="关系数据库" /></el-form-item>
          <el-form-item label="区域机房"><el-input v-model="newHost.region" placeholder="北京主中心" /></el-form-item>
          <el-form-item label="安全域"><el-input v-model="newHost.securityArea" placeholder="核心内网" /></el-form-item>
          <el-form-item label="故障域"><el-input v-model="newHost.faultZone" placeholder="az-1" /></el-form-item>
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
      </el-form>
      <template #footer>
        <el-button @click="showAdd = false">取消</el-button>
        <el-button type="primary" @click="createHost">保存主机</el-button>
      </template>
    </el-dialog>

    <KiDangerConfirmModal
      v-model="deleteModal.open"
      :env="ws.currentEnv"
      :title="deleteModal.batchIds.length > 0 ? `确认批量移除 ${deleteModal.batchIds.length} 台主机` : '确认移除纳管主机'"
      :resource-name="deleteModal.batchIds.length > 0 ? `${deleteModal.batchIds.length} hosts` : String(deleteModal.host?.name || '')"
      risk-summary="移除后目标主机将不再受平台纳管；若仍有活跃实例占用端口或目录，后端将自动拒绝删除以保护生产业务。"
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
  align-items: stretch;
  min-height: 500px;
}

.ledger-nav {
  width: 248px;
  flex: none;
  padding: 12px 10px;
}

.inline-cred-box {
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
}

.nav-mode-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
  padding: 3px;
  margin-bottom: 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
}

.nav-mode-btn {
  border: 0;
  border-radius: var(--ki-radius-sm);
  padding: 5px 4px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
  background-color: transparent;
  cursor: pointer;
}

.nav-mode-btn.is-active {
  background-color: var(--ki-bg-surface);
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
}

.nav-root,
.nav-item {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 8px;
  border-radius: var(--ki-radius-sm);
  cursor: pointer;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
}

.nav-root {
  font-size: var(--ki-fs-sm);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
  margin-bottom: 6px;
}

.nav-lvl-1 {
  padding-left: 10px;
  font-weight: var(--ki-fw-medium);
  color: var(--ki-text-primary);
}

.nav-lvl-2 {
  padding-left: 22px;
}

.nav-lvl-3 {
  padding-left: 34px;
  color: var(--ki-text-muted);
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

.nav-empty {
  padding: 6px 10px;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
  cursor: pointer;
}

.ledger-main {
  flex: 1;
  min-width: 0;
}

.search-kw {
  width: 310px;
}

.active-filter-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
  font-size: var(--ki-fs-xs);
}

.clear-chip-btn {
  border: 0;
  background: transparent;
  color: var(--ki-brand);
  font-weight: var(--ki-fw-semibold);
  cursor: pointer;
  padding: 0;
}

.batch-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
}

.batch-bar__count {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-primary);
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

.host-name,
.topo-main {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.host-sub {
  font-size: 11px;
}

.btn-ico {
  margin-right: 4px;
}

.btn-ico-r {
  margin-left: 4px;
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

.profile-check-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

@media (max-width: 960px) {
  .ledger-body {
    flex-direction: column;
    align-items: stretch;
  }

  .ledger-nav {
    width: auto;
  }
}
</style>
