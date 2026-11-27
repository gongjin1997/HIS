<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Download, CloudDownload, Trash2, Send, Search, HardDrive } from 'lucide-vue-next'
import { apiDownload, apiRequest, apiUpload } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import { useAuthStore } from '@/stores/auth'
import { useKiFormat } from '@/composables/useKiFormat'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiMonoText from '@/components/ki/KiMonoText.vue'
import KiBanner from '@/components/ki/KiBanner.vue'

interface ArtifactEntry {
  id: string
  category: string
  product: string
  version: string
  arch: string
  pageSize?: number
  fileName: string
  logicalPath: string
  sizeBytes: number
  sha256: string
  modifiedAt: string
}

interface ProductReadiness {
  product: string
  pluginName: string
  defaultVersion: string
  targets: string[]
  readyAmd64: boolean
  readyArm64: boolean
  artifactCount: number
  containerReady: boolean
}

interface CatalogResp {
  primaryRoot: string
  roots: string[]
  offlineStrict: boolean
  totalCount: number
  totalBytes: number
  uniqueBlobs?: number
  casDedupBytesSaved?: number
  items: ArtifactEntry[]
  readinessMatrix: ProductReadiness[]
}

interface HostOption {
  id: string
  name: string
  address: string
}

const router = useRouter()
const ws = useWorkspaceStore()
const auth = useAuthStore()
const { formatBytesIEC: formatBytes, formatAbsTime } = useKiFormat()

const loading = ref(false)
const activeTab = ref<'items' | 'matrix'>('items')
const catFilter = ref('all')
const keyword = ref('')
const catalog = ref<CatalogResp | null>(null)
const hosts = ref<HostOption[]>([])

const uploadDialog = reactive({
  open: false,
  uploading: false,
  category: 'binaries',
  product: 'valkey',
  version: '9.1.4',
  arch: 'amd64',
  file: null as File | null,
})

const prefetchDialog = reactive({
  open: false,
  loading: false,
  url: '',
  category: 'binaries',
  product: 'nginx',
  version: '1.28.0',
  arch: 'amd64',
  fileName: '',
})

const pushImageDialog = reactive({
  open: false,
  loading: false,
  logicalPath: '',
  hostId: '',
  output: '',
})

const ociPushDialog = reactive({
  open: false,
  loading: false,
  logicalPath: '',
  targetRef: 'harbor.internal.corp/library/app:1.0.0',
  expectedArch: 'amd64',
  username: '',
  password: '',
  insecureTls: true,
  result: '',
})

const filteredItems = computed(() => {
  const list = catalog.value?.items || []
  const kw = keyword.value.trim().toLowerCase()
  return list.filter((it) => {
    if (catFilter.value !== 'all' && it.category !== catFilter.value) return false
    if (!kw) return true
    return [it.product, it.version, it.fileName, it.logicalPath, it.sha256, it.arch].join(' ').toLowerCase().includes(kw)
  })
})

const readyProductsCount = computed(() =>
  (catalog.value?.readinessMatrix || []).filter((r) => r.readyAmd64 || r.readyArm64).length
)

const categoryLabels: Record<string, string> = {
  binaries: '二进制/离线包',
  images: '容器镜像',
  bundles: '增量 Bundle',
  charts: 'Helm Chart',
  runtimes: '共享运行时',
}

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [cRes, hRes] = await Promise.all([
      apiRequest<CatalogResp>('GET', `/api/v1/projects/${ws.currentProjectId}/artifacts`),
      apiRequest<{ items: HostOption[] }>('GET', `/api/v1/projects/${ws.currentProjectId}/hosts`).catch(() => ({ data: { items: [] } })),
    ])
    catalog.value = cRes.data
    hosts.value = hRes.data.items || []
  } finally {
    loading.value = false
  }
}

function openUploadFor(product = 'valkey', version = '9.1.4', arch = 'amd64', category = 'binaries') {
  uploadDialog.product = product
  uploadDialog.version = version
  uploadDialog.arch = arch
  uploadDialog.category = category
  uploadDialog.file = null
  uploadDialog.open = true
}

function onFilePicked(ev: Event) {
  const input = ev.target as HTMLInputElement
  uploadDialog.file = input.files?.[0] || null
}

async function submitUpload() {
  if (!uploadDialog.file) {
    ElMessage.warning('请选择要上传的二进制、tar.gz、镜像包或 Helm Chart 文件')
    return
  }
  uploadDialog.uploading = true
  try {
    const fd = new FormData()
    fd.append('category', uploadDialog.category)
    fd.append('product', uploadDialog.product.trim() || 'generic')
    fd.append('version', uploadDialog.version.trim() || '1.0.0')
    fd.append('arch', uploadDialog.arch)
    fd.append('file', uploadDialog.file)
    const saved = await apiUpload<ArtifactEntry>(`/api/v1/projects/${ws.currentProjectId}/artifacts/upload`, fd)
    ElMessage.success(`已入库 ${saved.fileName} (SHA-256: ${saved.sha256.slice(0, 12)}…)`)
    uploadDialog.open = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    uploadDialog.uploading = false
  }
}

async function submitPrefetch() {
  if (!prefetchDialog.url.trim()) {
    ElMessage.warning('请输入要预拉取的 HTTP/HTTPS 制品地址')
    return
  }
  prefetchDialog.loading = true
  try {
    const { data } = await apiRequest<ArtifactEntry>('POST', `/api/v1/projects/${ws.currentProjectId}/artifacts/prefetch`, {
      body: {
        url: prefetchDialog.url.trim(),
        category: prefetchDialog.category,
        product: prefetchDialog.product.trim(),
        version: prefetchDialog.version.trim(),
        arch: prefetchDialog.arch,
        fileName: prefetchDialog.fileName.trim(),
      },
    })
    ElMessage.success(`已预拉取入库：${data.fileName} (${formatBytes(data.sizeBytes)})`)
    prefetchDialog.open = false
    prefetchDialog.url = ''
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    prefetchDialog.loading = false
  }
}

async function deleteItem(row: ArtifactEntry) {
  try {
    await ElMessageBox.confirm(`确认删除离线制品「${row.logicalPath}」吗？`, '删除制品', {
      type: 'warning',
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
    })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/artifacts?path=${encodeURIComponent(row.logicalPath)}`)
    ElMessage.success('制品已删除')
    await loadAll()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

async function exportBOM() {
  try {
    const res = await apiDownload('GET', `/api/v1/projects/${ws.currentProjectId}/artifacts/bom`, 'kinstall-bom-manifest.json')
    ElMessage.success(`已导出离线 BOM 清单 ${res.filename} (${res.size} B)`)
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openPushImage(row: ArtifactEntry) {
  pushImageDialog.logicalPath = row.logicalPath
  pushImageDialog.hostId = hosts.value[0]?.id || ''
  pushImageDialog.output = ''
  pushImageDialog.open = true
}

function openOciPush(row: ArtifactEntry) {
  ociPushDialog.logicalPath = row.logicalPath
  ociPushDialog.expectedArch = row.arch === 'arm64' ? 'arm64' : 'amd64'
  ociPushDialog.result = ''
  ociPushDialog.open = true
}

async function submitOciPush() {
  ociPushDialog.loading = true
  try {
    const { data } = await apiRequest<{ targetRef: string; digest: string; architecture: string; durationMs: number }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/artifacts/oci/push`,
      {
        body: {
          logicalPath: ociPushDialog.logicalPath,
          targetRef: ociPushDialog.targetRef,
          expectedArch: ociPushDialog.expectedArch,
          username: ociPushDialog.username,
          password: ociPushDialog.password,
          insecureTls: ociPushDialog.insecureTls,
        },
      }
    )
    ociPushDialog.result = `OCI 原生直推成功：${data.targetRef}\n架构校验通过：${data.architecture}\nDigest: ${data.digest} (${data.durationMs}ms)`
    ElMessage.success('原生 Go OCI 镜像架构校验并推送成功（零 docker CLI）')
  } catch (e) {
    ociPushDialog.result = String(e)
    ElMessage.error(String(e))
  } finally {
    ociPushDialog.loading = false
  }
}

async function submitPushImage() {
  if (!pushImageDialog.hostId) {
    ElMessage.warning('请选择目标主机')
    return
  }
  pushImageDialog.loading = true
  try {
    const { data } = await apiRequest<{ output: string; hostName: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/artifacts/push-image`,
      { body: { logicalPath: pushImageDialog.logicalPath, hostId: pushImageDialog.hostId } }
    )
    pushImageDialog.output = data.output || 'docker load 执行成功'
    ElMessage.success(`已成功推送并加载镜像至 ${data.hostName}`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    pushImageDialog.loading = false
  }
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="离线制品库"
      subtitle="管理二进制、tar.gz 离线包、容器镜像、Helm Chart 与共享运行时，提供 SHA-256 校验与组件齐套矩阵"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="exportBOM">
          <Download :size="14" class="btn-ico" /> 导出 BOM 清单
        </el-button>
        <el-button
          v-if="auth.can('media:write', ws.currentProjectId)"
          :disabled="catalog?.offlineStrict"
          @click="prefetchDialog.open = true"
        >
          <CloudDownload :size="14" class="btn-ico" /> 在线预拉取
        </el-button>
        <el-button
          v-if="auth.can('media:write', ws.currentProjectId)"
          type="primary"
          @click="openUploadFor()"
        >
          <Upload :size="14" class="btn-ico" /> 上传离线包
        </el-button>
      </template>
    </PageHeader>

    <div class="ki-chips">
      <div class="ki-chip" @click="activeTab = 'items'; catFilter = 'all'">
        <span class="ki-chip__bar" />
        <div>
          <div class="ki-chip__label">已入库制品总数</div>
          <div class="ki-chip__val">{{ catalog?.totalCount || 0 }}</div>
        </div>
      </div>
      <div class="ki-chip">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div>
          <div class="ki-chip__label">本地 CAS 介质占用</div>
          <div class="ki-chip__val">{{ formatBytes(catalog?.totalBytes || 0) }}</div>
        </div>
      </div>
      <div class="ki-chip" @click="activeTab = 'matrix'">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div>
          <div class="ki-chip__label">已具备离线包组件</div>
          <div class="ki-chip__val">{{ readyProductsCount }} / {{ catalog?.readinessMatrix?.length || 0 }}</div>
        </div>
      </div>
      <div class="ki-chip" @click="router.push('/artifacts/media-policies')">
        <span class="ki-chip__bar" :class="catalog?.offlineStrict ? 'ki-chip__bar--ok' : 'ki-chip__bar--warn'" />
        <div class="ki-chip__body">
          <div class="ki-chip__label">介质池存储根目录</div>
          <div class="ki-chip__path ki-mono" :title="catalog?.primaryRoot || '~/.kinstall/artifacts'">{{ catalog?.primaryRoot || '~/.kinstall/artifacts' }}</div>
        </div>
      </div>
    </div>

    <KiBanner
      v-if="catalog?.offlineStrict"
      variant="info"
      text="当前项目处于「严格物理离线模式」：在线预拉取已锁定；直接点击「上传离线包」入库后，部署插件将自动按产品/版本/架构匹配并校验 SHA-256 分发至目标机。"
      class="ki-mb-4"
    />

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`离线制品台账 (${filteredItems.length})`" name="items">
          <div class="filter-bar ki-mb-3">
            <el-radio-group v-model="catFilter" size="small">
              <el-radio-button value="all">全部</el-radio-button>
              <el-radio-button value="binaries">二进制/包</el-radio-button>
              <el-radio-button value="images">容器镜像</el-radio-button>
              <el-radio-button value="bundles">离线 Bundle</el-radio-button>
              <el-radio-button value="charts">Helm Chart</el-radio-button>
              <el-radio-button value="runtimes">共享运行时</el-radio-button>
            </el-radio-group>
            <el-input v-model="keyword" clearable placeholder="搜索产品、版本、文件名或 SHA-256…" class="search-input">
              <template #prefix><Search :size="14" /></template>
            </el-input>
          </div>

          <el-table :data="filteredItems" size="small">
            <el-table-column label="类别" width="104">
              <template #default="{ row }">
                <span class="ki-badge cat-pill">{{ categoryLabels[row.category] || row.category }}</span>
              </template>
            </el-table-column>
            <el-table-column label="产品 / 版本" min-width="136">
              <template #default="{ row }">
                <b class="ki-mono">{{ row.product }}</b>
                <span class="ki-muted ki-mono"> · {{ row.version }}</span>
              </template>
            </el-table-column>
            <el-table-column label="架构" width="76">
              <template #default="{ row }">
                <span class="ki-mono">{{ row.arch }}{{ row.pageSize === 65536 ? ' 64K' : '' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="文件名与路径" min-width="180">
              <template #default="{ row }">
                <div class="ki-mono">{{ row.fileName }}</div>
                <div class="ki-muted ki-mono sub-path">{{ row.logicalPath }}</div>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="78">
              <template #default="{ row }"><span class="ki-mono">{{ formatBytes(row.sizeBytes) }}</span></template>
            </el-table-column>
            <el-table-column label="SHA-256" width="150">
              <template #default="{ row }"><KiMonoText :value="row.sha256" :head="8" :tail="5" /></template>
            </el-table-column>
            <el-table-column label="入库时间" width="136">
              <template #default="{ row }"><span class="ki-mono ki-muted">{{ formatAbsTime(row.modifiedAt) }}</span></template>
            </el-table-column>
            <el-table-column label="操作" width="124" align="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.category === 'images'"
                  link
                  type="primary"
                  size="small"
                  @click="openOciPush(row)"
                >
                  OCI 直推
                </el-button>
                <el-button
                  v-if="row.category === 'images'"
                  link
                  type="primary"
                  size="small"
                  @click="openPushImage(row)"
                >
                  <Send :size="12" class="btn-ico" /> 推送主机
                </el-button>
                <el-button link type="danger" size="small" @click="deleteItem(row)">
                  <Trash2 :size="12" class="btn-ico" /> 删除
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <div class="empty-box">
                <HardDrive :size="28" class="ki-muted" />
                <p>离线制品池暂无已入库文件</p>
                <p class="ki-muted empty-hint">
                  点击右上角「上传离线包」上传任意二进制、`.tar.gz`、容器镜像 `.tar` 或业务应用包，或将文件直接放入 <code>{{ catalog?.primaryRoot }}</code> 后刷新。
                </p>
                <el-button type="primary" @click="openUploadFor()">
                  <Upload :size="14" class="btn-ico" /> 立即上传离线包
                </el-button>
              </div>
            </template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="`组件离线齐套度矩阵 (${catalog?.readinessMatrix?.length || 0})`" name="matrix">
          <el-table :data="catalog?.readinessMatrix || []" size="small">
            <el-table-column prop="product" label="组件产品" min-width="140">
              <template #default="{ row }"><b class="ki-mono">{{ row.product }}</b></template>
            </el-table-column>
            <el-table-column prop="defaultVersion" label="默认版本" width="110">
              <template #default="{ row }"><span class="ki-mono">{{ row.defaultVersion }}</span></template>
            </el-table-column>
            <el-table-column label="支持形态" min-width="190">
              <template #default="{ row }">
                <span v-for="t in row.targets" :key="t" class="ki-badge target-pill">{{ t }}</span>
              </template>
            </el-table-column>
            <el-table-column label="AMD64 离线包" width="120">
              <template #default="{ row }">
                <KiStatusBadge :status="row.readyAmd64 ? 'ok' : 'unprobed'" :label="row.readyAmd64 ? '已就绪' : '待上传'" />
              </template>
            </el-table-column>
            <el-table-column label="ARM64 离线包" width="120">
              <template #default="{ row }">
                <KiStatusBadge :status="row.readyArm64 ? 'ok' : 'unprobed'" :label="row.readyArm64 ? '已就绪' : '待上传'" />
              </template>
            </el-table-column>
            <el-table-column label="已入库数" width="88" align="center">
              <template #default="{ row }"><b class="ki-mono">{{ row.artifactCount }}</b></template>
            </el-table-column>
            <el-table-column label="快捷入库" width="170" fixed="right" align="right">
              <template #default="{ row }">
                <el-button link type="primary" size="small" @click="openUploadFor(row.product, row.defaultVersion, 'amd64', 'binaries')">
                  上传 AMD64
                </el-button>
                <el-button link type="primary" size="small" @click="openUploadFor(row.product, row.defaultVersion, 'arm64', 'binaries')">
                  上传 ARM64
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 上传离线包弹窗 -->
    <el-dialog v-model="uploadDialog.open" title="上传离线制品至本地仓库（自动计算 SHA-256）" width="520px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="制品类别">
            <el-select v-model="uploadDialog.category" class="ki-w-full">
              <el-option label="二进制 / tar.gz 安装包 (binaries)" value="binaries" />
              <el-option label="离线容器镜像包 .tar/.tar.zst (images)" value="images" />
              <el-option label="离线增量 Bundle (bundles)" value="bundles" />
              <el-option label="Helm Chart .tgz (charts)" value="charts" />
              <el-option label="共享运行时 JDK/Node (runtimes)" value="runtimes" />
            </el-select>
          </el-form-item>
          <el-form-item label="目标 CPU 架构">
            <el-select v-model="uploadDialog.arch" class="ki-w-full">
              <el-option label="x86_64 / amd64" value="amd64" />
              <el-option label="ARM64 / aarch64 (4K)" value="arm64" />
              <el-option label="ARM64 鲲鹏 64K 页 (arm64-64k)" value="arm64-64k" />
              <el-option label="架构无关 (noarch)" value="noarch" />
            </el-select>
          </el-form-item>
          <el-form-item label="组件 / 应用标识">
            <el-input v-model="uploadDialog.product" class="ki-mono" placeholder="valkey / nginx / his-app" />
          </el-form-item>
          <el-form-item label="版本号">
            <el-input v-model="uploadDialog.version" class="ki-mono" placeholder="9.1.4" />
          </el-form-item>
        </div>
        <el-form-item label="选择文件（支持最大 2 GiB 流式入库）">
          <input type="file" class="file-input" @change="onFilePicked" />
          <div v-if="uploadDialog.file" class="ki-muted ki-mono ki-mt-1">
            已选择：{{ uploadDialog.file.name }} ({{ formatBytes(uploadDialog.file.size) }})
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="uploadDialog.open = false">取消</el-button>
        <el-button type="primary" :loading="uploadDialog.uploading" @click="submitUpload">上传并入库</el-button>
      </template>
    </el-dialog>

    <!-- 在线预拉取弹窗 -->
    <el-dialog v-model="prefetchDialog.open" title="在线预拉取制品至离线仓库" width="520px">
      <el-form label-position="top">
        <el-form-item label="制品下载 URL (HTTP / HTTPS)">
          <el-input v-model="prefetchDialog.url" class="ki-mono" placeholder="https://mirror.internal/valkey-9.1.4-linux-amd64.tar.gz" />
        </el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="类别">
            <el-select v-model="prefetchDialog.category" class="ki-w-full">
              <el-option label="二进制/安装包 (binaries)" value="binaries" />
              <el-option label="容器镜像包 (images)" value="images" />
              <el-option label="Helm Chart (charts)" value="charts" />
              <el-option label="共享运行时 (runtimes)" value="runtimes" />
            </el-select>
          </el-form-item>
          <el-form-item label="架构">
            <el-select v-model="prefetchDialog.arch" class="ki-w-full">
              <el-option label="amd64" value="amd64" />
              <el-option label="arm64" value="arm64" />
              <el-option label="noarch" value="noarch" />
            </el-select>
          </el-form-item>
          <el-form-item label="产品名"><el-input v-model="prefetchDialog.product" class="ki-mono" /></el-form-item>
          <el-form-item label="版本"><el-input v-model="prefetchDialog.version" class="ki-mono" /></el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="prefetchDialog.open = false">取消</el-button>
        <el-button type="primary" :loading="prefetchDialog.loading" @click="submitPrefetch">开始预拉取</el-button>
      </template>
    </el-dialog>

    <!-- 推送离线镜像至主机弹窗 -->
    <el-dialog v-model="pushImageDialog.open" title="推送离线容器镜像并执行 docker load" width="520px">
      <el-form label-position="top">
        <el-form-item label="离线镜像文件">
          <el-input :model-value="pushImageDialog.logicalPath" disabled class="ki-mono" />
        </el-form-item>
        <el-form-item label="目标 CMDB 主机">
          <el-select v-model="pushImageDialog.hostId" class="ki-w-full" placeholder="选择目标 Linux 主机">
            <el-option v-for="h in hosts" :key="h.id" :label="`${h.name} (${h.address})`" :value="h.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <pre v-if="pushImageDialog.output" class="ki-mono load-out">{{ pushImageDialog.output }}</pre>
      <template #footer>
        <el-button @click="pushImageDialog.open = false">关闭</el-button>
        <el-button type="primary" :loading="pushImageDialog.loading" @click="submitPushImage">推送并加载镜像</el-button>
      </template>
    </el-dialog>

    <!-- 纯 Go 原生 OCI 架构校验直推 Harbor/SWR 弹窗 -->
    <el-dialog v-model="ociPushDialog.open" title="纯 Go 原生 OCI 架构校验直推（零 docker CLI）" width="540px">
      <el-form label-position="top">
        <el-form-item label="离线镜像 Tarball">
          <el-input :model-value="ociPushDialog.logicalPath" disabled class="ki-mono" />
        </el-form-item>
        <div class="ki-grid-2">
          <el-form-item label="目标 Harbor/SWR 引用">
            <el-input v-model="ociPushDialog.targetRef" class="ki-mono" />
          </el-form-item>
          <el-form-item label="强制校验目标架构（防混推）">
            <el-select v-model="ociPushDialog.expectedArch" class="ki-w-full">
              <el-option label="amd64 (x86_64)" value="amd64" />
              <el-option label="arm64 (鲲鹏/飞腾)" value="arm64" />
            </el-select>
          </el-form-item>
        </div>
        <div class="ki-grid-2">
          <el-form-item label="仓库用户名（可选）"><el-input v-model="ociPushDialog.username" /></el-form-item>
          <el-form-item label="仓库密码（可选）"><el-input v-model="ociPushDialog.password" type="password" show-password /></el-form-item>
        </div>
        <el-checkbox v-model="ociPushDialog.insecureTls">允许自签名内网 TLS 仓库</el-checkbox>
      </el-form>
      <pre v-if="ociPushDialog.result" class="ki-mono load-out">{{ ociPushDialog.result }}</pre>
      <template #footer>
        <el-button @click="ociPushDialog.open = false">关闭</el-button>
        <el-button type="primary" :loading="ociPushDialog.loading" @click="submitOciPush">校验架构并直推仓库</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.ki-chip__body {
  min-width: 0;
  flex: 1;
}

.ki-chip__path {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-primary);
  margin-top: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.search-input {
  width: 280px;
}

.cat-pill,
.target-pill {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
  margin-right: 4px;
}

.sub-path {
  font-size: 11px;
}

.empty-box {
  padding: 28px 16px;
  text-align: center;
}

.empty-hint {
  max-width: 560px;
  margin: 0 auto 14px;
  font-size: var(--ki-fs-xs);
}

.file-input {
  width: 100%;
  padding: 8px;
  border: 1px dashed var(--ki-border-control);
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-primary);
}

.load-out {
  margin: 10px 0 0;
  padding: 10px 12px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-terminal);
  color: var(--ki-term-fg);
  font-size: var(--ki-fs-xs);
  overflow: auto;
}
</style>
