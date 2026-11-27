<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Save, CheckCircle2, Play, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiEnvBadge from '@/components/ki/KiEnvBadge.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'
import StackCanvas, { type StackNodeInput } from '@/components/StackCanvas.vue'

interface ResolvedDAG {
  installLayers: string[][]
  uninstallLayers: string[][]
  edges: Record<string, string[]>
  externalRefIds?: string[]
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()

const presets: Record<string, { label: string; name: string; yaml: string }> = {
  logging: {
    label: '日志分析栈 (S3 + Kafka + OpenSearch + DataPrepper)',
    name: 'prod-t3-logging-platform',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: prod-t3-logging-platform
spec:
  instances:
    - id: s3
      product: object-storage
      productVersion: "4.54.0"
      deployTarget: host-binary
    - id: kafka
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
    - id: opensearch
      product: opensearch
      productVersion: "3.10.0"
      deployTarget: host-binary
      dependsOn: [s3]
    - id: dataprepper
      product: elk-pipeline
      productVersion: "2.16.0"
      deployTarget: host-binary
      dependsOn: [kafka, opensearch, s3]
`,
  },
  hisCore: {
    label: '核心交易栈 (PostgreSQL + Valkey + Kafka)',
    name: 'his-core-app-stack',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: his-core-app-stack
spec:
  instances:
    - id: pg
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: mq
      product: kafka
      productVersion: "4.3.2"
      deployTarget: host-binary
      dependsOn: [pg, cache]
`,
  },
  devops: {
    label: '研发交付栈 (PG + Valkey + GitLab + Harbor)',
    name: 'devops-platform-stack',
    yaml: `apiVersion: kinstall.io/v1alpha1
kind: Stack
metadata:
  name: devops-platform-stack
spec:
  instances:
    - id: db
      product: postgresql
      productVersion: "18.3"
      deployTarget: host-binary
    - id: cache
      product: valkey
      productVersion: "9.1.4"
      deployTarget: host-binary
    - id: gitlab
      product: gitlab
      productVersion: "18.10.0"
      deployTarget: host-container
      dependsOn: [db, cache]
    - id: harbor
      product: harbor
      productVersion: "2.15.0"
      deployTarget: host-container
      dependsOn: [db, cache]
`,
  },
}

const loading = ref(false)
const validating = ref(false)
const stacks = ref<Array<Record<string, unknown>>>([])
const stackName = ref(presets.logging.name)
const specYaml = ref(presets.logging.yaml)
const activePreset = ref('logging')
const canvasNodes = ref<StackNodeInput[]>([])
const resolvedDag = ref<ResolvedDAG | null>(null)
const validateError = ref('')

const uninstallModal = reactive({
  open: false,
  stack: null as Record<string, unknown> | null,
  loading: false,
})

async function validateAndRender() {
  if (!ws.currentProjectId || !specYaml.value.trim()) return
  validating.value = true
  validateError.value = ''
  try {
    const { data } = await apiRequest<{ dag: ResolvedDAG; nodes: StackNodeInput[]; valid: boolean }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/stacks:validate`,
      { body: { specYaml: specYaml.value } }
    )
    resolvedDag.value = data.dag
    canvasNodes.value = (data.nodes || []).map((n) => ({ ...n, status: 'ready' }))
  } catch (e) {
    validateError.value = String(e)
    resolvedDag.value = null
  } finally {
    validating.value = false
  }
}

function selectPreset(key: string) {
  const p = presets[key]
  if (!p) return
  activePreset.value = key
  stackName.value = p.name
  specYaml.value = p.yaml
  void validateAndRender()
}

function loadStackIntoEditor(row: Record<string, unknown>) {
  activePreset.value = ''
  stackName.value = String(row.name || '')
  if (typeof row.specYaml === 'string' && row.specYaml.trim()) {
    specYaml.value = row.specYaml
    void validateAndRender()
  }
}

async function loadStacks() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const { data } = await apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/stacks`)
    stacks.value = data.items || []
    await validateAndRender()
  } finally {
    loading.value = false
  }
}

async function createStack() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/stacks`, {
      body: { env: ws.currentEnv, name: stackName.value, specYaml: specYaml.value },
      idempotent: true,
    })
    ElMessage.success('应用栈拓扑与输出参数校验通过，已保存')
    await loadStacks()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function applyStack(row: Record<string, unknown>) {
  try {
    const { data } = await apiRequest<{ id: string; state: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/stacks/${row.id}:apply`,
      { idempotent: true }
    )
    await ws.refreshApprovalBadge()
    if (data.state === 'awaiting_approval') {
      ElMessage.warning(`生产环境应用栈 ${row.name} 已进入变更审批队列`)
      await router.push('/operations/approvals')
    } else {
      ElMessage.success(`应用栈分层部署任务 ${data.id} 已下发`)
      await router.push(`/operations?id=${data.id}`)
    }
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function openUninstallStack(row: Record<string, unknown>) {
  uninstallModal.stack = row
  uninstallModal.open = true
}

async function confirmUninstallStack() {
  if (!uninstallModal.stack) return
  uninstallModal.loading = true
  try {
    const { data } = await apiRequest<{ id: string; state: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/stacks/${uninstallModal.stack.id}:uninstall`,
      { idempotent: true }
    )
    uninstallModal.open = false
    await ws.refreshApprovalBadge()
    if (data.state === 'awaiting_approval') {
      ElMessage.warning('逆序卸载已进入变更审批队列')
      await router.push('/operations/approvals')
    } else {
      ElMessage.success(`逆序卸载操作 ${data.id} 已入队（外部引用已自动保护）`)
      await router.push(`/operations?id=${data.id}`)
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    uninstallModal.loading = false
  }
}

watch(() => ws.currentProjectId, loadStacks)
onMounted(loadStacks)
</script>

<template>
  <div>
    <PageHeader title="应用栈编排" :refresh="loadStacks" :refreshing="loading" />

    <div class="ki-card ki-mb-4">
      <div class="ki-card__title">
        <span>复合应用栈拓扑画布<span class="sub">实时依赖分层校验 · 自动阻断循环依赖 · 插件输出参数自动关联</span></span>
        <div class="ki-flex-row">
          <el-button :loading="validating" @click="validateAndRender">
            <CheckCircle2 :size="14" class="btn-ico" /> 校验并渲染拓扑
          </el-button>
          <el-button v-if="auth.can('stack:write', ws.currentProjectId)" type="primary" @click="createStack">
            <Save :size="14" class="btn-ico" /> 校验并保存应用栈
          </el-button>
        </div>
      </div>

      <KiBanner v-if="validateError" variant="danger" :text="validateError" class="ki-mb-3" />

      <div v-if="resolvedDag" class="dag-layers ki-mb-3">
        <span class="ki-secondary">安装执行分层：</span>
        <span v-for="(layer, i) in resolvedDag.installLayers" :key="i" class="ki-badge ki-mono layer-pill">
          L{{ i }} · {{ layer.join(', ') }}
        </span>
        <span v-if="resolvedDag.externalRefIds?.length" class="ki-badge ki-mono ext-pill">
          受保护外部引用：{{ resolvedDag.externalRefIds.join(', ') }}
        </span>
      </div>

      <StackCanvas :instances="canvasNodes" />
    </div>

    <div class="ki-grid-2">
      <div class="ki-card">
        <div class="ki-card__title">
          <span>声明式 Stack YAML</span>
          <el-input v-model="stackName" size="small" class="ki-mono stack-name-input" />
        </div>
        <div class="ki-pill-tabs ki-mb-3">
          <button
            v-for="(p, key) in presets"
            :key="key"
            type="button"
            class="ki-pill-tab"
            :class="{ 'ki-pill-tab--active': activePreset === key }"
            @click="selectPreset(key)"
          >
            {{ p.label }}
          </button>
        </div>
        <el-input v-model="specYaml" type="textarea" :rows="14" class="ki-mono" />
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>已保存应用栈清单<span class="sub">共 {{ stacks.length }} 套 · 点击行载入上方画布</span></span>
        </div>
        <el-table v-loading="loading" :data="stacks" size="small" @row-click="loadStackIntoEditor">
          <el-table-column prop="name" label="应用栈名称" min-width="140">
            <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
          </el-table-column>
          <el-table-column label="环境" width="106">
            <template #default="{ row }"><KiEnvBadge :env="String(row.env || ws.currentEnv)" /></template>
          </el-table-column>
          <el-table-column label="状态" width="96">
            <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'ready')" /></template>
          </el-table-column>
          <el-table-column label="操作" width="148">
            <template #default="{ row }">
              <div class="ki-row-actions">
                <el-button
                  v-if="auth.can('stack:write', ws.currentProjectId)"
                  link
                  type="primary"
                  @click.stop="applyStack(row)"
                >
                  <Play :size="13" class="btn-ico" /> Apply
                </el-button>
                <el-button
                  v-if="auth.can('stack:write', ws.currentProjectId)"
                  link
                  type="danger"
                  @click.stop="openUninstallStack(row)"
                >
                  <Trash2 :size="13" class="btn-ico" /> 逆序卸载
                </el-button>
              </div>
            </template>
          </el-table-column>
          <template #empty><el-empty description="暂无已保存应用栈，点击上方按钮保存当前模板" :image-size="60" /></template>
        </el-table>
      </div>
    </div>

    <KiDangerConfirmModal
      v-model="uninstallModal.open"
      :env="String(uninstallModal.stack?.env || ws.currentEnv)"
      title="确认逆序卸载应用栈"
      :resource-name="String(uninstallModal.stack?.name || '')"
      risk-summary="将严格按依赖逆序分层卸载栈内自建实例；externalRefs 声明的外部托管服务绝不会被修改或删除。"
      confirm-label="解锁并逆序卸载"
      :loading="uninstallModal.loading"
      @confirm="confirmUninstallStack"
    />
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.stack-name-input {
  width: 230px;
}

.dag-layers {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: var(--ki-fs-xs);
}

.layer-pill {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.ext-pill {
  background-color: var(--ki-status-ok-subtle);
  color: var(--ki-status-ok-text);
}
</style>
