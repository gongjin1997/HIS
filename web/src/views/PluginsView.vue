<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Code2, Plus, Puzzle, Rocket, Trash2 } from 'lucide-vue-next'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'

interface DeclarativePlugin {
  id: string
  name: string
  displayName: string
  category: string
  version: string
  description: string
  targets: string[]
  manifestYaml: string
  isEnabled: boolean
  createdBy: string
}

interface BuiltinPlugin {
  name: string
  version: string
  description: string
  products?: Array<{ product: string; defaultVersion: string; targets: string[] }>
}

const ws = useWorkspaceStore()
const router = useRouter()
const loading = ref(false)
const activeTab = ref<'declarative' | 'builtin'>('declarative')
const declPlugins = ref<DeclarativePlugin[]>([])
const builtinPlugins = ref<BuiltinPlugin[]>([])

const starterYAML = `apiVersion: kinstall.io/v1alpha1
kind: PluginPackage
name: custom-app-service
displayName: 自定义业务服务容器包
category: 业务应用
version: 1.0.0
icon: Package
description: 通过声明式 YAML 定义参数 Schema、Docker Compose 模板与健康探针，无需修改任何 Go 代码
targets:
  - host-container
params:
  title: 自定义业务服务参数
  properties:
    image:
      type: string
      default: nginx:1.27-alpine
      x-kinstall-ui: { group: basic, order: 1, label: 容器镜像 }
    port:
      type: integer
      default: 8089
      x-kinstall-ui: { group: network, order: 2, label: 服务端口 }
composeTemplate: |
  services:
    app:
      image: "{{ .image }}"
      container_name: "kinstall-{{ .InstanceName }}"
      restart: unless-stopped
      ports:
        - "{{ .port }}:80"
healthCheck:
  tcpPort: 8089
  timeout: 30s
`

const editorDialog = reactive({
  open: false,
  title: '新建声明式插件包 (YAML)',
  manifestYaml: starterYAML,
  isEnabled: true,
  saving: false,
})

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [dRes, bRes] = await Promise.all([
      apiRequest<{ items: DeclarativePlugin[] }>('GET', `/api/v1/projects/${ws.currentProjectId}/plugins/declarative`),
      apiRequest<{ items: BuiltinPlugin[] }>('GET', '/api/v1/plugins'),
    ])
    declPlugins.value = dRes.data.items || []
    builtinPlugins.value = bRes.data.items || []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editorDialog.title = '新建声明式插件包 (YAML Manifest)'
  editorDialog.manifestYaml = starterYAML
  editorDialog.isEnabled = true
  editorDialog.open = true
}

function openEdit(p: DeclarativePlugin) {
  editorDialog.title = `编辑声明式插件包 · ${p.displayName}`
  editorDialog.manifestYaml = p.manifestYaml
  editorDialog.isEnabled = p.isEnabled
  editorDialog.open = true
}

async function savePlugin() {
  editorDialog.saving = true
  try {
    const { data } = await apiRequest<DeclarativePlugin>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/plugins/declarative`,
      { body: { manifestYaml: editorDialog.manifestYaml, isEnabled: editorDialog.isEnabled } }
    )
    ElMessage.success(`声明式插件「${data.displayName} (${data.name})」已保存并热注册至执行引擎！`)
    editorDialog.open = false
    await loadAll()
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    editorDialog.saving = false
  }
}

async function deletePlugin(p: DeclarativePlugin) {
  try {
    await ElMessageBox.confirm(`确认删除声明式插件「${p.displayName}」吗？`, '删除插件', { type: 'warning' })
    await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/plugins/declarative/${encodeURIComponent(p.id)}`)
    ElMessage.success('插件包已删除并从引擎卸载')
    await loadAll()
  } catch (e) {
    if (e !== 'cancel' && String(e) !== 'cancel') ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadAll)
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="声明式插件与模板市场"
      subtitle="YAML Manifest 声明业务应用/中间件模板与探针，保存即热注册生效"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="router.push('/delivery')">
          <Rocket :size="14" class="btn-ico" /> 前往分批交付
        </el-button>
        <el-button type="primary" @click="openCreate">
          <Plus :size="14" class="btn-ico" /> 新建 YAML 插件包
        </el-button>
      </template>
    </PageHeader>

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`声明式可扩展插件包 (${declPlugins.length})`" name="declarative">
          <div class="plug-grid">
            <div v-for="p in declPlugins" :key="p.id" class="plug-card">
              <div class="plug-card__head">
                <div class="ki-flex-row">
                  <Puzzle :size="16" class="plug-ico" />
                  <b>{{ p.displayName }}</b>
                </div>
                <span class="ki-badge ki-mono ver-pill">v{{ p.version }}</span>
              </div>
              <div class="ki-mono ki-muted plug-name">{{ p.name }} · {{ p.category }}</div>
              <p class="ki-muted plug-desc">{{ p.description }}</p>
              <div class="plug-targets">
                <span v-for="t in p.targets" :key="t" class="ki-badge target-pill ki-mono">{{ t }}</span>
              </div>
              <div class="plug-foot">
                <el-button size="small" @click="openEdit(p)">
                  <Code2 :size="13" class="btn-ico" /> 编辑 YAML
                </el-button>
                <div class="ki-flex-row">
                  <el-button type="primary" size="small" @click="router.push('/instances')">去部署</el-button>
                  <el-button link type="danger" size="small" @click="deletePlugin(p)">
                    <Trash2 :size="13" />
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="`内置核心中间件引擎 (${builtinPlugins.length})`" name="builtin">
          <el-table :data="builtinPlugins" size="small">
            <el-table-column prop="name" label="引擎插件" width="160">
              <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
            </el-table-column>
            <el-table-column prop="version" label="版本" width="90">
              <template #default="{ row }"><span class="ki-mono">{{ row.version }}</span></template>
            </el-table-column>
            <el-table-column label="涵盖组件" min-width="200">
              <template #default="{ row }">
                <span v-for="pr in row.products || []" :key="pr.product" class="ki-badge target-pill ki-mono">{{ pr.product }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="description" label="能力说明" min-width="260" />
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="editorDialog.open" :title="editorDialog.title" width="740px">
      <div class="ki-flex-row ki-mb-2" style="justify-content: space-between;">
        <span class="ki-muted" style="font-size: var(--ki-fs-xs);">支持 Go 模板变量：<code v-pre>{{ .InstanceName }}</code> 及自定义 params 字段；保存后立即热注入部署引擎。</span>
        <el-switch v-model="editorDialog.isEnabled" active-text="立即热注册启用" />
      </div>
      <el-input v-model="editorDialog.manifestYaml" type="textarea" :rows="20" class="yaml-editor ki-mono" />
      <template #footer>
        <el-button @click="editorDialog.open = false">取消</el-button>
        <el-button type="primary" :loading="editorDialog.saving" @click="savePlugin">校验 YAML 并热注册</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.plug-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 14px;
}

.plug-card {
  padding: 16px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.plug-card__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.plug-ico {
  color: var(--ki-brand-primary);
}

.ver-pill,
.target-pill {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
  margin-right: 4px;
}

.plug-name {
  font-size: 11px;
}

.plug-desc {
  margin: 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.5;
  flex: 1;
}

.plug-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 10px;
  border-top: 1px solid var(--ki-border-subtle);
}

.yaml-editor {
  font-family: var(--ki-font-mono);
  font-size: var(--ki-fs-xs);
}
</style>
