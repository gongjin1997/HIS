<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Blocks, Code2, Plus, Rocket, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import PageHeader from '@/components/PageHeader.vue'

interface PluginPackage {
  id: string
  code: string
  name: string
  category: string
  version: string
  runtime: string
  description: string
  isBuiltin: boolean
  manifestYaml: string
  schema: Array<{ key: string; label: string; type: string; default: string }>
}

const router = useRouter()
const loading = ref(false)
const plugins = ref<PluginPackage[]>([])
const activeCat = ref('all')

const showEditor = ref(false)
const editorTitle = ref('新建声明式 YAML 插件包')
const yamlDraft = ref('')

const categories = [
  { key: 'all', label: '全部插件' },
  { key: 'database', label: '数据库' },
  { key: 'middleware', label: '中间件与网关' },
  { key: 'storage', label: '对象存储' },
  { key: 'application', label: '业务应用' },
  { key: 'observability', label: '监控采集' },
]

const filteredPlugins = computed(() =>
  activeCat.value === 'all'
    ? plugins.value
    : plugins.value.filter((p) => p.category === activeCat.value)
)

const starterYAML = `apiVersion: kinstall.io/v2
code: custom-service
name: 自定义业务微服务插件
category: application
version: "1.0.0"
runtime: compose
cmdbModel: m_app
defaultPort: 8088
description: 通过 YAML Manifest 声明参数 Schema、安装脚本与健康拨测，无需编写 Go 代码
params:
  - key: APP_PORT
    label: 监听端口
    type: int
    default: "8088"
  - key: CPU_CORES
    label: CPU 核数
    type: float
    default: "2"
  - key: MEM_GB
    label: 内存 (GB)
    type: float
    default: "4"
installScript: |
  echo "[Plugin:custom-service] Deploying {{SERVICE_NAME}} on port {{APP_PORT}}"
healthCheck:
  type: http
  port: "{{APP_PORT}}"
  path: /healthz
uninstallScript: |
  echo "[Plugin:custom-service] Removing {{SERVICE_NAME}}"
`

async function loadPlugins() {
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: PluginPackage[] }>('GET', '/api/v1/delivery/plugins')
    plugins.value = data.items || []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editorTitle.value = '新建声明式 YAML 插件包'
  yamlDraft.value = starterYAML
  showEditor.value = true
}

function openEdit(p: PluginPackage) {
  editorTitle.value = `编辑插件声明 · ${p.name}`
  yamlDraft.value = p.manifestYaml
  showEditor.value = true
}

async function savePlugin() {
  try {
    await apiRequest('POST', '/api/v1/delivery/plugins', { body: { manifestYaml: yamlDraft.value } })
    ElMessage.success('声明式 YAML 插件已保存生效')
    showEditor.value = false
    await loadPlugins()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'YAML 校验失败')
  }
}

async function deletePlugin(p: PluginPackage) {
  await ElMessageBox.confirm(`确认删除插件「${p.name}」？`, '删除插件', { type: 'warning' })
  await apiRequest('DELETE', `/api/v1/delivery/plugins/${p.code}`)
  ElMessage.success('插件已删除')
  await loadPlugins()
}

onMounted(loadPlugins)
</script>

<template>
  <div class="plugins-page">
    <PageHeader
      title="声明式 YAML 插件库"
      subtitle="参考 1Panel / LiveOps IaC · 零 Go 代码扩展组件与业务应用包"
      :refresh="loadPlugins"
      :refreshing="loading"
    >
      <template #actions>
        <el-button type="primary" @click="openCreate"><Plus :size="14" class="btn-ico" />新建 YAML 插件</el-button>
      </template>
    </PageHeader>

    <div class="ki-pill-tabs cat-bar">
      <button
        v-for="c in categories"
        :key="c.key"
        type="button"
        class="ki-pill-tab"
        :class="{ 'ki-pill-tab--active': activeCat === c.key }"
        @click="activeCat = c.key"
      >
        {{ c.label }}
      </button>
    </div>

    <div class="plg-grid">
      <article v-for="p in filteredPlugins" :key="p.id" class="ki-card plg-card">
        <div class="plg-head">
          <div class="plg-title">
            <Blocks :size="18" class="plg-ico" />
            <div>
              <h3>{{ p.name }}</h3>
              <span class="ki-mono ki-muted">{{ p.code }} · v{{ p.version }}</span>
            </div>
          </div>
          <el-tag size="small">{{ p.runtime }}</el-tag>
        </div>

        <p class="plg-desc">{{ p.description }}</p>

        <div class="plg-meta ki-muted">
          <span>分类: {{ p.category }}</span>
          <span>参数项: {{ p.schema?.length || 0 }} 个</span>
          <span>{{ p.isBuiltin ? '官方内置' : '自定义扩展' }}</span>
        </div>

        <div class="plg-foot">
          <el-button size="small" @click="openEdit(p)"><Code2 :size="13" class="btn-ico" />YAML 源码</el-button>
          <el-button size="small" type="primary" plain @click="router.push(`/delivery?plugin=${p.code}`)">
            <Rocket :size="13" class="btn-ico" />发起编排
          </el-button>
          <el-button v-if="!p.isBuiltin" link type="danger" size="small" @click="deletePlugin(p)">
            <Trash2 :size="13" />
          </el-button>
        </div>
      </article>
    </div>

    <el-dialog v-model="showEditor" :title="editorTitle" width="760px">
      <p class="ki-muted yaml-tip">
        支持参数模板变量（如 <code v-pre class="ki-mono">{{SERVICE_NAME}}</code>、<code v-pre class="ki-mono">{{PG_PORT}}</code>），发布时由三级参数引擎自动渲染并同步登记 CMDB 资产。
      </p>
      <el-input v-model="yamlDraft" type="textarea" :rows="20" class="yaml-area" />
      <template #footer>
        <el-button @click="showEditor = false">取消</el-button>
        <el-button type="primary" @click="savePlugin">校验并保存插件</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.cat-bar {
  margin-bottom: var(--ki-space-4);
}

.plg-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--ki-space-3);
}

.plg-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.plg-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}

.plg-title {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.plg-title h3 {
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.plg-ico {
  flex: none;
  margin-top: 2px;
  color: var(--ki-brand);
}

.plg-desc {
  margin: 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
  line-height: 1.5;
  flex: 1;
}

.plg-meta {
  display: flex;
  justify-content: space-between;
  font-size: var(--ki-fs-xs);
  padding-top: 8px;
  border-top: 1px solid var(--ki-border-subtle);
}

.plg-foot {
  display: flex;
  align-items: center;
  gap: 8px;
}

.yaml-tip {
  margin: 0 0 10px;
  font-size: var(--ki-fs-xs);
}

.yaml-area {
  font-family: var(--ki-font-mono);
}

@media (max-width: 1080px) {
  .plg-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .plg-grid {
    grid-template-columns: 1fr;
  }
}
</style>
