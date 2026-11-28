<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { HardDrive, Network, Plus, Trash2, Zap } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'

interface RackUnit {
  id: string
  rackId: string
  ciId: string
  deviceName: string
  modelCode: string
  uStart: number
  uSize: number
  face: string
  powerW: number
  colorTag: string
}

interface Rack {
  id: string
  datacenter: string
  room: string
  name: string
  uHeight: number
  powerKw: number
  usedU: number
  usedPowerW: number
  notes: string
  units: RackUnit[]
}

interface IPAMPrefix {
  id: string
  cidr: string
  vlanId: number
  site: string
  vrf: string
  gateway: string
  description: string
  totalIps: number
  usedIps: number
  utilPct: number
}

interface IPAMAddress {
  id: string
  prefixId: string
  ip: string
  status: string
  ciId: string
  dnsName: string
  owner: string
  description: string
}

const loading = ref(false)
const activeTab = ref<'racks' | 'ipam'>('racks')
const racks = ref<Rack[]>([])
const selectedRackId = ref('')
const prefixes = ref<IPAMPrefix[]>([])
const selectedPrefixId = ref('')
const addresses = ref<IPAMAddress[]>([])

const showRackDialog = ref(false)
const rackForm = reactive({ datacenter: '华东主数据中心 (IDC-SH01)', room: 'A 区 302 机房', name: '', uHeight: 42, powerKw: 12, notes: '' })

const mountForm = reactive({ deviceName: '', modelCode: 'm_host', uStart: 1, uSize: 2, face: 'front', powerW: 450, colorTag: 'primary' })

const showPrefixDialog = ref(false)
const pfxForm = reactive({ cidr: '10.20.20.0/24', vlanId: 1024, site: 'IDC-SH01', vrf: 'vrf-prod', gateway: '10.20.20.1', description: '' })

const allocForm = reactive({ ip: '', dnsName: '', owner: 'SRE 核心组', description: '' })

const selectedRack = computed(() => racks.value.find((r) => r.id === selectedRackId.value) || null)

interface RackSlotRow {
  u: number
  unit?: RackUnit
  isTop?: boolean
  occupied?: boolean
}

const elevationRows = computed<RackSlotRow[]>(() => {
  const r = selectedRack.value
  if (!r) return []
  const unitAt = new Map<number, { unit: RackUnit; isTop: boolean }>()
  for (const u of r.units || []) {
    const topU = u.uStart + u.uSize - 1
    for (let slot = u.uStart; slot <= topU; slot++) {
      unitAt.set(slot, { unit: u, isTop: slot === topU })
    }
  }
  const rows: RackSlotRow[] = []
  for (let u = r.uHeight; u >= 1; u--) {
    const hit = unitAt.get(u)
    if (!hit) rows.push({ u })
    else rows.push({ u, unit: hit.unit, isTop: hit.isTop, occupied: true })
  }
  return rows
})

async function loadAll() {
  loading.value = true
  try {
    const [rRes, pRes] = await Promise.all([
      apiRequest<{ items: Rack[] }>('GET', '/api/v1/cmdb/dcim/racks'),
      apiRequest<{ items: IPAMPrefix[] }>('GET', '/api/v1/cmdb/ipam/prefixes'),
    ])
    racks.value = rRes.data.items || []
    prefixes.value = pRes.data.items || []
    if (!racks.value.some((x) => x.id === selectedRackId.value) && racks.value.length > 0) {
      selectedRackId.value = racks.value[0].id
    }
    if (!prefixes.value.some((x) => x.id === selectedPrefixId.value) && prefixes.value.length > 0) {
      selectedPrefixId.value = prefixes.value[0].id
    }
    if (selectedPrefixId.value) {
      await loadAddresses(selectedPrefixId.value)
    }
  } finally {
    loading.value = false
  }
}

async function loadAddresses(pfxId: string) {
  selectedPrefixId.value = pfxId
  const { data } = await apiRequest<{ items: IPAMAddress[] }>('GET', `/api/v1/cmdb/ipam/addresses?prefixId=${pfxId}`)
  addresses.value = data.items || []
}

async function createRack() {
  try {
    await apiRequest('POST', '/api/v1/cmdb/dcim/racks', { body: rackForm })
    ElMessage.success('42U 机柜已创建')
    showRackDialog.value = false
    rackForm.name = ''
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建机柜失败')
  }
}

async function mountDevice() {
  if (!selectedRack.value) return
  try {
    await apiRequest('POST', `/api/v1/cmdb/dcim/racks/${selectedRack.value.id}/units`, { body: mountForm })
    ElMessage.success('设备已上架到指定 U 位')
    mountForm.deviceName = ''
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '上架失败（可能 U 位重叠）')
  }
}

async function unmountDevice(unitId: string) {
  await apiRequest('DELETE', `/api/v1/cmdb/dcim/units/${unitId}`)
  ElMessage.success('设备已下架释放 U 位')
  await loadAll()
}

async function createPrefix() {
  try {
    await apiRequest('POST', '/api/v1/cmdb/ipam/prefixes', { body: pfxForm })
    ElMessage.success('CIDR 网段已录入')
    showPrefixDialog.value = false
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '新增网段失败')
  }
}

async function allocateIP(autoNext: boolean) {
  if (!selectedPrefixId.value) return
  try {
    const { data } = await apiRequest<IPAMAddress>('POST', `/api/v1/cmdb/ipam/prefixes/${selectedPrefixId.value}/allocate`, {
      body: {
        ip: autoNext ? '' : allocForm.ip,
        dnsName: allocForm.dnsName,
        owner: allocForm.owner,
        description: allocForm.description,
      },
    })
    ElMessage.success(`成功分配 IP: ${data.ip}`)
    allocForm.ip = ''
    allocForm.dnsName = ''
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'IP 分配失败')
  }
}

async function releaseIP(addrId: string) {
  await apiRequest('DELETE', `/api/v1/cmdb/ipam/addresses/${addrId}`)
  ElMessage.success('IP 地址已回收释放')
  await loadAll()
}

onMounted(loadAll)
</script>

<template>
  <div class="dcim-page">
    <PageHeader
      title="DCIM 42U 机柜与 IPAM 网段"
      subtitle="可视化 42U 立面防重叠上架 · CIDR 子网与 IP 自动分配"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="showPrefixDialog = true"><Network :size="14" class="btn-ico" />新增 CIDR 网段</el-button>
        <el-button type="primary" @click="showRackDialog = true"><Plus :size="14" class="btn-ico" />新建机柜</el-button>
      </template>
    </PageHeader>

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="42U 机柜 U 位立面图 (DCIM)" name="racks">
          <div class="dcim-grid">
            <div class="rack-list">
              <div class="section-title">物理机柜 ({{ racks.length }})</div>
              <button
                v-for="r in racks"
                :key="r.id"
                type="button"
                class="rack-btn"
                :class="{ 'is-active': r.id === selectedRackId }"
                @click="selectedRackId = r.id"
              >
                <div class="rack-btn__top">
                  <HardDrive :size="15" />
                  <b>{{ r.name }}</b>
                  <span class="ki-num">{{ r.usedU }}/{{ r.uHeight }}U</span>
                </div>
                <span class="ki-muted rack-btn__sub">{{ r.datacenter }} · {{ r.room }}</span>
                <span class="ki-muted rack-btn__sub">功耗 {{ (r.usedPowerW / 1000).toFixed(2) }} / {{ r.powerKw }} kW</span>
              </button>

              <div class="section-title">上架设备到 U 位</div>
              <el-form label-position="top" size="small">
                <el-form-item label="设备名称"><el-input v-model="mountForm.deviceName" placeholder="his-app-node-02" /></el-form-item>
                <div class="two-col">
                  <el-form-item label="起始 U 位"><el-input-number v-model="mountForm.uStart" :min="1" :max="42" style="width: 100%" /></el-form-item>
                  <el-form-item label="高度 (U)"><el-input-number v-model="mountForm.uSize" :min="1" :max="12" style="width: 100%" /></el-form-item>
                </div>
                <div class="two-col">
                  <el-form-item label="额定功耗 (W)"><el-input-number v-model="mountForm.powerW" :min="50" :step="50" style="width: 100%" /></el-form-item>
                  <el-form-item label="色标分类">
                    <el-select v-model="mountForm.colorTag" style="width: 100%">
                      <el-option label="计算主机" value="primary" />
                      <el-option label="网络交换" value="info" />
                      <el-option label="核心数据库" value="warn" />
                      <el-option label="边缘节点" value="ok" />
                    </el-select>
                  </el-form-item>
                </div>
                <el-button type="primary" style="width: 100%" :disabled="!mountForm.deviceName" @click="mountDevice">
                  上架校验并锁定 U 位
                </el-button>
              </el-form>
            </div>

            <div class="rack-elevation">
              <div class="elev-hdr">
                <strong>{{ selectedRack?.name || '请选择机柜' }} · 42U 前视立面图</strong>
                <span class="ki-muted">自顶向下 U42 → U1 · 自动拦截 U 位重叠冲突</span>
              </div>
              <div class="u-chassis">
                <div v-for="row in elevationRows" :key="row.u" class="u-slot">
                  <span class="u-num ki-mono">U{{ String(row.u).padStart(2, '0') }}</span>
                  <div
                    v-if="row.unit && row.isTop"
                    class="u-device"
                    :class="`u-device--${row.unit.colorTag}`"
                  >
                    <span class="u-dev-name">{{ row.unit.deviceName }}</span>
                    <span class="u-dev-meta ki-mono">{{ row.unit.uStart }}U-{{ row.unit.uStart + row.unit.uSize - 1 }}U · {{ row.unit.powerW }}W</span>
                    <el-button link type="danger" size="small" @click="unmountDevice(row.unit.id)">下架</el-button>
                  </div>
                  <div v-else-if="row.occupied" class="u-device-cont" :class="`u-device--${row.unit?.colorTag}`">
                    <span class="ki-muted">↳ {{ row.unit?.deviceName }} ({{ row.unit?.uSize }}U 占用)</span>
                  </div>
                  <div v-else class="u-empty">空闲槽位</div>
                </div>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="IPAM CIDR 子网与地址台账" name="ipam">
          <div class="ipam-grid">
            <div>
              <div class="section-title">CIDR 网段池 ({{ prefixes.length }})</div>
              <div
                v-for="p in prefixes"
                :key="p.id"
                class="pfx-card"
                :class="{ 'is-active': p.id === selectedPrefixId }"
                @click="loadAddresses(p.id)"
              >
                <div class="pfx-top">
                  <code class="ki-mono pfx-cidr">{{ p.cidr }}</code>
                  <el-tag size="small">VLAN {{ p.vlanId }}</el-tag>
                </div>
                <div class="ki-muted pfx-sub">{{ p.site }} · GW {{ p.gateway || '—' }} · {{ p.description }}</div>
                <el-progress :percentage="Math.min(100, Math.round(p.utilPct))" :stroke-width="6" />
                <div class="ki-muted pfx-stat">已分配 {{ p.usedIps }} / {{ p.totalIps }} 可用地址 ({{ p.utilPct }}%)</div>
              </div>
            </div>

            <div>
              <div class="table-heading">
                <div class="section-title">已分配 IP 地址台账 ({{ addresses.length }})</div>
                <div class="alloc-bar">
                  <el-input v-model="allocForm.dnsName" placeholder="主机域名 (如 app02.his.local)" size="small" style="width: 190px" />
                  <el-input v-model="allocForm.ip" placeholder="留空自动分配下一个空闲 IP" size="small" style="width: 185px" />
                  <el-button type="primary" size="small" @click="allocateIP(!allocForm.ip)">
                    <Zap :size="13" class="btn-ico" />{{ allocForm.ip ? '指定分配' : '自动分配空闲 IP' }}
                  </el-button>
                </div>
              </div>

              <el-table :data="addresses" size="small">
                <el-table-column prop="ip" label="IP 地址" width="135">
                  <template #default="{ row }"><code class="ki-mono">{{ row.ip }}</code></template>
                </el-table-column>
                <el-table-column prop="status" label="状态" width="90" />
                <el-table-column prop="dnsName" label="DNS / 主机名" min-width="170" />
                <el-table-column prop="owner" label="负责人" width="110" />
                <el-table-column prop="description" label="用途说明" min-width="140" />
                <el-table-column label="操作" width="76" align="right">
                  <template #default="{ row }">
                    <el-button link type="danger" size="small" @click="releaseIP(row.id)"><Trash2 :size="12" /></el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="showRackDialog" title="新建物理机柜" width="440px">
      <el-form label-position="top">
        <el-form-item label="数据中心"><el-input v-model="rackForm.datacenter" /></el-form-item>
        <el-form-item label="机房房间"><el-input v-model="rackForm.room" /></el-form-item>
        <el-form-item label="机柜编号"><el-input v-model="rackForm.name" placeholder="RACK-A02" /></el-form-item>
        <div class="two-col">
          <el-form-item label="高度 (U)"><el-input-number v-model="rackForm.uHeight" :min="12" :max="48" style="width: 100%" /></el-form-item>
          <el-form-item label="额定电力 (kW)"><el-input-number v-model="rackForm.powerKw" :min="2" :max="40" style="width: 100%" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="showRackDialog = false">取消</el-button>
        <el-button type="primary" @click="createRack">确认创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showPrefixDialog" title="新增 IPAM CIDR 网段" width="440px">
      <el-form label-position="top">
        <el-form-item label="IPv4 CIDR"><el-input v-model="pfxForm.cidr" placeholder="10.20.20.0/24" /></el-form-item>
        <div class="two-col">
          <el-form-item label="VLAN ID"><el-input-number v-model="pfxForm.vlanId" :min="0" :max="4094" style="width: 100%" /></el-form-item>
          <el-form-item label="默认网关"><el-input v-model="pfxForm.gateway" /></el-form-item>
        </div>
        <el-form-item label="机房 / 用途描述"><el-input v-model="pfxForm.description" placeholder="数据库心跳与备份子网" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showPrefixDialog = false">取消</el-button>
        <el-button type="primary" @click="createPrefix">保存网段</el-button>
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
  grid-template-columns: 300px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

.rack-btn {
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

.rack-btn.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.rack-btn__top {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--ki-text-primary);
}

.rack-btn__top b {
  flex: 1;
}

.rack-btn__sub {
  font-size: var(--ki-fs-xs);
}

.elev-hdr {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  font-size: var(--ki-fs-sm);
}

.u-chassis {
  border: 2px solid var(--ki-border-strong);
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-canvas);
  max-height: 540px;
  overflow-y: auto;
  padding: 6px;
}

.u-slot {
  display: grid;
  grid-template-columns: 44px 1fr;
  align-items: center;
  gap: 6px;
  min-height: 24px;
  border-bottom: 1px dashed var(--ki-border-subtle);
}

.u-num {
  text-align: center;
  font-size: 11px;
  color: var(--ki-text-muted);
}

.u-empty {
  font-size: 11px;
  color: var(--ki-text-muted);
  padding: 2px 8px;
}

.u-device,
.u-device-cont {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 2px 10px;
  border-radius: var(--ki-radius-xs);
  font-size: var(--ki-fs-xs);
  border-left: 3px solid var(--ki-brand);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-text-primary);
}

.u-device--warn {
  border-left-color: var(--ki-status-warn);
  background-color: var(--ki-status-warn-subtle);
}

.u-device--ok {
  border-left-color: var(--ki-status-ok);
  background-color: var(--ki-status-ok-subtle);
}

.u-device--info {
  border-left-color: var(--ki-status-running);
  background-color: var(--ki-bg-subtle);
}

.u-dev-name {
  font-weight: var(--ki-fw-semibold);
}

.u-dev-meta {
  color: var(--ki-text-secondary);
}

.ipam-grid {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: var(--ki-space-4);
  align-items: start;
}

.pfx-card {
  padding: 12px;
  margin-bottom: 10px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-subtle);
  cursor: pointer;
}

.pfx-card.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.pfx-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
}

.pfx-cidr {
  font-weight: var(--ki-fw-bold);
  color: var(--ki-text-primary);
}

.pfx-sub,
.pfx-stat {
  font-size: var(--ki-fs-xs);
  margin: 4px 0;
}

.alloc-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

@media (max-width: 960px) {
  .dcim-grid,
  .ipam-grid {
    grid-template-columns: 1fr;
  }
}
</style>
