<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Activity, Save } from 'lucide-vue-next'
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

const auth = useAuthStore()
const ws = useWorkspaceStore()

const loading = ref(false)
const probing = ref(false)
const policies = ref<MediaPolicyItem[]>([])
const probeResult = ref<Record<string, unknown> | null>(null)
const activeEtag = ref('')
const activeId = ref('')

const editor = reactive({
  name: 'prod-ternary-media-policy',
  offlineStrict: true,
  deliveryMode: 'auto' as 'auto' | 'relay-cache' | 'direct-presigned',
  obsPreset: 'huawei-obs',
  sources: [
    { type: 'local', priority: 1, internal: true, roots: ['/var/lib/kinstall/artifacts'] },
    { type: 'obs', priority: 2, internal: true, endpoint: 'https://obs.cn-north-4.myhuaweicloud.com', region: 'cn-north-4', bucket: 'kinstall-artifacts-prod', driver: 's3v4' },
    { type: 'online', priority: 3, internal: false, baseUrl: 'https://releases.kinstall.io' },
  ] as MediaSourceConfig[],
})

function toCardItem(src: MediaSourceConfig): MediaSourceItem {
  const endpoint =
    src.type === 'local'
      ? (src.roots || ['/var/lib/kinstall/artifacts']).join(', ')
      : src.type === 'online'
        ? src.baseUrl || 'https://releases.kinstall.io'
        : `${src.endpoint || ''} / ${src.bucket || ''}`
  const name =
    src.type === 'local'
      ? '本地离线制品仓库与缓存池'
      : src.type === 'online'
        ? '在线官方上游源（受控代理）'
        : '对象存储桶（华为云 OBS / SeaweedFS S3）'
  return {
    id: src.type,
    type: src.type,
    name,
    endpoint,
    enabled: !editor.offlineStrict || src.internal || src.type === 'local',
    probeStatus: src.type === 'local' ? 'ok' : src.type === 'online' ? 'warn' : 'ok',
    latencyMs: src.type === 'local' ? 1 : src.type === 'obs' || src.type === 's3' ? 12 : undefined,
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
  try {
    const { data } = await apiRequest<{ items: MediaPolicyItem[] }>('GET', `/api/v1/projects/${ws.currentProjectId}/media-policies`)
    policies.value = data.items || []
    if (policies.value.length > 0) {
      const p = policies.value[0]
      activeId.value = p.id
      activeEtag.value = `"v-${p.version}"`
      editor.name = p.name
      editor.offlineStrict = p.offlineStrict
      editor.deliveryMode = p.deliveryMode
      if (p.sources?.length) editor.sources = [...p.sources].sort((a, b) => a.priority - b.priority)
    }
  } finally {
    loading.value = false
  }
}

async function savePolicy() {
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
      ElMessage.success('三元介质策略已更新（ETag 乐观锁校验通过）')
    } else {
      const { data, etag } = await apiRequest<MediaPolicyItem>(
        'POST',
        `/api/v1/projects/${ws.currentProjectId}/media-policies`,
        { body: payload, idempotent: true }
      )
      activeId.value = data.id
      activeEtag.value = etag || `"v-${data.version}"`
      ElMessage.success('三元介质策略已创建')
    }
    await loadPolicies()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function testPolicy() {
  probing.value = true
  try {
    if (!activeId.value) await savePolicy()
    if (!activeId.value) return
    const { data } = await apiRequest<Record<string, unknown>>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/media-policies/${activeId.value}:test`
    )
    probeResult.value = data
    ElMessage.success('三元介质源连通性与签名权限探测完成')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    probing.value = false
  }
}

watch(() => ws.currentProjectId, loadPolicies)
onMounted(loadPolicies)
</script>

<template>
  <div v-loading="loading">
    <PageHeader title="介质源策略" :refresh="loadPolicies" :refreshing="loading" />

    <KiBanner
      v-if="editor.offlineStrict"
      variant="offline-lock"
      text="严格物理离线模式 (offlineStrict=true) 已开启：自动封禁公网 Online 上游与非内网对象存储桶，后端门禁 KI-SRC-0005 同步生效。"
      class="ki-mb-4"
    />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>策略全局开关与分发模式<span class="sub">支持 Alt+↑/↓ 键盘重排介质源优先级</span></span>
        <div class="table-actions">
          <el-button :loading="probing" @click="testPolicy">
            <Activity :size="14" class="btn-ico" /> 探测连通与权限
          </el-button>
          <el-button v-if="auth.can('media:write', ws.currentProjectId)" type="primary" @click="savePolicy">
            <Save :size="14" class="btn-ico" /> 保存策略
          </el-button>
        </div>
      </div>

      <div class="ki-flex-between ki-flex-wrap">
        <div class="ki-flex-row">
          <span class="ki-secondary">严格物理离线模式 (offlineStrict)：</span>
          <el-switch v-model="editor.offlineStrict" />
        </div>
        <div class="ki-flex-row">
          <span class="ki-secondary">制品分发模式：</span>
          <el-radio-group v-model="editor.deliveryMode" size="small">
            <el-radio-button value="auto">auto 智能</el-radio-button>
            <el-radio-button value="relay-cache">relay-cache 部署机中转</el-radio-button>
            <el-radio-button value="direct-presigned">direct-presigned 15m 预签名直拉</el-radio-button>
          </el-radio-group>
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

    <!-- 对象存储配置细项 -->
    <div v-for="src in editor.sources.filter((s) => s.type === 'obs' || s.type === 's3')" :key="src.type" class="ki-card ki-mb-4">
      <div class="section-title">对象存储参数（华为云 OBS / SeaweedFS S3）</div>
      <div class="ki-grid-2">
        <el-form-item label="厂商快速预设">
          <el-select v-model="editor.obsPreset" class="ki-w-full" @change="applyVendorPreset">
            <el-option label="华为云 OBS (S3v4 WhenRequired 兼容)" value="huawei-obs" />
            <el-option label="内网 SeaweedFS S3 (P6)" value="seaweedfs" />
          </el-select>
        </el-form-item>
        <el-form-item label="Endpoint"><el-input v-model="src.endpoint" class="ki-mono" /></el-form-item>
        <el-form-item label="Bucket 桶名"><el-input v-model="src.bucket" class="ki-mono" /></el-form-item>
        <el-form-item label="内网白名单 (offlineStrict 放行)">
          <el-switch v-model="src.internal" />
        </el-form-item>
      </div>
    </div>

    <div v-if="probeResult" class="ki-card">
      <div class="section-title">最近一次探活响应 (:test)</div>
      <pre class="ki-mono probe-pre">{{ JSON.stringify(probeResult, null, 2) }}</pre>
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.probe-pre {
  margin: 0;
  padding: 12px 14px;
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  border-radius: var(--ki-radius-sm);
  overflow: auto;
}
</style>
