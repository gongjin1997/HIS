<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { Copy, KeyRound, Network, Radio, ShieldOff } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiBanner from '@/components/ki/KiBanner.vue'

interface AgentRecord {
  id: string
  hostname: string
  ipAddress: string
  zone: string
  osArch: string
  osInfo: string
  agentVersion: string
  status: string
  metrics: Record<string, unknown>
  lastHeartbeatAt: string
}

interface EnrollTokenIssue {
  id: string
  zone: string
  token: string
  maxUses: number
  expiresAt: string
  installCmd: string
}

interface TCPProbeResult {
  target: string
  status: string
  latencyMs: number
  error?: string
  executedBy: string
}

const ws = useWorkspaceStore()
const { formatRelTime } = useKiFormat()
const loading = ref(false)
const activeTab = ref<'fleet' | 'probe'>('fleet')
const agents = ref<AgentRecord[]>([])
const latestToken = ref<EnrollTokenIssue | null>(null)

const tokDialog = reactive({ open: false, zone: 'prod-core', maxUses: 5, ttlMinutes: 15 })
const probeForm = reactive({
  agentId: '',
  targetsText: '127.0.0.1:8080\n169.254.169.254:80',
  timeoutSec: 3,
  running: false,
  results: [] as TCPProbeResult[],
})

async function loadAgents() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: AgentRecord[] }>('GET', `/api/v1/projects/${ws.currentProjectId}/fleet/agents`)
    agents.value = data.items || []
  } finally {
    loading.value = false
  }
}

async function issueToken() {
  try {
    const { data } = await apiRequest<EnrollTokenIssue>('POST', `/api/v1/projects/${ws.currentProjectId}/fleet/enroll-tokens`, {
      body: {
        zone: tokDialog.zone,
        maxUses: tokDialog.maxUses,
        ttlMinutes: tokDialog.ttlMinutes,
        serverUrl: window.location.origin,
      },
    })
    latestToken.value = data
    tokDialog.open = false
    ElMessage.success('已签发一次性短期引导令牌（数据库仅存 SHA-256 哈希）')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function copyInstallCmd() {
  if (!latestToken.value) return
  await navigator.clipboard.writeText(latestToken.value.installCmd)
  ElMessage.success('已复制单向出站 Agent 启动命令')
}

async function revokeAgent(a: AgentRecord) {
  try {
    await ElMessageBox.confirm(`确认立即吊销 Agent「${a.hostname} (${a.id})」凭证吗？吊销后将阻断其一切上报与任务轮询。`, '吊销节点', { type: 'warning' })
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/fleet/agents/${encodeURIComponent(a.id)}:revoke`)
    ElMessage.success('Agent 凭证已吊销')
    await loadAgents()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function runTCPProbes() {
  const lines = probeForm.targetsText.split('\n').map((s) => s.trim()).filter(Boolean)
  const targets = lines.map((line) => {
    const idx = line.lastIndexOf(':')
    return idx > 0 ? { host: line.slice(0, idx), port: Number(line.slice(idx + 1)) || 80 } : { host: line, port: 80 }
  })
  probeForm.running = true
  try {
    const { data } = await apiRequest<{ items: TCPProbeResult[] }>('POST', `/api/v1/projects/${ws.currentProjectId}/fleet/probes/tcp`, {
      body: { agentId: probeForm.agentId, targets, timeoutSec: probeForm.timeoutSec },
    })
    probeForm.results = data.items || []
    ElMessage.success(`完成 ${probeForm.results.length} 个目标的纯 Go TCP 连通性探测`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    probeForm.running = false
  }
}

watch(() => ws.currentProjectId, loadAgents)
onMounted(loadAgents)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="零信任 Agent 执行舰队与网络探测"
      subtitle="SSH + 纯 Go Agent 单向出站双模并存、一次性令牌引导与 CIDR TCP 探测"
      :refresh="loadAgents"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="activeTab = 'probe'">
          <Network :size="14" class="btn-ico" /> TCP 批量连通性探测
        </el-button>
        <el-button type="primary" @click="tokDialog.open = true">
          <KeyRound :size="14" class="btn-ico" /> 签发 Agent 引导令牌
        </el-button>
      </template>
    </PageHeader>

    <div v-if="latestToken" class="ki-card ki-mb-4 cmd-box">
      <div class="cmd-head">
        <b>一次性引导令牌已就绪（区域：{{ latestToken.zone }} · 最多注册 {{ latestToken.maxUses }} 台 · 有效期至 {{ formatRelTime(latestToken.expiresAt) }}）</b>
        <el-button type="primary" size="small" @click="copyInstallCmd">
          <Copy :size="13" class="btn-ico" /> 复制启动命令
        </el-button>
      </div>
      <pre class="cmd-pre ki-mono">{{ latestToken.installCmd }}</pre>
    </div>

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`在线执行舰队 (${agents.length})`" name="fleet">
          <el-table :data="agents" size="small">
            <el-table-column prop="hostname" label="主机名" min-width="150">
              <template #default="{ row }">
                <div class="ki-flex-row"><Radio :size="13" class="ag-ico" /> <b>{{ row.hostname }}</b></div>
                <div class="ki-mono ki-muted sub-id">{{ row.id }}</div>
              </template>
            </el-table-column>
            <el-table-column prop="ipAddress" label="内网 IP" width="136">
              <template #default="{ row }"><span class="ki-mono">{{ row.ipAddress || '—' }}</span></template>
            </el-table-column>
            <el-table-column prop="zone" label="安全域" width="110" />
            <el-table-column label="系统 / 架构" min-width="170">
              <template #default="{ row }"><span class="ki-mono">{{ row.osInfo || 'linux' }} ({{ row.osArch }})</span></template>
            </el-table-column>
            <el-table-column label="实时负载 / 指标" min-width="150">
              <template #default="{ row }">
                <span class="ki-mono ki-muted">{{ row.metrics?.loadAvg || `${row.metrics?.numCpu || '—'} vCPU` }}</span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="96">
              <template #default="{ row }">
                <KiStatusBadge :status="row.status === 'ONLINE' ? 'ok' : row.status === 'REVOKED' ? 'failed' : 'warn'" :label="row.status" />
              </template>
            </el-table-column>
            <el-table-column label="最近心跳" width="100">
              <template #default="{ row }"><span class="ki-muted">{{ formatRelTime(row.lastHeartbeatAt) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ row }">
                <el-button v-if="row.status !== 'REVOKED'" link type="danger" size="small" @click="revokeAgent(row)">
                  <ShieldOff :size="12" class="btn-ico" /> 吊销
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <span class="ki-muted">暂无已注册 Agent；点击右上角「签发 Agent 引导令牌」并在目标 Linux 主机运行 <code>kinstall agent</code> 即可秒级单向出站上线并自动回填 CMDB。</span>
            </template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="CIDR 安全护栏 TCP 批量探测" name="probe">
          <KiBanner
            variant="info"
            text="采用纯 Go net.Dialer 原生握手探测（绝不拼装 bash /dev/tcp），内置 CIDR 护栏自动阻断 169.254.0.0/16 等云元数据敏感网段。"
            class="ki-mb-3"
          />
          <el-form label-position="top">
            <div class="ki-grid-2">
              <el-form-item label="探测发起节点（控制面直连 或 指定在线 Agent）">
                <el-select v-model="probeForm.agentId" clearable class="ki-w-full" placeholder="默认：中心控制面直连探测">
                  <el-option
                    v-for="a in agents.filter((x) => x.status === 'ONLINE')"
                    :key="a.id"
                    :label="`Agent: ${a.hostname} (${a.ipAddress} · ${a.zone})`"
                    :value="a.id"
                  />
                </el-select>
              </el-form-item>
              <el-form-item label="单目标超时（秒）">
                <el-input-number v-model="probeForm.timeoutSec" :min="1" :max="15" class="ki-w-full" />
              </el-form-item>
            </div>
            <el-form-item label="探测目标清单（每行一个 host:port，最多 200 个）">
              <el-input v-model="probeForm.targetsText" type="textarea" :rows="4" class="ki-mono" />
            </el-form-item>
            <el-button type="primary" :loading="probeForm.running" class="ki-mb-3" @click="runTCPProbes">立即执行批量 TCP 探测</el-button>
          </el-form>

          <el-table v-if="probeForm.results.length > 0" :data="probeForm.results" size="small">
            <el-table-column prop="target" label="目标端点" width="180">
              <template #default="{ row }"><b class="ki-mono">{{ row.target }}</b></template>
            </el-table-column>
            <el-table-column label="探测状态" width="120">
              <template #default="{ row }">
                <KiStatusBadge :status="row.status === 'OK' ? 'ok' : row.status === 'BLOCKED' ? 'warn' : 'failed'" :label="row.status" />
              </template>
            </el-table-column>
            <el-table-column label="握手延迟" width="100">
              <template #default="{ row }"><span class="ki-mono">{{ row.latencyMs }} ms</span></template>
            </el-table-column>
            <el-table-column prop="executedBy" label="执行源" width="160">
              <template #default="{ row }"><span class="ki-mono">{{ row.executedBy }}</span></template>
            </el-table-column>
            <el-table-column prop="error" label="诊断详情" min-width="200" />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="tokDialog.open" title="签发一次性零信任 Agent 引导令牌" width="440px">
      <el-form label-position="top">
        <el-form-item label="所属安全域 / 隔离区 (Zone)"><el-input v-model="tokDialog.zone" /></el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="最大注册台数"><el-input-number v-model="tokDialog.maxUses" :min="1" :max="100" class="ki-w-full" /></el-form-item>
          <el-form-item label="有效时长（分钟）"><el-input-number v-model="tokDialog.ttlMinutes" :min="5" :max="1440" class="ki-w-full" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="tokDialog.open = false">取消</el-button>
        <el-button type="primary" @click="issueToken">签发令牌</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.cmd-box {
  border-left: 4px solid var(--ki-brand-primary);
}

.cmd-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  font-size: var(--ki-fs-xs);
  margin-bottom: 8px;
}

.cmd-pre {
  margin: 0;
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  font-size: var(--ki-fs-xs);
  overflow-x: auto;
}

.ag-ico {
  color: var(--ki-status-ok);
}

.sub-id {
  font-size: 11px;
}
</style>
