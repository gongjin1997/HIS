<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeftRight, CheckCircle2, Network, Plus, RefreshCw } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore } from '@/stores/scope'

interface RouterRow {
  router_id: string
  name: string
  listen_host: string
  rate_limit_qps: number
  burst: number
  status: string
}

interface RouteRow {
  route_id: string
  router_id: string
  name: string
  path_prefix: string
  upstream_url: string
  is_default: boolean
  timeout_ms: number
  version: number
}

interface TelemetryRow {
  router_id: string
  bucket_kind: string
  bucket_key: string
  total_requests: number
  error_requests: number
  avg_latency_ms: number
}

interface ArtifactRow {
  file_id: string
  filename: string
  size_bytes: number
  sha256: string
  verification_status: string
}

const scope = useScopeStore()
const activeTab = ref('routes')
const loading = ref(false)
const routeVersion = ref(1)
const routers = ref<RouterRow[]>([])
const routes = ref<RouteRow[]>([])
const telemetry = ref<TelemetryRow[]>([])
const artifacts = ref<ArtifactRow[]>([])

const routeDlg = ref(false)
const routeForm = ref({ routerId: 'rtr-edge-main', name: '', pathPrefix: '/api/', upstreamUrl: 'http://10.20.1.15:8080', isDefault: false })

async function load() {
  loading.value = true
  try {
    const [rtrs, rts, tel, arts] = await Promise.all([
      api.get<{ items: RouterRow[]; routeVersion: number }>('/api/v1/gateway/routers'),
      api.get<{ items: RouteRow[]; routeVersion: number }>('/api/v1/gateway/routes'),
      api.get<{ items: TelemetryRow[] }>('/api/v1/gateway/telemetry'),
      api.get<{ items: ArtifactRow[] }>('/api/v1/artifacts/files'),
    ])
    routers.value = rtrs.data.items || []
    routes.value = rts.data.items || []
    routeVersion.value = rts.data.routeVersion || 1
    telemetry.value = tel.data.items || []
    artifacts.value = arts.data.items || []
    if (routers.value.length > 0 && !routeForm.value.routerId) {
      routeForm.value.routerId = routers.value[0].router_id
    }
  } finally {
    loading.value = false
  }
}

async function createRoute() {
  try {
    await api.post('/api/v1/gateway/routes', routeForm.value)
    ElMessage.success('动态路由已创建并递增 routeVersion')
    routeDlg.value = false
    routeForm.value = { routerId: routers.value[0]?.router_id || '', name: '', pathPrefix: '/api/', upstreamUrl: 'http://10.20.1.15:8080', isDefault: false }
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function setDefault(routeId: string) {
  try {
    const { data } = await api.post<{ routeVersion: number }>(`/api/v1/gateway/routes/${routeId}/set-default`)
    ElMessage.success(`已原子切换默认路由 (G01/G02, routeVersion=v${data.routeVersion})`)
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function verifyArtifact(fileId: string, sha256: string) {
  try {
    await api.post(`/api/v1/artifacts/files/${fileId}/verify`, { expectedSha256: sha256 })
    ElMessage.success('制品 SHA-256 验签通过 (VERIFIED)')
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
      title="动态 API 网关与制品验签门禁"
      description="遵循 LiveOps G01–G05 不变量：单默认路由范围锁原子切换、routeVersion 单调递增、制品 VERIFIED 强门禁与外键防误删。"
      :icon="Network"
      :badge="`routeVersion v${routeVersion}`"
      :tabs="[
        { key: 'routes', label: '动态路由规则', count: routes.length },
        { key: 'telemetry', label: '网关流控与遥测', count: telemetry.length },
        { key: 'artifacts', label: '制品完整性门禁', count: artifacts.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="routeDlg = true"><Plus :size="14" /><span class="ks-gap">新增动态路由</span></el-button>
      </template>
    </KsBanner>

    <div v-if="activeTab === 'routes'" class="ki-card">
      <el-table :data="routes" size="small" stripe>
        <el-table-column prop="name" label="路由名称" min-width="150" />
        <el-table-column prop="path_prefix" label="路径前缀" width="170">
          <template #default="{ row }"><span class="ki-mono">{{ row.path_prefix }}</span></template>
        </el-table-column>
        <el-table-column prop="upstream_url" label="上游地址" min-width="210">
          <template #default="{ row }"><span class="ki-mono">{{ row.upstream_url }}</span></template>
        </el-table-column>
        <el-table-column label="默认路由 (G01)" width="140">
          <template #default="{ row }">
            <span v-if="row.is_default" class="ki-badge ks-pill-ok"><CheckCircle2 :size="12" /><span>默认路由</span></span>
            <span v-else class="ki-muted">常规路由</span>
          </template>
        </el-table-column>
        <el-table-column prop="timeout_ms" label="超时 (ms)" width="100" />
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button size="small" :disabled="row.is_default" @click="setDefault(row.route_id)">
              <ArrowLeftRight :size="12" /><span class="ks-gap-xs">设为默认</span>
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else-if="activeTab === 'telemetry'" class="ki-card">
      <el-table :data="telemetry" size="small" stripe>
        <el-table-column prop="router_id" label="路由器" width="180"><template #default="{ row }"><span class="ki-mono">{{ row.router_id }}</span></template></el-table-column>
        <el-table-column prop="bucket_kind" label="聚合周期" width="110" />
        <el-table-column prop="bucket_key" label="时间桶" width="130"><template #default="{ row }"><span class="ki-mono">{{ row.bucket_key }}</span></template></el-table-column>
        <el-table-column prop="total_requests" label="总请求数" width="130"><template #default="{ row }"><span class="ki-num">{{ row.total_requests.toLocaleString() }}</span></template></el-table-column>
        <el-table-column prop="error_requests" label="错误数" width="110" />
        <el-table-column label="平均延迟" width="130">
          <template #default="{ row }"><span class="ki-mono">{{ row.avg_latency_ms.toFixed(1) }} ms</span></template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else class="ki-card">
      <el-table :data="artifacts" size="small" stripe empty-text="当前作用域暂无制品文件">
        <el-table-column prop="filename" label="制品文件" min-width="180" />
        <el-table-column prop="sha256" label="SHA-256 摘要" min-width="200"><template #default="{ row }"><span class="ki-mono">{{ row.sha256 }}</span></template></el-table-column>
        <el-table-column prop="verification_status" label="验签状态 (G03/G04)" width="150">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.verification_status === 'VERIFIED' ? 'ks-pill-ok' : 'ks-pill-warn'">{{ row.verification_status }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="130">
          <template #default="{ row }">
            <el-button size="small" :disabled="row.verification_status === 'VERIFIED'" @click="verifyArtifact(row.file_id, row.sha256)">执行验签</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="routeDlg" title="新增动态网关路由" width="480px">
      <el-form label-position="top">
        <el-form-item label="归属路由器" required>
          <el-select v-model="routeForm.routerId" style="width: 100%">
            <el-option v-for="rt in routers" :key="rt.router_id" :label="`${rt.name} (${rt.listen_host})`" :value="rt.router_id" />
          </el-select>
        </el-form-item>
        <el-form-item label="路由名称" required><el-input v-model="routeForm.name" placeholder="例如: 检验科 LIS 路由" /></el-form-item>
        <el-form-item label="路径前缀" required><el-input v-model="routeForm.pathPrefix" placeholder="/api/lis" /></el-form-item>
        <el-form-item label="上游服务地址" required><el-input v-model="routeForm.upstreamUrl" placeholder="http://10.20.1.25:8083" /></el-form-item>
        <el-form-item label="设为默认路由 (原子切换)">
          <el-switch v-model="routeForm.isDefault" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="routeDlg = false">取消</el-button>
        <el-button type="primary" @click="createRoute">确认添加</el-button>
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

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-warn {
  background-color: var(--ki-status-warn-subtle);
  border: 1px solid var(--ki-status-warn-border);
  color: var(--ki-status-warn-text);
}
</style>
