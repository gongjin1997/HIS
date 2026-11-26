<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiStatusBadge from '@/components/KiStatusBadge.vue'
import KiArchBadge from '@/components/KiArchBadge.vue'

const ws = useWorkspaceStore()
const hosts = ref<Array<Record<string, unknown>>>([])
const profiles = ref<Record<string, unknown>>({})
const modules = ref<Array<Record<string, unknown>>>([])
const activeTab = ref('hosts')
const showAdd = ref(false)

const newHost = reactive({
  name: '',
  address: '',
  sshPort: 22,
  sshUser: 'root',
  faultZone: 'az-1',
  arch: 'amd64',
  pageSize: 4096,
})

async function loadAll() {
  if (!ws.currentProjectId) return
  const [hRes, pRes] = await Promise.all([
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/hosts`),
    apiRequest<{ profiles: Record<string, unknown>; modules: Array<Record<string, unknown>> }>('GET', '/api/v1/hostinit/profiles'),
  ])
  hosts.value = hRes.data.items || []
  profiles.value = pRes.data.profiles || {}
  modules.value = pRes.data.modules || []
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
    ElMessage.success('1-RTT Facts 采集完成')
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">CMDB 主机与 22 模块初始化引擎</h1>
        <div class="ki-muted" style="font-size: 12px">自动识别 amd64 / arm64-4K / arm64-64K 页大小与 7 类生产内核 Profile</div>
      </div>
      <el-button type="primary" @click="showAdd = true">新增纳管主机</el-button>
    </div>

    <el-tabs v-model="activeTab">
      <el-tab-pane label="主机列表" name="hosts">
        <div class="ki-panel">
          <el-table :data="hosts">
            <el-table-column prop="name" label="主机名 (DNS-1123)" min-width="150">
              <template #default="{ row }"><span class="ki-mono">{{ row.name }}</span></template>
            </el-table-column>
            <el-table-column prop="address" label="IP 地址" width="140">
              <template #default="{ row }"><span class="ki-mono">{{ row.address }}:{{ row.sshPort }}</span></template>
            </el-table-column>
            <el-table-column label="架构 / 页大小" width="140">
              <template #default="{ row }">
                <KiArchBadge :arch="String(row.arch || 'amd64')" :page-size="Number(row.pageSize || 4096)" />
              </template>
            </el-table-column>
            <el-table-column prop="faultZone" label="故障域" width="100" />
            <el-table-column prop="state" label="状态" width="120">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state)" /></template>
            </el-table-column>
            <el-table-column label="操作" width="130">
              <template #default="{ row }">
                <el-button size="small" @click="probeHost(String(row.id))">Facts 探活</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="`主机初始化 (${modules.length} 模块 / ${Object.keys(profiles).length} Profiles)`" name="init">
        <div class="ki-panel">
          <div style="margin-bottom: 12px; display: flex; flex-wrap: wrap; gap: 8px">
            <span v-for="name in Object.keys(profiles)" :key="name" class="ki-badge ki-mono" style="background: var(--ki-brand-subtle); color: var(--ki-brand-hover)">
              profile: {{ name }}
            </span>
          </div>
          <el-table :data="modules" size="small">
            <el-table-column prop="order" label="#" width="60" />
            <el-table-column prop="id" label="模块 ID" width="160">
              <template #default="{ row }"><span class="ki-mono">{{ row.id }}</span></template>
            </el-table-column>
            <el-table-column prop="title" label="名称" width="200" />
            <el-table-column prop="description" label="功能与安全基线" />
          </el-table>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="showAdd" title="注册 CMDB 主机" width="480px">
      <el-form label-position="top">
        <el-form-item label="主机名 (DNS-1123)"><el-input v-model="newHost.name" placeholder="node-kunpeng-01" class="ki-mono" /></el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="IP / 主机地址"><el-input v-model="newHost.address" placeholder="10.20.1.11" class="ki-mono" /></el-form-item>
          <el-form-item label="SSH 端口"><el-input-number v-model="newHost.sshPort" :min="1" :max="65535" style="width: 100%" /></el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="架构">
            <el-select v-model="newHost.arch" style="width: 100%">
              <el-option label="amd64" value="amd64" />
              <el-option label="arm64" value="arm64" />
            </el-select>
          </el-form-item>
          <el-form-item label="内核页大小">
            <el-select v-model="newHost.pageSize" style="width: 100%">
              <el-option label="4096 (4K)" :value="4096" />
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
