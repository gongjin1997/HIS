<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Landmark, Network, Plus, Trash2, Zap } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface RackUnit {
  id: string
  rackId: string
  deviceName: string
  deviceRole: string
  startU: number
  uSize: number
  powerW: number
  status: string
  colorTag: string
}

interface Rack {
  id: string
  siteName: string
  roomName: string
  name: string
  uHeight: number
  powerKw: number
  status: string
  notes: string
  usedU: number
  usedPowerW: number
  utilPercent: number
  units: RackUnit[]
}

interface IPAMPrefix {
  id: string
  cidr: string
  vlanId: number
  vrf: string
  siteName: string
  gateway: string
  role: string
  description: string
  totalUsable: number
  usedCount: number
  utilPercent: number
}

interface IPAMAddress {
  id: string
  prefixId: string
  ipAddress: string
  status: string
  hostname: string
  macAddress: string
  description: string
}

const ws = useWorkspaceStore()
const loading = ref(false)
const activeTab = ref<'racks' | 'ipam'>('racks')
const racks = ref<Rack[]>([])
const selectedRackId = ref('')
const prefixes = ref<IPAMPrefix[]>([])
const selectedPrefixId = ref('')
const addresses = ref<IPAMAddress[]>([])

const rackDialog = reactive({ open: false, siteName: 'BJ-IDC-01', roomName: 'A区主机房', name: '', uHeight: 42, powerKw: 10, notes: '' })
const mountDialog = reactive({ open: false, deviceName: '', deviceRole: 'server', startU: 1, uSize: 2, powerW: 500 })
const prefixDialog = reactive({ open: false, cidr: '', vlanId: 120, vrf: 'prod-core', siteName: 'BJ-IDC-01', role: 'business', description: '' })
const allocDialog = reactive({ open: false, ipAddress: '', status: 'active', hostname: '', macAddress: '', description: '' })

const currentRack = computed(() => racks.value.find((r) => r.id === selectedRackId.value) || racks.value[0] || null)
const currentPrefix = computed(() => prefixes.value.find((p) => p.id === selectedPrefixId.value) || prefixes.value[0] || null)

// Build top-down U slots (U42 .. U1) for authentic NetBox rack elevation rendering
const rackSlots = computed(() => {
  const r = currentRack.value
  if (!r) return []
  const byTopU = new Map<number, RackUnit>()
  const occupied = new Set<number>()
  for (const u of r.units || []) {
    const topU = u.startU + u.uSize - 1
    byTopU.set(topU, u)
    for (let k = u.startU; k <= topU; k++) occupied.add(k)
  }
  const rows: Array<{ u: number; unit?: RackUnit; skipped: boolean }> = []
  for (let u = r.uHeight; u >= 1; u--) {
    const unit = byTopU.get(u)
    if (unit) {
      rows.push({ u, unit, skipped: false })
    } else if (occupied.has(u)) {
      rows.push({ u, skipped: true })
    } else {
      rows.push({ u, skipped: false })
    }
  }
  return rows
})

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [rRes, pRes] = await Promise.all([
      apiRequest<{ items: Rack[] }>('GET', `/api/v1/projects/${pid}/dcim/racks`),
      apiRequest<{ items: IPAMPrefix[] }>('GET', `/api/v1/projects/${pid}/ipam/prefixes`),
    ])
    racks.value = rRes.data.items || []
    prefixes.value = pRes.data.items || []
    if (!racks.value.some((r) => r.id === selectedRackId.value) && racks.value.length > 0) {
      selectedRackId.value = racks.value[0].id
    }
    if (!prefixes.value.some((p) => p.id === selectedPrefixId.value) && prefixes.value.length > 0) {
      selectedPrefixId.value = prefixes.value[0].id
    }
    if (selectedPrefixId.value) {
      await loadAddresses(selectedPrefixId.value)
    }
  } finally {
    loading.value = false
  }
}

async function loadAddresses(prefixId: string) {
  if (!ws.currentProjectId || !prefixId) return
  const { data } = await apiRequest<{ items: IPAMAddress[] }>(
    'GET',
    `/api/v1/projects/${ws.currentProjectId}/ipam/prefixes/${encodeURIComponent(prefixId)}/addresses`
  )
  addresses.value = data.items || []
}

async function createRack() {
  try {
    const { data } = await apiRequest<Rack>('POST', `/api/v1/projects/${ws.currentProjectId}/dcim/racks`, { body: rackDialog })
    ElMessage.success(`机柜 ${data.name} 已创建`)
    selectedRackId.value = data.id
    rackDialog.open = false
    rackDialog.name = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openMountAtU(u: number) {
  mountDialog.startU = u
  mountDialog.deviceName = ''
  mountDialog.deviceRole = 'server'
  mountDialog.uSize = 2
  mountDialog.powerW = 500
  mountDialog.open = true
}

async function mountDevice() {
  if (!currentRack.value) return
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/dcim/racks/${encodeURIComponent(currentRack.value.id)}/units`, {
      body: {
        deviceName: mountDialog.deviceName,
        deviceRole: mountDialog.deviceRole,
        startU: mountDialog.startU,
        uSize: mountDialog.uSize,
        powerW: mountDialog.powerW,
      },
    })
    ElMessage.success('设备已上架至机柜（U 位防冲突校验通过）')
    mountDialog.open = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function unmountDevice(unit: RackUnit) {
  try {
    await ElMessageBox.confirm(`确认将「${unit.deviceName}」(U${unit.startU}-U${unit.startU + unit.uSize - 1}) 从机柜下架吗？`, '设备下架', { type: 'warning' })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/dcim/units/${encodeURIComponent(unit.id)}`)
    ElMessage.success('设备已下架，U 位已释放')
    await loadAll()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function createPrefix() {
  try {
    const { data } = await apiRequest<IPAMPrefix>('POST', `/api/v1/projects/${ws.currentProjectId}/ipam/prefixes`, { body: prefixDialog })
    ElMessage.success(`CIDR 网段 ${data.cidr} 已创建`)
    selectedPrefixId.value = data.id
    prefixDialog.open = false
    prefixDialog.cidr = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function allocateIP() {
  if (!currentPrefix.value) return
  try {
    const { data } = await apiRequest<IPAMAddress>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/ipam/prefixes/${encodeURIComponent(currentPrefix.value.id)}/allocate`,
      { body: allocDialog }
    )
    ElMessage.success(`已分配 IP 地址：${data.ipAddress}`)
    allocDialog.open = false
    allocDialog.ipAddress = ''
    allocDialog.hostname = ''
    allocDialog.description = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function releaseIP(addr: IPAMAddress) {
  await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/ipam/addresses/${encodeURIComponent(addr.id)}`)
  ElMessage.success(`已回收 IP ${addr.ipAddress}`)
  await loadAll()
}

watch(selectedPrefixId, (id) => {
  if (id) void loadAddresses(id)
})
watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="DCIM 机柜与 IPAM 网段管理"
      subtitle="借鉴 NetBox：42U 物理机柜 U 位立面图可视化上架（防 U 位重叠冲突 + 功率核算）与 CIDR 网段 IP 自动分配台账"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button v-if="activeTab === 'racks'" type="primary" @click="rackDialog.open = true">
          <Plus :size="14" class="btn-ico" /> 新增物理机柜
        </el-button>
        <el-button v-else type="primary" @click="prefixDialog.open = true">
          <Plus :size="14" class="btn-ico" /> 新增 CIDR 网段
        </el-button>
      </template>
    </PageHeader>

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <!-- Tab 1: DCIM 42U 机柜立面图 -->
        <el-tab-pane :label="`物理机柜 U 位视图 (${racks.length})`" name="racks">
          <div class="dcim-grid">
            <div class="rack-list">
              <div
                v-for="r in racks"
                :key="r.id"
                class="rack-card"
                :class="{ 'rack-card--active': currentRack?.id === r.id }"
                @click="selectedRackId = r.id"
              >
                <div class="rack-card__top">
                  <Landmark :size="14" class="rack-ico" />
                  <b>{{ r.name }}</b>
                  <span class="ki-mono ki-muted">{{ r.uHeight }}U</span>
                </div>
                <div class="rack-card__meta ki-muted">{{ r.siteName }} · {{ r.roomName }}</div>
                <div class="rack-card__bar">
                  <div class="bar-fill" :style="{ width: `${r.utilPercent}%` }" />
                </div>
                <div class="rack-card__foot ki-mono ki-muted">
                  <span>已用 {{ r.usedU }}/{{ r.uHeight }}U ({{ r.utilPercent }}%)</span>
                  <span><Zap :size="11" /> {{ (r.usedPowerW / 1000).toFixed(2) }}/{{ r.powerKw }}kW</span>
                </div>
              </div>
            </div>

            <div v-if="currentRack" class="elevation-panel">
              <div class="elev-head ki-mb-3">
                <div>
                  <b>{{ currentRack.siteName }} / {{ currentRack.name }}（{{ currentRack.uHeight }}U 机柜立面图）</b>
                  <p class="ki-muted elev-sub">点击任意空闲 U 位槽可直接上架服务器/交换机，系统自动校验 U 高度与区间防重叠。</p>
                </div>
                <el-button type="primary" size="small" @click="openMountAtU(1)">
                  <Plus :size="13" class="btn-ico" /> 上架设备
                </el-button>
              </div>

              <div class="rack-chassis">
                <template v-for="slot in rackSlots" :key="slot.u">
                  <div v-if="!slot.skipped" class="u-row" :style="{ height: `${(slot.unit ? slot.unit.uSize : 1) * 24}px` }">
                    <div class="u-num ki-mono">U{{ String(slot.u).padStart(2, '0') }}</div>
                    <div
                      v-if="slot.unit"
                      class="u-device"
                      :class="`u-device--${slot.unit.colorTag || 'primary'}`"
                    >
                      <div class="u-device__left">
                        <b>{{ slot.unit.deviceName }}</b>
                        <span class="ki-mono u-device__meta">
                          {{ slot.unit.deviceRole }} · U{{ slot.unit.startU }}-U{{ slot.unit.startU + slot.unit.uSize - 1 }} ({{ slot.unit.uSize }}U) · {{ slot.unit.powerW }}W
                        </span>
                      </div>
                      <el-button link size="small" type="danger" @click="unmountDevice(slot.unit)">下架</el-button>
                    </div>
                    <button v-else type="button" class="u-empty" @click="openMountAtU(slot.u)">
                      <span>空闲 U{{ slot.u }} · 点击上架</span>
                    </button>
                  </div>
                </template>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <!-- Tab 2: IPAM 网段与地址分配台账 -->
        <el-tab-pane :label="`IPAM 网段与地址池 (${prefixes.length})`" name="ipam">
          <div class="dcim-grid">
            <div class="rack-list">
              <div
                v-for="p in prefixes"
                :key="p.id"
                class="rack-card"
                :class="{ 'rack-card--active': currentPrefix?.id === p.id }"
                @click="selectedPrefixId = p.id"
              >
                <div class="rack-card__top">
                  <Network :size="14" class="rack-ico" />
                  <b class="ki-mono">{{ p.cidr }}</b>
                  <span class="ki-mono ki-muted">VLAN {{ p.vlanId }}</span>
                </div>
                <div class="rack-card__meta ki-muted">{{ p.vrf }} · GW {{ p.gateway }} · {{ p.description }}</div>
                <div class="rack-card__bar">
                  <div class="bar-fill" :style="{ width: `${p.utilPercent}%` }" />
                </div>
                <div class="rack-card__foot ki-mono ki-muted">
                  <span>已分配 {{ p.usedCount }} / {{ p.totalUsable }}</span>
                  <span>{{ p.utilPercent }}%</span>
                </div>
              </div>
            </div>

            <div v-if="currentPrefix">
              <div class="elev-head ki-mb-3">
                <div>
                  <b class="ki-mono">{{ currentPrefix.cidr }}</b>
                  <span class="ki-muted"> · VLAN {{ currentPrefix.vlanId }} · 网关 {{ currentPrefix.gateway }}</span>
                </div>
                <el-button type="primary" size="small" @click="allocDialog.open = true">
                  <Plus :size="13" class="btn-ico" /> 分配 IP（支持自动取下一个空闲 IP）
                </el-button>
              </div>

              <el-table :data="addresses" size="small">
                <el-table-column prop="ipAddress" label="IP 地址" width="144">
                  <template #default="{ row }"><b class="ki-mono">{{ row.ipAddress }}</b></template>
                </el-table-column>
                <el-table-column prop="status" label="状态" width="96">
                  <template #default="{ row }">
                    <KiStatusBadge :status="row.status === 'active' ? 'ok' : row.status === 'reserved' ? 'warn' : 'unprobed'" :label="row.status" />
                  </template>
                </el-table-column>
                <el-table-column prop="hostname" label="绑定主机 / CI" min-width="150" />
                <el-table-column prop="macAddress" label="MAC 地址" width="140">
                  <template #default="{ row }"><span class="ki-mono ki-muted">{{ row.macAddress || '—' }}</span></template>
                </el-table-column>
                <el-table-column prop="description" label="用途说明" min-width="160" />
                <el-table-column label="操作" width="76" align="right">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="releaseIP(row)">
                      <Trash2 :size="12" />
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 新增机柜 -->
    <el-dialog v-model="rackDialog.open" title="新增物理机柜" width="460px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="机房站点"><el-input v-model="rackDialog.siteName" /></el-form-item>
          <el-form-item label="机房区域"><el-input v-model="rackDialog.roomName" /></el-form-item>
        </div>
        <div class="ki-grid-3">
          <el-form-item label="机柜编号"><el-input v-model="rackDialog.name" placeholder="B02-APP" /></el-form-item>
          <el-form-item label="U 高"><el-input-number v-model="rackDialog.uHeight" :min="12" :max="60" class="ki-w-full" /></el-form-item>
          <el-form-item label="额定功率(kW)"><el-input-number v-model="rackDialog.powerKw" :min="2" :max="40" class="ki-w-full" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="rackDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createRack">创建机柜</el-button>
      </template>
    </el-dialog>

    <!-- 上架设备 -->
    <el-dialog v-model="mountDialog.open" title="设备上架至机柜 U 位" width="460px">
      <el-form label-position="top">
        <el-form-item label="设备名称"><el-input v-model="mountDialog.deviceName" placeholder="his-app-node-02" /></el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="设备角色">
            <el-select v-model="mountDialog.deviceRole" class="ki-w-full">
              <el-option label="服务器 (server)" value="server" />
              <el-option label="交换机 (switch)" value="switch" />
              <el-option label="防火墙 (firewall)" value="firewall" />
              <el-option label="存储阵列 (storage)" value="storage" />
            </el-select>
          </el-form-item>
          <el-form-item label="功耗 (W)"><el-input-number v-model="mountDialog.powerW" :min="50" :max="5000" class="ki-w-full" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="起始 U 位"><el-input-number v-model="mountDialog.startU" :min="1" :max="currentRack?.uHeight || 42" class="ki-w-full" /></el-form-item>
          <el-form-item label="占用 U 高"><el-input-number v-model="mountDialog.uSize" :min="1" :max="12" class="ki-w-full" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="mountDialog.open = false">取消</el-button>
        <el-button type="primary" @click="mountDevice">确认上架</el-button>
      </template>
    </el-dialog>

    <!-- 新增 CIDR 网段 -->
    <el-dialog v-model="prefixDialog.open" title="新增 IPAM CIDR 网段" width="460px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="CIDR 网段 *"><el-input v-model="prefixDialog.cidr" placeholder="10.20.20.0/24" /></el-form-item>
          <el-form-item label="VLAN ID"><el-input-number v-model="prefixDialog.vlanId" :min="1" :max="4094" class="ki-w-full" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="VRF 路由域"><el-input v-model="prefixDialog.vrf" /></el-form-item>
          <el-form-item label="机房站点"><el-input v-model="prefixDialog.siteName" /></el-form-item>
        </div>
        <el-form-item label="用途描述"><el-input v-model="prefixDialog.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="prefixDialog.open = false">取消</el-button>
        <el-button type="primary" @click="createPrefix">创建网段</el-button>
      </template>
    </el-dialog>

    <!-- 分配 IP -->
    <el-dialog v-model="allocDialog.open" :title="`从 ${currentPrefix?.cidr} 分配 IP`" width="460px">
      <el-form label-position="top">
        <el-form-item label="指定 IP（留空则自动计算并分配下一个空闲 IP）">
          <el-input v-model="allocDialog.ipAddress" placeholder="留空自动分配" />
        </el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="主机名 / 设备名"><el-input v-model="allocDialog.hostname" placeholder="his-app-02" /></el-form-item>
          <el-form-item label="状态">
            <el-select v-model="allocDialog.status" class="ki-w-full">
              <el-option label="active (已分配)" value="active" />
              <el-option label="reserved (预留/VIP)" value="reserved" />
              <el-option label="dhcp (DHCP池)" value="dhcp" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item label="用途说明"><el-input v-model="allocDialog.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="allocDialog.open = false">取消</el-button>
        <el-button type="primary" @click="allocateIP">确认分配</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.dcim-grid {
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 18px;
  align-items: start;
}

@media (width <= 1080px) {
  .dcim-grid {
    grid-template-columns: 1fr;
  }
}

.rack-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rack-card {
  padding: 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  cursor: pointer;
}

.rack-card--active {
  border-color: var(--ki-brand-primary);
  background-color: var(--ki-brand-primary-bg);
}

.rack-card__top {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--ki-fs-sm);
}

.rack-ico {
  color: var(--ki-brand-primary);
}

.rack-card__top span {
  margin-left: auto;
  font-size: 11px;
}

.rack-card__meta {
  font-size: 11px;
  margin-top: 4px;
}

.rack-card__bar {
  height: 6px;
  border-radius: 999px;
  background-color: var(--ki-bg-surface);
  margin: 8px 0 6px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  background-color: var(--ki-brand-primary);
}

.rack-card__foot {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
}

.elev-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.elev-sub {
  margin: 3px 0 0;
  font-size: var(--ki-fs-xs);
}

.rack-chassis {
  border: 2px solid var(--ki-border-control);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  padding: 6px;
  max-height: 640px;
  overflow-y: auto;
}

.u-row {
  display: flex;
  align-items: stretch;
  gap: 6px;
  margin-bottom: 2px;
}

.u-num {
  width: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--ki-text-muted);
  background-color: var(--ki-bg-surface);
  border-radius: var(--ki-radius-xs);
  border: 1px solid var(--ki-border-subtle);
  flex-shrink: 0;
}

.u-empty {
  flex: 1;
  border: 1px dashed var(--ki-border-subtle);
  border-radius: var(--ki-radius-xs);
  background-color: transparent;
  color: var(--ki-text-muted);
  font-size: 10px;
  text-align: left;
  padding: 0 10px;
  cursor: pointer;
}

.u-empty:hover {
  border-color: var(--ki-brand-primary);
  color: var(--ki-brand-primary);
  background-color: var(--ki-brand-primary-bg);
}

.u-device {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 10px;
  border-radius: var(--ki-radius-xs);
  border-left: 4px solid var(--ki-brand-primary);
  background-color: var(--ki-bg-surface);
  border-top: 1px solid var(--ki-border-subtle);
  border-right: 1px solid var(--ki-border-subtle);
  border-bottom: 1px solid var(--ki-border-subtle);
  font-size: var(--ki-fs-xs);
}

.u-device--ok {
  border-left-color: var(--ki-status-ok);
}

.u-device--warn {
  border-left-color: var(--ki-status-warn);
}

.u-device--accent,
.u-device--info {
  border-left-color: var(--ki-status-info);
}

.u-device__left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.u-device__meta {
  font-size: 11px;
  color: var(--ki-text-muted);
}
</style>
