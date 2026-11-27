<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, RefreshCw, Search, Server, Cpu, ShieldCheck } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'

const auth = useAuthStore()
const ws = useWorkspaceStore()

const loading = ref(false)
const hosts = ref<Array<Record<string, unknown>>>([])
const profiles = ref<Record<string, unknown>>({})
const modules = ref<Array<Record<string, unknown>>>([])
const activeTab = ref('hosts')
const showAdd = ref(false)
const selectedHost = ref<Record<string, unknown> | null>(null)
const drawerOpen = ref(false)

const filterArch = ref<string>('ALL')
const filterZone = ref<string>('ALL')
const keyword = ref('')

const newHost = reactive({
  name: '',
  address: '',
  sshPort: 22,
  sshUser: 'root',
  faultZone: 'az-1',
  arch: 'amd64',
  pageSize: 4096,
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
    const [hRes, pRes] = await Promise.all([
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/hosts`),
      apiRequest<{ profiles: Record<string, unknown>; modules: Array<Record<string, unknown>> }>('GET', '/api/v1/hostinit/profiles'),
    ])
    hosts.value = hRes.data.items || []
    profiles.value = pRes.data.profiles || {}
    modules.value = pRes.data.modules || []
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
    ElMessage.success('主机已注册至 CMDB')
    showAdd.value = false
    newHost.name = ''
    newHost.address = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function probeHost(hostId: string) {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/hosts/${hostId}:probe`)
    ElMessage.success('1-RTT POSIX Facts 采集完成')
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openHostDetail(row: Record<string, unknown>) {
  selectedHost.value = row
  drawerOpen.value = true
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div class="hosts-page">
    <PageHeader title="CMDB 主机" :refresh="loadAll" :refreshing="loading" />

    <!-- 顶部可点击统计条（HCS LedgerView 风格） -->
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
          <div class="ki-chip__label">ARM64 64K 页节点</div>
          <div class="ki-chip__val ki-num">{{ arm64kTotal }}</div>
        </div>
      </div>
      <div class="ki-chip">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div>
          <div class="ki-chip__label">探活就绪 / 内核模块</div>
          <div class="ki-chip__val ki-num">{{ readyTotal }} / {{ modules.length }}</div>
        </div>
      </div>
    </div>

    <!-- 左右结构：左侧分类导航 + 右侧卡片主表（对标 HCS LedgerView） -->
    <div class="ledger-body">
      <aside class="ki-card ledger-nav">
        <div class="nav-root" :class="{ 'is-active': filterZone === 'ALL' && filterArch === 'ALL' }" @click="filterZone = 'ALL'; filterArch = 'ALL'">
          <Server :size="14" />
          <span>全部 CMDB 主机</span>
          <em class="ki-num">{{ hosts.length }}</em>
        </div>

        <div class="nav-section"><Cpu :size="13" /> CPU 架构与页大小</div>
        <div class="nav-item" :class="{ 'is-active': filterArch === 'amd64' }" @click="filterArch = 'amd64'">
          <span>amd64-4K</span>
          <em class="ki-num">{{ hosts.filter((h) => String(h.arch || 'amd64') === 'amd64').length }}</em>
        </div>
        <div class="nav-item" :class="{ 'is-active': filterArch === 'arm64' }" @click="filterArch = 'arm64'">
          <span>arm64 (鲲鹏/飞腾)</span>
          <em class="ki-num">{{ hosts.filter((h) => String(h.arch || '') === 'arm64').length }}</em>
        </div>
        <div class="nav-item" :class="{ 'is-active': filterArch === '64k' }" @click="filterArch = '64k'">
          <span>arm64-64K 警示</span>
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
          <span class="ki-secondary">支持 1-RTT POSIX Facts 探测与 {{ Object.keys(profiles).length }} 套内核 Profile 基线检查</span>
          <div class="table-actions">
            <el-button @click="loadAll"><RefreshCw :size="14" class="btn-ico" /> 刷新</el-button>
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

            <el-table v-loading="loading" :data="filteredHosts" @row-click="openHostDetail">
              <el-table-column label="主机名称" min-width="190">
                <template #default="{ row }">
                  <div class="host-name ki-mono">{{ row.name }}</div>
                  <div class="ki-muted ki-mono host-sub">{{ row.id }}</div>
                </template>
              </el-table-column>
              <el-table-column label="SSH 端点" width="160">
                <template #default="{ row }">
                  <span class="ki-mono">{{ row.sshUser || 'root' }}@{{ row.address }}:{{ row.sshPort || 22 }}</span>
                </template>
              </el-table-column>
              <el-table-column label="架构 / 页大小" width="140">
                <template #default="{ row }">
                  <KiDomainTag kind="arch" :value="String(row.arch || 'amd64')" :page-size="Number(row.pageSize || 4096)" />
                </template>
              </el-table-column>
              <el-table-column prop="faultZone" label="故障域" width="100" />
              <el-table-column label="状态" width="120">
                <template #default="{ row }">
                  <KiStatusBadge :status="String(row.state || 'pending')" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="150" fixed="right">
                <template #default="{ row }">
                  <el-button link type="primary" @click.stop="openHostDetail(row)">详情</el-button>
                  <el-button
                    v-if="auth.can('host:probe', ws.currentProjectId)"
                    link
                    type="primary"
                    @click.stop="probeHost(String(row.id))"
                  >
                    Facts 探活
                  </el-button>
                </template>
              </el-table-column>
              <template #empty><el-empty description="没有符合条件的主机" :image-size="70" /></template>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="`主机初始化基线 (${modules.length} 模块)`" name="init">
            <div class="section-title">内置生产 Profile 集合<span class="sub">按业务角色自动叠加参数</span></div>
            <div class="profile-list ki-mb-3">
              <span v-for="name in Object.keys(profiles)" :key="name" class="ki-badge ki-mono profile-pill">
                profile: {{ name }}
              </span>
            </div>
            <el-table :data="modules" size="small">
              <el-table-column prop="order" label="#" width="60" />
              <el-table-column prop="id" label="模块 ID" width="170">
                <template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template>
              </el-table-column>
              <el-table-column prop="title" label="名称" width="200" />
              <el-table-column prop="description" label="功能与安全基线" />
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </section>
    </div>

    <!-- 主机详情抽屉（HCS ResourceDrawer 风格：section-title + kv-grid） -->
    <KiDrawer v-model="drawerOpen" :title="String(selectedHost?.name || '主机详情')" subtitle="CMDB 资产元数据与 POSIX Facts" size="md">
      <template v-if="selectedHost">
        <div class="section-title">基本连接信息</div>
        <div class="kv-grid">
          <div class="k">主机 ID</div><div><KiMonoText :value="String(selectedHost.id || '')" /></div>
          <div class="k">运行状态</div><div><KiStatusBadge :status="String(selectedHost.state || 'pending')" /></div>
          <div class="k">IP 地址</div><div class="ki-mono">{{ selectedHost.address }}:{{ selectedHost.sshPort || 22 }}</div>
          <div class="k">SSH 用户</div><div class="ki-mono">{{ selectedHost.sshUser || 'root' }}</div>
          <div class="k">架构 / 页</div><div><KiDomainTag kind="arch" :value="String(selectedHost.arch || 'amd64')" :page-size="Number(selectedHost.pageSize || 4096)" /></div>
          <div class="k">故障域</div><div>{{ selectedHost.faultZone || '—' }}</div>
        </div>
      </template>
      <template #footer>
        <el-button @click="drawerOpen = false">关闭</el-button>
        <el-button v-if="selectedHost && auth.can('host:probe', ws.currentProjectId)" type="primary" @click="probeHost(String(selectedHost.id))">
          执行 1-RTT Facts 探活
        </el-button>
      </template>
    </KiDrawer>

    <el-dialog v-model="showAdd" title="新增 CMDB 纳管主机" width="520px">
      <el-form label-position="top">
        <el-form-item label="主机名 (DNS-1123)"><el-input v-model="newHost.name" placeholder="node-kunpeng-01" class="ki-mono" /></el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="IP / 主机地址"><el-input v-model="newHost.address" placeholder="10.20.1.11" class="ki-mono" /></el-form-item>
          <el-form-item label="SSH 端口"><el-input-number v-model="newHost.sshPort" :min="1" :max="65535" class="ki-w-full" /></el-form-item>
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
  </div>
</template>

<style scoped>
.ledger-body {
  display: flex;
  gap: var(--ki-space-4);
  align-items: flex-start;
}

.ledger-nav {
  width: 224px;
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
  padding-left: 24px;
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
