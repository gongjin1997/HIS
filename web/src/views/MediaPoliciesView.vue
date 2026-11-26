<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Lock, HardDrive, Cloud, Globe, ArrowUp, ArrowDown, Activity } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiStatusBadge from '@/components/KiStatusBadge.vue'

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

const ws = useWorkspaceStore()
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
  arr.forEach((s, i) => { s.priority = i + 1 })
  editor.sources = arr
}

async function loadPolicies() {
  if (!ws.currentProjectId) return
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
  if (!activeId.value) await savePolicy()
  if (!activeId.value) return
  try {
    const { data } = await apiRequest<Record<string, unknown>>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/media-policies/${activeId.value}:test`
    )
    probeResult.value = data
    ElMessage.success('连通性与权限探活完成')
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadPolicies)
onMounted(loadPolicies)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">三元制品介质源策略中心 (Local / 华为云 OBS·S3 / Online)</h1>
        <div class="ki-muted" style="font-size: 12px">支持优先级排序、严格物理离线红线 (KI-SRC-0005)、relay-cache 与 direct-presigned 双模式 (ADR-077)</div>
      </div>
      <div class="ki-flex-row">
        <el-button @click="testPolicy"><Activity :size="14" style="margin-right: 4px" /> 测试连通与权限</el-button>
        <el-button type="primary" @click="savePolicy">保存策略</el-button>
      </div>
    </div>

    <div
      v-if="editor.offlineStrict"
      class="ki-flex-row"
      style="padding: 10px 14px; margin-bottom: 14px; border-radius: 6px; background: var(--ki-brand-subtle); border: 1px solid var(--ki-brand-border); color: var(--ki-brand-hover)"
    >
      <Lock :size="15" />
      <span><strong>物理隔离保护中 (offlineStrict=true)</strong>：自动阻断所有公网 Online 源与非内网白名单对象存储桶，后端硬门禁 KI-SRC-0005 同步生效。</span>
    </div>

    <div class="ki-panel" style="margin-bottom: 16px">
      <div class="ki-flex-between" style="flex-wrap: wrap">
        <div class="ki-flex-row">
          <span style="font-weight: 600">严格物理离线模式 (offlineStrict)</span>
          <el-switch v-model="editor.offlineStrict" />
        </div>
        <div class="ki-flex-row">
          <span class="ki-secondary">分发模式：</span>
          <el-radio-group v-model="editor.deliveryMode" size="small">
            <el-radio-button value="auto">auto (智能)</el-radio-button>
            <el-radio-button value="relay-cache">relay-cache (部署机中转)</el-radio-button>
            <el-radio-button value="direct-presigned">direct-presigned (15m 预签名直拉)</el-radio-button>
          </el-radio-group>
        </div>
      </div>
    </div>

    <div v-for="(src, idx) in editor.sources" :key="src.type" class="ki-panel" style="margin-bottom: 12px">
      <div class="ki-flex-between" style="margin-bottom: 10px">
        <div class="ki-flex-row">
          <span class="ki-badge ki-mono" style="background: var(--ki-brand-subtle); color: var(--ki-brand-hover)">#{{ idx + 1 }}</span>
          <HardDrive v-if="src.type === 'local'" :size="16" style="color: var(--ki-status-ok)" />
          <Cloud v-else-if="src.type === 'obs' || src.type === 's3'" :size="16" style="color: var(--ki-brand)" />
          <Globe v-else :size="16" style="color: var(--ki-text-muted)" />
          <strong :style="editor.offlineStrict && (src.type === 'online' || !src.internal) ? 'text-decoration: line-through; opacity: 0.55' : ''">
            {{ src.type === 'local' ? '本地离线仓库与缓存池 (Local)' : src.type === 'online' ? '在线官方源与受控代理 (Online)' : '对象存储桶 (华为云 OBS / SeaweedFS S3)' }}
          </strong>
          <KiStatusBadge v-if="editor.offlineStrict && (src.type === 'online' || !src.internal)" status="blocked" />
        </div>
        <div class="ki-flex-row">
          <el-button size="small" :disabled="idx === 0" @click="moveSource(idx, -1)"><ArrowUp :size="13" /></el-button>
          <el-button size="small" :disabled="idx === editor.sources.length - 1" @click="moveSource(idx, 1)"><ArrowDown :size="13" /></el-button>
        </div>
      </div>

      <div v-if="src.type === 'local'" class="ki-mono ki-secondary" style="font-size: 12px">
        本地目录: {{ (src.roots || []).join(', ') }}
      </div>
      <div v-else-if="src.type === 'obs' || src.type === 's3'" class="ki-grid-2">
        <el-form-item label="厂商快速预设">
          <el-select v-model="editor.obsPreset" style="width: 100%" @change="applyVendorPreset">
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
      <div v-else>
        <el-input v-model="src.baseUrl" :disabled="editor.offlineStrict" class="ki-mono" />
      </div>
    </div>

    <div v-if="probeResult" class="ki-panel">
      <div style="font-weight: 600; margin-bottom: 8px">最近一次连通性探活结果 (:test)</div>
      <pre class="ki-mono" style="margin: 0; padding: 10px; background: var(--ki-bg-terminal); color: var(--ki-term-fg); border-radius: 4px; overflow: auto">{{ JSON.stringify(probeResult, null, 2) }}</pre>
    </div>
  </div>
</template>
