<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CheckCircle2,
  Download,
  KeyRound,
  Package,
  Play,
  Radar,
  ShieldAlert,
  Terminal,
  Trash2,
  Upload,
} from 'lucide-vue-next'
import { apiDownload, apiRequest, apiUpload, openSSEStream, type SSELogLine } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import { formatBytesIEC } from '@/composables/useKiFormat'

interface AgentItem {
  id: string
  hostCiId: string
  hostname: string
  ip: string
  zone: string
  os: string
  arch: string
  version: string
  status: string
  cpuCores: number
  memMb: number
  loadAvg: string
  lastHeartbeatAt: string
}

interface EnrollToken {
  id: string
  label: string
  zone: string
  expiresAt: string
  usedAt: string
  plainToken?: string
  installCmd?: string
}

interface HostCI {
  id: string
  name: string
  host?: { ip: string; agentStatus: string }
}

interface DynGroup {
  id: string
  name: string
  matchCount: number
}

interface JobTemplate {
  code: string
  name: string
  category: string
  description: string
  command: string
}

interface JobItem {
  id: string
  name: string
  command: string
  status: string
  createdBy: string
  createdAt: string
  runs: Array<{ id: string; hostName: string; hostIp: string; transport: string; status: string; exitCode: number; output: string }>
}

interface ArtifactItem {
  id: string
  name: string
  version: string
  kind: string
  arch: string
  sha256: string
  sizeBytes: number
  ociRef: string
  createdAt: string
}

interface DedupStats {
  totalArtifacts: number
  uniqueBlobs: number
  logicalBytes: number
  physicalBytes: number
  savedBytes: number
}

const loading = ref(false)
const activeTab = ref<'fleet' | 'jobs' | 'probe' | 'artifacts'>('fleet')

// Fleet
const agents = ref<AgentItem[]>([])
const enrollTokens = ref<EnrollToken[]>([])
const latestEnroll = ref<EnrollToken | null>(null)
const enrollForm = reactive({ label: '生产区边缘节点', zone: 'prod-core', ttlMin: 15 })

// Jobs
const hosts = ref<HostCI[]>([])
const dynGroups = ref<DynGroup[]>([])
const templates = ref<JobTemplate[]>([])
const jobs = ref<JobItem[]>([])
const jobForm = reactive({
  name: '核心集群巡检作业',
  templateCode: 'baseline_inspect',
  command: 'uname -a && uptime && free -h',
  targetMode: 'hosts',
  hostCiIds: [] as string[],
  dynamicGroupId: '',
  artifactId: '',
  remoteDest: '',
})
const jobLogs = ref<SSELogLine[]>([])
let closeJobSSE: (() => void) | null = null

// CIDR TCP Probe
const probeForm = reactive({ cidr: '127.0.0.1/32', portsRaw: '22,80,443,5432,6379,8080', agentId: '', timeoutMs: 600 })
const probeResults = ref<Array<{ ip: string; port: number; open: boolean; latencyMs: number }>>([])
const probeExecutor = ref('')
const probing = ref(false)

// Artifacts & Native Go OCI Push
const artifacts = ref<ArtifactItem[]>([])
const dedupStats = ref<DedupStats>({ totalArtifacts: 0, uniqueBlobs: 0, logicalBytes: 0, physicalBytes: 0, savedBytes: 0 })
const uploadMeta = reactive({ version: '1.0.0', kind: 'binary', arch: 'amd64' })
const showOCIDialog = ref(false)
const ociTargetArt = ref<ArtifactItem | null>(null)
const ociForm = reactive({ targetRef: '127.0.0.1:5000/his/app:v1', expectedArch: 'amd64', credentialId: '', insecureTls: true })

async function loadAll() {
  loading.value = true
  try {
    const [aRes, tRes, hRes, dRes, jtRes, jRes, artRes] = await Promise.all([
      apiRequest<{ items: AgentItem[] }>('GET', '/api/v1/ops/agents'),
      apiRequest<{ items: EnrollToken[] }>('GET', '/api/v1/ops/enroll-tokens'),
      apiRequest<{ items: HostCI[] }>('GET', '/api/v1/cmdb/cis?modelCode=m_host'),
      apiRequest<{ items: DynGroup[] }>('GET', '/api/v1/cmdb/dynamic-groups'),
      apiRequest<{ items: JobTemplate[] }>('GET', '/api/v1/ops/job-templates'),
      apiRequest<{ items: JobItem[] }>('GET', '/api/v1/ops/jobs'),
      apiRequest<{ items: ArtifactItem[]; stats: DedupStats }>('GET', '/api/v1/catalog/artifacts'),
    ])
    agents.value = aRes.data.items || []
    enrollTokens.value = tRes.data.items || []
    hosts.value = hRes.data.items || []
    dynGroups.value = dRes.data.items || []
    templates.value = jtRes.data.items || []
    jobs.value = jRes.data.items || []
    artifacts.value = artRes.data.items || []
    if (artRes.data.stats) dedupStats.value = artRes.data.stats
  } finally {
    loading.value = false
  }
}

async function issueEnrollToken() {
  const { data } = await apiRequest<EnrollToken>('POST', '/api/v1/ops/enroll-tokens', { body: enrollForm })
  latestEnroll.value = data
  ElMessage.success('已签发 15 分钟一次性出站 Agent 纳管令牌')
  await loadAll()
}

async function revokeAgent(a: AgentItem) {
  await ElMessageBox.confirm(`确认吊销 Agent「${a.hostname}」的通信凭据？`, '吊销确认', { type: 'warning' })
  await apiRequest('POST', `/api/v1/ops/agents/${a.id}/revoke`)
  ElMessage.warning('已吊销 Agent 令牌')
  await loadAll()
}

function applyTemplate(code: string) {
  const t = templates.value.find((x) => x.code === code)
  if (t) {
    jobForm.name = t.name
    jobForm.command = t.command
  }
}

function streamJob(jobId: string) {
  if (closeJobSSE) closeJobSSE()
  jobLogs.value = []
  closeJobSSE = openSSEStream(`job:${jobId}`, (line) => {
    jobLogs.value.push(line)
  }, () => { void loadAll() })
}

async function runJob() {
  try {
    const { data } = await apiRequest<JobItem>('POST', '/api/v1/ops/jobs', { body: jobForm })
    ElMessage.success(`作业已下发 (${data.runs?.length || 0} 台主机)`)
    streamJob(data.id)
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '作业下发失败')
  }
}

async function runProbe() {
  probing.value = true
  try {
    const ports = probeForm.portsRaw.split(',').map((s) => Number(s.trim())).filter((n) => n > 0 && n <= 65535)
    const { data } = await apiRequest<{ executor: string; items: typeof probeResults.value }>('POST', '/api/v1/ops/tcp-probe', {
      body: { cidr: probeForm.cidr, ports, agentId: probeForm.agentId, timeoutMs: probeForm.timeoutMs },
    })
    probeResults.value = data.items || []
    probeExecutor.value = data.executor
    ElMessage.success(`CIDR 探测完成（执行面: ${data.executor}），发现 ${probeResults.value.length} 个开放端口`)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '探测失败')
  } finally {
    probing.value = false
  }
}

async function onUploadArtifact(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const fd = new FormData()
  fd.append('file', file)
  fd.append('name', file.name)
  fd.append('version', uploadMeta.version)
  fd.append('kind', uploadMeta.kind)
  fd.append('arch', uploadMeta.arch)
  try {
    await apiUpload('/api/v1/catalog/artifacts', fd)
    ElMessage.success('制品已入库 CAS 内容寻址仓并完成 Ed25519 数字签名')
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '上传失败')
  } finally {
    input.value = ''
  }
}

async function verifyArt(a: ArtifactItem) {
  const { data } = await apiRequest<{ verified: boolean; message: string }>('POST', `/api/v1/catalog/artifacts/${a.id}/verify`)
  if (data.verified) ElMessage.success(data.message)
  else ElMessage.error(data.message)
}

async function downloadArt(a: ArtifactItem) {
  await apiDownload(`/api/v1/catalog/artifacts/${a.id}/download`, a.name)
}

async function exportBOM() {
  const { data } = await apiRequest<Record<string, unknown>>('GET', '/api/v1/catalog/bom')
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'kinstall-artifact-bom.json'
  link.click()
  URL.revokeObjectURL(url)
  ElMessage.success('已导出制品 BOM 清单与 Ed25519 公钥')
}

function openOCIPush(a: ArtifactItem) {
  ociTargetArt.value = a
  ociForm.expectedArch = a.arch === 'arm64' ? 'arm64' : 'amd64'
  showOCIDialog.value = true
}

async function pushOCI() {
  if (!ociTargetArt.value) return
  try {
    const { data } = await apiRequest<{ targetRef: string; digest: string }>('POST', `/api/v1/catalog/artifacts/${ociTargetArt.value.id}/oci-push`, {
      body: ociForm,
    })
    ElMessage.success(`原生 Go OCI 推送成功: ${data.targetRef}`)
    showOCIDialog.value = false
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'OCI 推送失败')
  }
}

async function deleteArt(a: ArtifactItem) {
  await apiRequest('DELETE', `/api/v1/catalog/artifacts/${a.id}`)
  ElMessage.success('制品记录已移除')
  await loadAll()
}

onMounted(loadAll)
onBeforeUnmount(() => {
  if (closeJobSSE) closeJobSSE()
})
</script>

<template>
  <div class="ops-page">
    <PageHeader
      title="执行舰队、批量作业与 CAS 制品仓"
      subtitle="零信任出站 Agent · 动态分组作业 · CIDR 探测 · Ed25519 & Go OCI"
      :refresh="loadAll"
      :refreshing="loading"
    />

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`零信任 Agent 舰队 (${agents.length})`" name="fleet">
          <div class="enroll-bar">
            <el-input v-model="enrollForm.label" placeholder="节点标签" style="width: 180px" />
            <el-input v-model="enrollForm.zone" placeholder="安全域 (prod-core)" style="width: 150px" />
            <el-button type="primary" @click="issueEnrollToken">
              <KeyRound :size="14" class="btn-ico" />签发一次性纳管令牌 (15m)
            </el-button>
          </div>

          <div v-if="latestEnroll" class="enroll-cmd-box">
            <div class="enroll-cmd-hdr">
              <strong>边缘主机单向出站启动命令（零入站端口，自动登记为 CMDB m_host 资产）：</strong>
              <span class="ki-muted">有效期至 {{ latestEnroll.expiresAt }}</span>
            </div>
            <pre class="ki-mono enroll-pre">{{ latestEnroll.installCmd }}</pre>
          </div>

          <el-table :data="agents" size="small" empty-text="当前暂无已纳管出站 Agent，点击上方签发令牌并执行 kinstall agent 即可秒级上线">
            <el-table-column prop="hostname" label="主机名" min-width="140" />
            <el-table-column prop="ip" label="IP 地址" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ row.ip }}</code></template>
            </el-table-column>
            <el-table-column prop="zone" label="安全域" width="110" />
            <el-table-column label="平台 / 规格" width="170">
              <template #default="{ row }">
                <span class="ki-mono">{{ row.os }}/{{ row.arch }} · {{ row.cpuCores }}C/{{ Math.round((row.memMb || 0) / 1024) }}G</span>
              </template>
            </el-table-column>
            <el-table-column prop="loadAvg" label="Load" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ row.loadAvg || '—' }}</code></template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <KiStatusBadge :status="row.status === 'ONLINE' ? 'ok' : row.status === 'REVOKED' ? 'failed' : 'cancelled'" :label="row.status" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="90" align="right">
              <template #default="{ row }">
                <el-button v-if="row.status !== 'REVOKED'" link type="danger" size="small" @click="revokeAgent(row)">
                  <ShieldAlert :size="12" class="btn-ico" />吊销
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="批量作业与制品分发" name="jobs">
          <div class="jobs-grid">
            <div>
              <el-form label-position="top" size="small">
                <div class="two-col">
                  <el-form-item label="快捷运维模板">
                    <el-select v-model="jobForm.templateCode" style="width: 100%" @change="applyTemplate">
                      <el-option v-for="t in templates" :key="t.code" :label="`${t.name} (${t.category})`" :value="t.code" />
                    </el-select>
                  </el-form-item>
                  <el-form-item label="目标选取模式">
                    <el-radio-group v-model="jobForm.targetMode">
                      <el-radio-button value="hosts">指定主机</el-radio-button>
                      <el-radio-button value="dynamic_group">动态分组</el-radio-button>
                    </el-radio-group>
                  </el-form-item>
                </div>

                <el-form-item v-if="jobForm.targetMode === 'hosts'" label="目标 CMDB 主机（自动优先 Agent 通道）">
                  <el-select v-model="jobForm.hostCiIds" multiple filterable placeholder="选择一台或多台主机资产" style="width: 100%">
                    <el-option v-for="h in hosts" :key="h.id" :label="`${h.name} (${h.host?.ip || ''})`" :value="h.id" />
                  </el-select>
                </el-form-item>
                <el-form-item v-else label="目标 CMDB 动态分组">
                  <el-select v-model="jobForm.dynamicGroupId" placeholder="选择规则动态分组" style="width: 100%">
                    <el-option v-for="g in dynGroups" :key="g.id" :label="`${g.name} (匹配 ${g.matchCount} 台)`" :value="g.id" />
                  </el-select>
                </el-form-item>

                <el-form-item label="执行脚本（受高危 Shell 护栏保护）">
                  <el-input v-model="jobForm.command" type="textarea" :rows="3" />
                </el-form-item>

                <el-button type="primary" @click="runJob"><Play :size="13" class="btn-ico" />并发下发批量作业</el-button>
              </el-form>

              <div class="section-title">最近作业记录 ({{ jobs.length }})</div>
              <el-table :data="jobs" size="small" @row-click="(j: JobItem) => streamJob(j.id)">
                <el-table-column prop="name" label="作业名称" min-width="140" />
                <el-table-column label="主机数" width="70">
                  <template #default="{ row }">{{ row.runs?.length || 0 }}</template>
                </el-table-column>
                <el-table-column label="状态" width="105">
                  <template #default="{ row }">
                    <KiStatusBadge :status="row.status === 'SUCCEEDED' ? 'ok' : row.status === 'FAILED' ? 'failed' : 'running'" :label="row.status" />
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <div>
              <div class="section-title"><Terminal :size="14" /> 实时 SSE 作业输出终端</div>
              <div class="sse-console ki-mono">
                <div v-if="jobLogs.length === 0" class="ki-muted">点击下发作业或选择历史作业查看实时输出…</div>
                <div v-for="l in jobLogs" :key="l.seq" class="sse-line">[{{ l.stream }}] {{ l.message }}</div>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="CIDR 批量 TCP 探测" name="probe">
          <div class="probe-bar">
            <el-input v-model="probeForm.cidr" placeholder="10.20.10.0/24 或 127.0.0.1/32" style="width: 210px" />
            <el-input v-model="probeForm.portsRaw" placeholder="22,80,443,5432,6379,8080" style="width: 220px" />
            <el-select v-model="probeForm.agentId" clearable placeholder="控制面本机直探 / 委派 Agent" style="width: 220px">
              <el-option v-for="a in agents.filter((x) => x.status === 'ONLINE')" :key="a.id" :label="`委派 Agent: ${a.hostname} (${a.zone})`" :value="a.id" />
            </el-select>
            <el-button type="primary" :loading="probing" @click="runProbe"><Radar :size="14" class="btn-ico" />开始安全扫描</el-button>
          </div>
          <p class="ki-muted probe-note">内置 CIDR 护栏：自动阻断云元数据网段 169.254.0.0/16 与超大网段爆炸扫描。</p>

          <el-table :data="probeResults" size="small" empty-text="尚未扫描或未发现开放端口">
            <el-table-column prop="ip" label="发现 IP" width="150">
              <template #default="{ row }"><code class="ki-mono">{{ row.ip }}</code></template>
            </el-table-column>
            <el-table-column prop="port" label="开放 TCP 端口" width="130">
              <template #default="{ row }"><code class="ki-mono">{{ row.port }}</code></template>
            </el-table-column>
            <el-table-column label="往返延迟" width="120">
              <template #default="{ row }"><span class="ki-num">{{ row.latencyMs }} ms</span></template>
            </el-table-column>
            <el-table-column label="探测执行面">
              <template #default><span class="ki-mono">{{ probeExecutor || 'control-plane' }}</span></template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="`CAS 制品库与 OCI 直推 (${artifacts.length})`" name="artifacts">
          <div class="dedup-strip">
            <span>逻辑总容量 <b class="ki-num">{{ formatBytesIEC(dedupStats.logicalBytes) }}</b></span>
            <span>CAS 物理占用 <b class="ki-num">{{ formatBytesIEC(dedupStats.physicalBytes) }}</b></span>
            <span>内容去重节省 <b class="ki-num">{{ formatBytesIEC(dedupStats.savedBytes) }}</b></span>
            <span>唯一 Blob <b class="ki-num">{{ dedupStats.uniqueBlobs }}</b></span>
          </div>

          <div class="filter-bar">
            <el-input v-model="uploadMeta.version" placeholder="版本" style="width: 100px" />
            <el-select v-model="uploadMeta.kind" style="width: 130px">
              <el-option label="二进制 binary" value="binary" />
              <el-option label="OCI 镜像 tar" value="oci_tar" />
              <el-option label="安装包 package" value="package" />
              <el-option label="运维脚本 script" value="script" />
            </el-select>
            <el-select v-model="uploadMeta.arch" style="width: 110px">
              <el-option label="amd64" value="amd64" />
              <el-option label="arm64" value="arm64" />
              <el-option label="noarch" value="noarch" />
            </el-select>
            <label class="ki-upload-btn">
              <Upload :size="14" />上传并自动 Ed25519 签名
              <input type="file" @change="onUploadArtifact" />
            </label>
            <el-button @click="exportBOM"><Download :size="14" class="btn-ico" />导出 BOM 清单</el-button>
          </div>

          <el-table :data="artifacts" size="small">
            <el-table-column prop="name" label="制品名称" min-width="170" />
            <el-table-column prop="version" label="版本" width="85" />
            <el-table-column prop="kind" label="类型" width="90" />
            <el-table-column prop="arch" label="架构" width="80" />
            <el-table-column label="SHA-256 摘要" width="135">
              <template #default="{ row }"><code class="ki-mono">{{ row.sha256.slice(0, 12) }}…</code></template>
            </el-table-column>
            <el-table-column label="体积" width="95">
              <template #default="{ row }"><span class="ki-num">{{ formatBytesIEC(row.sizeBytes) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="225" align="right">
              <template #default="{ row }">
                <div class="ki-row-actions">
                  <el-button link type="primary" size="small" @click="verifyArt(row)">
                    <CheckCircle2 :size="12" class="btn-ico" />验签
                  </el-button>
                  <el-button v-if="row.kind === 'oci_tar'" link type="primary" size="small" @click="openOCIPush(row)">
                    <Package :size="12" class="btn-ico" />OCI 直推
                  </el-button>
                  <el-button link size="small" @click="downloadArt(row)">下载</el-button>
                  <el-button link type="danger" size="small" @click="deleteArt(row)"><Trash2 :size="12" /></el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="showOCIDialog" title="纯 Go 原生 OCI 镜像推送（零 docker CLI）" width="480px">
      <el-form label-position="top">
        <el-form-item label="目标仓库地址 (Harbor / SWR)">
          <el-input v-model="ociForm.targetRef" placeholder="harbor.his.local/prod/his-app:v3.2" />
        </el-form-item>
        <el-form-item label="目标集群强制架构校验（Fail-Closed 防混推）">
          <el-select v-model="ociForm.expectedArch" style="width: 100%">
            <el-option label="amd64 (x86_64)" value="amd64" />
            <el-option label="arm64 (鲲鹏/飞腾)" value="arm64" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="ociForm.insecureTls">允许离线自签证书/HTTP 私有仓库</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showOCIDialog = false">取消</el-button>
        <el-button type="primary" @click="pushOCI">校验架构并推送</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.enroll-bar,
.probe-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}

.enroll-cmd-box {
  padding: 12px;
  margin-bottom: 14px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-brand-border);
  background-color: var(--ki-brand-subtle);
}

.enroll-cmd-hdr {
  display: flex;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  margin-bottom: 6px;
}

.enroll-pre {
  margin: 0;
  padding: 8px 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-surface);
  overflow-x: auto;
  font-size: 12px;
}

.jobs-grid {
  display: grid;
  grid-template-columns: 1.15fr 1fr;
  gap: var(--ki-space-4);
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.sse-console {
  height: 360px;
  overflow-y: auto;
  padding: 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-text-terminal);
  font-size: 12px;
  line-height: 1.5;
}

.sse-line {
  margin-bottom: 4px;
  word-break: break-all;
}

.probe-note {
  margin: 0 0 12px;
  font-size: var(--ki-fs-xs);
}

.dedup-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 20px;
  padding: 10px 14px;
  margin-bottom: 12px;
  border-radius: var(--ki-radius-md);
  background-color: var(--ki-bg-subtle);
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-secondary);
}

.dedup-strip b {
  color: var(--ki-text-primary);
  margin-left: 4px;
}

@media (max-width: 960px) {
  .jobs-grid {
    grid-template-columns: 1fr;
  }
}
</style>
