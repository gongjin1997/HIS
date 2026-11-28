<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { KeyRound, Plus, RefreshCw, RotateCw, Server, ShieldAlert } from 'lucide-vue-next'
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
  cred_version: number
  last_seen_at: string
  enrolled_at: string
}

const scope = useScopeStore()
const nodes = ref<NodeItem[]>([])
const loading = ref(false)
const enrollModal = ref(false)
const issuedToken = ref('')
const rotatedCred = ref<{ nodeId: string; cred: string } | null>(null)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: NodeItem[] }>('/api/v1/nodes?allApps=true')
    nodes.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function issueEnrollToken() {
  try {
    const { data } = await api.post<{ token: string }>('/api/v1/nodes/enroll-tokens', {
      ttlMinutes: 15,
      labels: { env: 'production' },
    })
    issuedToken.value = data.token
    enrollModal.value = true
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function rotateCred(nodeId: string) {
  try {
    const { data } = await api.post<{ newCredential: string }>(`/api/v1/nodes/${nodeId}/rotate-credential`)
    rotatedCred.value = { nodeId, cred: data.newCredential }
    ElMessage.success('已完成双轨凭据轮换（旧凭据保留 5 分钟宽限期）')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function setAction(nodeId: string, act: 'isolate' | 'unisolate' | 'revoke') {
  try {
    await api.post(`/api/v1/nodes/${nodeId}/${act}`)
    ElMessage.success(`节点操作 ${act} 已生效`)
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
      title="零信任外联节点池 (liveops-node)"
      description="遵循 LiveOps 零信任外联原则：节点通过一次性 15m 注册令牌主动向控制面报到，15s 心跳 / 30s 降级 / 45s 离线，支持双凭据无感轮换与隔离吊销。"
      :icon="Server"
      :badge="`${nodes.length} 台纳管节点`"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新节点</span></el-button>
        <el-button type="primary" @click="issueEnrollToken"><Plus :size="14" /><span class="ks-gap">签发一次性注册令牌</span></el-button>
      </template>
    </KsBanner>

    <div v-if="rotatedCred" class="ki-card ks-alert-card">
      <div class="ks-alert-title"><KeyRound :size="15" /><span>节点 {{ rotatedCred.nodeId }} 新凭据（仅展示一次，旧凭据 5m 内双轨兼容）</span></div>
      <code class="ki-mono ks-code-line">{{ rotatedCred.cred }}</code>
    </div>

    <div class="ki-card">
      <el-table :data="nodes" size="small" stripe empty-text="暂无外联节点，请点击右上角签发一次性注册令牌并使用 bin/liveops-node 接入">
        <el-table-column prop="node_id" label="节点 ID" width="210">
          <template #default="{ row }"><span class="ki-mono">{{ row.node_id }}</span></template>
        </el-table-column>
        <el-table-column prop="hostname" label="主机名" min-width="150" />
        <el-table-column prop="ip_address" label="IP 地址" width="130">
          <template #default="{ row }"><span class="ki-mono">{{ row.ip_address }}</span></template>
        </el-table-column>
        <el-table-column prop="os_arch" label="平台架构" width="120" />
        <el-table-column prop="cred_version" label="凭据版本" width="90">
          <template #default="{ row }"><span class="ki-mono">v{{ row.cred_version }}</span></template>
        </el-table-column>
        <el-table-column prop="status" label="健康状态" width="120">
          <template #default="{ row }">
            <span
              class="ki-badge"
              :class="row.status === 'ONLINE' ? 'ks-pill-ok' : row.status === 'DEGRADED' || row.status === 'ISOLATED' ? 'ks-pill-warn' : 'ks-pill-danger'"
            >
              {{ row.status }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="管控操作" width="250">
          <template #default="{ row }">
            <div class="ki-row-actions">
              <el-button size="small" :disabled="row.status === 'REVOKED'" @click="rotateCred(row.node_id)">
                <RotateCw :size="12" /><span class="ks-gap-xs">轮换凭据</span>
              </el-button>
              <el-button v-if="row.status !== 'ISOLATED'" size="small" :disabled="row.status === 'REVOKED'" @click="setAction(row.node_id, 'isolate')">隔离</el-button>
              <el-button v-else size="small" @click="setAction(row.node_id, 'unisolate')">恢复</el-button>
              <el-button size="small" type="danger" plain :disabled="row.status === 'REVOKED'" @click="setAction(row.node_id, 'revoke')">
                <ShieldAlert :size="12" />
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="enrollModal" title="零信任节点一次性注册指令 (15 分钟有效)" width="620px">
      <p class="ki-muted">在目标边缘主机上执行以下命令完成零信任出站接入（令牌使用一次后立即失效）：</p>
      <pre class="ks-cmd-box ki-mono">./bin/liveops-node --server http://127.0.0.1:8080 --enroll-token {{ issuedToken }}</pre>
      <template #footer>
        <el-button type="primary" @click="enrollModal = false">我已复制</el-button>
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

.ks-alert-card {
  margin-bottom: var(--ki-space-4);
  background-color: var(--ki-status-ok-subtle);
  border-color: var(--ki-status-ok-border);
}

.ks-alert-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-status-ok-text);
  margin-bottom: 6px;
}

.ks-code-line {
  display: block;
  word-break: break-all;
  color: var(--ki-text-primary);
}

.ks-cmd-box {
  padding: 14px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  white-space: pre-wrap;
  word-break: break-all;
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

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}
</style>
