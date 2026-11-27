<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Activity, Download, Save, Upload } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiMediaSourceCard, { type MediaSourceItem } from '@/components/domain/KiMediaSourceCard.vue'

interface MediaSourceConfig {
  type: 'local' | 'obs' | 's3' | 'online'
  priority: number
  internal: boolean
  roots?: string[]
  endpoint?: string
  region?: string
  bucket?: string
  driver?: string
  credentialRef?: string
  baseUrl?: string
}

interface MediaPolicyItem {
  id: string
  name: string
  deliveryMode: 'auto' | 'relay-cache' | 'direct-presigned'
  offlineStrict: boolean
  strictLocal: boolean
  sources: MediaSourceConfig[]
  version: number
}

interface ProbeSourceResult {
  type: string
  priority?: number
  internal?: boolean
  status: string
  latencyMs?: number
  httpStatus?: number
  error?: string
  code?: string
}

interface ProbeResponse {
  policyId?: string
  offlineStrict?: boolean
  deliveryMode?: string
  probedAt?: string
  sources?: ProbeSourceResult[]
}

const auth = useAuthStore()
const ws = useWorkspaceStore()

const loading = ref(false)
const probing = ref(false)
const saving = ref(false)
const policies = ref<MediaPolicyItem[]>([])
const credentials = ref<Array<{ id: string; name: string; kind: string }>>([])
const probeResult = ref<ProbeResponse | null>(null)
const activeEtag = ref('')
const activeId = ref('')

const editor = reactive({
  name: 'prod-ternary-media-policy',
  offlineStrict: true,
  deliveryMode: 'auto' as 'auto' | 'relay-cache' | 'direct-presigned',
  obsPreset: 'huawei-obs',
  sources: [
    { type: 'local', priority: 1, internal: true, roots: ['/var/lib/kinstall/artifacts'] },
    {
      type: 'obs',
      priority: 2,
      internal: true,
      endpoint: 'https://obs.cn-north-4.myhuaweicloud.com',
      region: 'cn-north-4',
      bucket: 'kinstall-artifacts-prod',
      driver: 's3v4',
      credentialRef: '',
    },
    { type: 'online', priority: 3, internal: false, baseUrl: 'https://releases.kinstall.io' },
  ] as MediaSourceConfig[],
})

const localSource = computed(() => editor.sources.find((s) => s.type === 'local'))
const obsSources = computed(() => editor.sources.filter((s) => s.type === 'obs' || s.type === 's3'))
const onlineSource = computed(() => editor.sources.find((s) => s.type === 'online'))

const localRootsText = computed({
  get: () => (localSource.value?.roots || ['/var/lib/kinstall/artifacts']).join(', '),
  set: (val: string) => {
    if (!localSource.value) return
    localSource.value.roots = val
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
  },
})

function findProbeFor(type: string): ProbeSourceResult | undefined {
  return probeResult.value?.sources?.find((s) => s.type.toLowerCase() === type.toLowerCase())
}

function mapProbeStatus(raw?: ProbeSourceResult): MediaSourceItem['probeStatus'] {
  if (!raw) return 'unprobed'
  const st = (raw.status || '').toLowerCase()
  if (st === 'ok' || st === 'reachable') return 'ok'
  if (st === 'blocked') return 'blocked'
  if (st === 'unconfigured') return 'warn'
  if (st === 'missing' || st === 'unreachable' || st === 'failed') return 'failed'
  return 'unprobed'
}

function mapProbeNote(raw?: ProbeSourceResult): string | undefined {
  if (!raw) return undefined
  if (raw.status === 'missing') return '本地目录不存在或不可访问'
  if (raw.status === 'unconfigured') return '未配置地址'
  if (raw.error) return raw.error.length > 72 ? `${raw.error.slice(0, 72)}…` : raw.error
  if (raw.httpStatus) return `HTTP ${raw.httpStatus}`
  return undefined
}

function toCardItem(src: MediaSourceConfig): MediaSourceItem {
  const endpoint =
    src.type === 'local'
      ? (src.roots || ['/var/lib/kinstall/artifacts']).join(', ')
      : src.type === 'online'
        ? src.baseUrl || 'https://releases.kinstall.io'
        : `${src.endpoint || '(未配置)'} / ${src.bucket || '(未配置桶)'}`
  const name =
    src.type === 'local'
      ? '本地离线制品仓库与缓存池'
      : src.type === 'online'
        ? '在线官方上游源（受控代理）'
        : '对象存储桶（华为云 OBS / SeaweedFS S3）'
  const probe = findProbeFor(src.type)
  return {
    id: src.type,
    type: src.type,
    name,
    endpoint,
    enabled: src.type === 'local' ? true : src.type === 'online' ? !editor.offlineStrict : !editor.offlineStrict || src.internal,
    probeStatus: mapProbeStatus(probe),
    latencyMs: probe?.latencyMs,
    probeNote: mapProbeNote(probe),
  }
}

function applyVendorPreset() {
  const obs = editor.sources.find((s) => s.type === 'obs' || s.type === 's3')
  if (!obs) return
  if (editor.obsPreset === 'huawei-obs') {
    obs.endpoint = `https://obs.${obs.region || 'cn-north-4'}.myhuaweicloud.com`
    obs.driver = 's3v4'
  } else if (editor.obsPreset === 'seaweedfs') {
    obs.endpoint = 'https://s3.prod.internal:8333'
    obs.driver = 's3v4'
    obs.internal = true
  }
}

function moveSource(idx: number, dir: -1 | 1) {
  const next = idx + dir
  if (next < 0 || next >= editor.sources.length) return
  const arr = [...editor.sources]
  ;[arr[idx], arr[next]] = [arr[next], arr[idx]]
  arr.forEach((s, i) => {
    s.priority = i + 1
  })
  editor.sources = arr
}

async function loadPolicies() {
  if (!ws.currentProjectId) return
  loading.value = true
  probeResult.value = null
  try {
    const [pRes, cRes] = await Promise.all([
      apiRequest<{ items: MediaPolicyItem[] }>('GET', `/api/v1/projects/${ws.currentProjectId}/media-policies`),
      apiRequest<{ items: Array<{ id: string; name: string; kind: string }> }>(
        'GET',
        `/api/v1/projects/${ws.currentProjectId}/credentials`
      ).catch(() => ({ data: { items: [] } })),
    ])
    policies.value = pRes.data.items || []
    credentials.value = cRes.data.items || []
    if (policies.value.length > 0) {
      const p = policies.value[0]
      activeId.value = p.id
      activeEtag.value = `"v-${p.version}"`
      editor.name = p.name
      editor.offlineStrict = p.offlineStrict
      editor.deliveryMode = p.deliveryMode
      if (p.sources?.length) editor.sources = [...p.sources].sort((a, b) => a.priority - b.priority)
    } else {
      activeId.value = ''
      activeEtag.value = ''
    }
    await ws.refreshOfflineMode()
  } finally {
    loading.value = false
  }
}

async function savePolicy() {
  saving.value = true
  try {
    const payload = {
      name: editor.name,
      offlineStrict: editor.offlineStrict,
      deliveryMode: editor.deliveryMode,
      sources: editor.sources,
    }
    if (activeId.value) {
      const { data, etag } = await apiRequest<MediaPolicyItem>(
        'PUT',
        `/api/v1/projects/${ws.currentProjectId}/media-policies/${activeId.value}`,
        { body: payload, etag: activeEtag.value }
      )
      activeEtag.value = etag || `"v-${data.version}"`
      ElMessage.success('介质源策略已保存并生效')
    } else {
      const { data, etag } = await apiRequest<MediaPolicyItem>(
        'POST',
        `/api/v1/projects/${ws.currentProjectId}/media-policies`,
        { body: payload, idempotent: true }
      )
      activeId.value = data.id
      activeEtag.value = etag || `"v-${data.version}"`
      ElMessage.success('介质源策略已创建并生效')
    }
    await loadPolicies()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    saving.value = false
  }
}

async function testPolicy() {
  probing.value = true
  try {
    if (!activeId.value) await savePolicy()
    if (!activeId.value) return
    const { data } = await apiRequest<ProbeResponse>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/media-policies/${activeId.value}:test`
    )
    probeResult.value = data
    const failedCount = (data.sources || []).filter((s) => s.status === 'missing' || s.status === 'unreachable').length
    if (failedCount > 0) {
      ElMessage.warning(`探测完成：发现 ${failedCount} 个介质源当前不可达，已按真实状态更新`)
    } else {
      ElMessage.success('介质源真实连通性与权限探测完成')
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    probing.value = false
  }
}

function exportPolicyJSON() {
  const payload = {
    name: editor.name,
    offlineStrict: editor.offlineStrict,
    deliveryMode: editor.deliveryMode,
    sources: editor.sources,
  }
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${editor.name || 'media-policy'}.json`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
  ElMessage.success(`已导出介质策略 ${editor.name}.json`)
}

function onImportPolicyJSON(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    try {
      const parsed = JSON.parse(String(reader.result || '{}')) as Partial<MediaPolicyItem>
      if (parsed.name) editor.name = parsed.name
      if (typeof parsed.offlineStrict === 'boolean') editor.offlineStrict = parsed.offlineStrict
      if (parsed.deliveryMode) editor.deliveryMode = parsed.deliveryMode
      if (Array.isArray(parsed.sources) && parsed.sources.length > 0) {
        editor.sources = parsed.sources.map((s, idx) => ({ ...s, priority: s.priority || idx + 1 }))
      }
      probeResult.value = null
      ElMessage.success(`已载入介质策略文件 ${file.name}，请核对后点击「保存策略」生效`)
    } catch (e) {
      ElMessage.error(`策略 JSON 解析失败：${String(e)}`)
    }
  }
  reader.readAsText(file)
  input.value = ''
}

watch(() => ws.currentProjectId, loadPolicies)
onMounted(loadPolicies)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="介质源策略"
      subtitle="管理 Local / OBS·S3 / Online 三元离线回退链、分发模式与真实连通性探测"
      :refresh="loadPolicies"
      :refreshing="loading"
    >
      <template #actions>
        <label class="ki-upload-btn">
          <Upload :size="14" /> 导入策略
          <input type="file" accept=".json" class="hidden-file" @change="onImportPolicyJSON" />
        </label>
        <el-button @click="exportPolicyJSON">
          <Download :size="14" class="btn-ico" /> 导出策略
        </el-button>
        <el-button :loading="probing" @click="testPolicy">
          <Activity :size="14" class="btn-ico" /> 探测连通与权限
        </el-button>
        <el-button v-if="auth.can('media:write', ws.currentProjectId)" type="primary" :loading="saving" @click="savePolicy">
          <Save :size="14" class="btn-ico" /> 保存策略
        </el-button>
      </template>
    </PageHeader>

    <KiBanner
      v-if="editor.offlineStrict"
      variant="offline-lock"
      text="严格物理离线模式已启用：系统将自动阻断公网在线上游与非内网白名单对象存储桶，确保全部软件包与镜像仅从受控内网介质源拉取。"
      class="ki-mb-4"
    />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>策略全局开关与分发模式<span class="sub">按优先级自动回退，支持 Alt+↑/↓ 键盘调整顺序</span></span>
        <span v-if="probeResult?.probedAt" class="ki-muted ki-mono probe-time">
          最近探测：{{ probeResult.probedAt }}
        </span>
      </div>

      <div class="ki-setting-list">
        <div class="ki-setting-row">
          <div class="ki-setting-row__info">
            <span class="ki-setting-row__label">严格物理离线模式</span>
            <span class="ki-setting-row__desc">开启后仅允许本地制品目录与标记为内网白名单的对象存储源；保存后将同步更新顶栏状态</span>
          </div>
          <div class="ki-setting-row__control">
            <el-switch v-model="editor.offlineStrict" />
          </div>
        </div>

        <div class="ki-setting-row">
          <div class="ki-setting-row__info">
            <span class="ki-setting-row__label">制品分发模式</span>
            <span class="ki-setting-row__desc">控制目标主机通过部署机缓存中转或 15 分钟短期预签名 URL 直接拉取</span>
          </div>
          <div class="ki-setting-row__control">
            <el-radio-group v-model="editor.deliveryMode" size="small">
              <el-radio-button value="auto">智能自动</el-radio-button>
              <el-radio-button value="relay-cache">部署机中转</el-radio-button>
              <el-radio-button value="direct-presigned">15m 预签名直拉</el-radio-button>
            </el-radio-group>
          </div>
        </div>
      </div>
    </div>

    <!-- 优先级卡片列表 -->
    <div class="ki-flex-col ki-mb-4">
      <KiMediaSourceCard
        v-for="(src, idx) in editor.sources"
        :key="src.type"
        :source="toCardItem(src)"
        :index="idx"
        :total="editor.sources.length"
        :offline-strict="editor.offlineStrict"
        :probing="probing"
        @move-up="moveSource($event, -1)"
        @move-down="moveSource($event, 1)"
        @probe="testPolicy"
      />
    </div>

    <!-- 三元源细项参数 -->
    <div class="ki-grid-2 ki-mb-4">
      <div v-if="localSource" class="ki-card">
        <div class="section-title">本地离线制品目录 (Local)</div>
        <el-form label-position="top">
          <el-form-item label="离线根目录列表（逗号分隔）">
            <el-input v-model="localRootsText" class="ki-mono" placeholder="/var/lib/kinstall/artifacts" />
          </el-form-item>
        </el-form>
      </div>

      <div v-if="onlineSource" class="ki-card">
        <div class="section-title">在线官方上游源 (Online)</div>
        <el-form label-position="top">
          <el-form-item label="上游 Base URL（严格离线模式下自动拦截）">
            <el-input v-model="onlineSource.baseUrl" class="ki-mono" :disabled="editor.offlineStrict" placeholder="https://releases.kinstall.io" />
          </el-form-item>
        </el-form>
      </div>
    </div>

    <!-- 对象存储配置细项 -->
    <div v-for="src in obsSources" :key="src.type" class="ki-card ki-mb-4">
      <div class="section-title">对象存储参数（华为云 OBS / SeaweedFS S3）</div>
      <el-form label-position="top">
        <div class="ki-grid-3">
          <el-form-item label="厂商快速预设">
            <el-select v-model="editor.obsPreset" class="ki-w-full" @change="applyVendorPreset">
              <el-option label="华为云 OBS (S3v4 兼容)" value="huawei-obs" />
              <el-option label="内网 SeaweedFS S3" value="seaweedfs" />
            </el-select>
          </el-form-item>
          <el-form-item label="Endpoint 地址">
            <el-input v-model="src.endpoint" class="ki-mono" />
          </el-form-item>
          <el-form-item label="Bucket 桶名">
            <el-input v-model="src.bucket" class="ki-mono" />
          </el-form-item>
          <el-form-item label="Region 区域">
            <el-input v-model="src.region" placeholder="cn-north-4" class="ki-mono" />
          </el-form-item>
          <el-form-item label="Vault 访问凭据引用 (AK/SK)">
            <el-select v-model="src.credentialRef" clearable class="ki-w-full" placeholder="可选：选择保险库 s3_aksk 凭据">
              <el-option v-for="c in credentials" :key="c.id" :label="`${c.name} (${c.kind})`" :value="c.id" />
            </el-select>
          </el-form-item>
          <el-form-item label="内网白名单放行">
            <div class="whitelist-row">
              <el-switch v-model="src.internal" />
              <span class="ki-muted">离线模式下允许访问该内网存储</span>
            </div>
          </el-form-item>
        </div>
      </el-form>
    </div>

    <div v-if="probeResult" class="ki-card">
      <div class="section-title">最近一次真实连通性探测详情</div>
      <pre class="ki-mono probe-pre">{{ JSON.stringify(probeResult, null, 2) }}</pre>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.probe-time {
  font-size: var(--ki-fs-xs);
}

.whitelist-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  font-size: var(--ki-fs-xs);
}

.probe-pre {
  margin: 0;
  padding: 12px 14px;
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  border-radius: var(--ki-radius-sm);
  overflow: auto;
}

.hidden-file {
  display: none;
}
</style>
