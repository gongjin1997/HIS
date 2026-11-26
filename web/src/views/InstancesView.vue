<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import KiTargetBadge from '@/components/KiTargetBadge.vue'
import KiStatusBadge from '@/components/KiStatusBadge.vue'
import DynamicSchemaForm, { type PluginJSONSchema } from '@/components/DynamicSchemaForm.vue'
import ThreeWayDiff from '@/components/ThreeWayDiff.vue'

interface ProductRange {
  product: string
  versions: string
  defaultVersion?: string
  targets: string[]
  licenseTier?: string
}

interface PluginItem {
  name: string
  version: string
  description: string
  products?: ProductRange[]
}

const ws = useWorkspaceStore()
const router = useRouter()

const plugins = ref<PluginItem[]>([])
const instances = ref<Array<Record<string, unknown>>>([])
const activeTab = ref('instances')
const showWizard = ref(false)
const showDiff = ref(false)
const activePlan = ref<{ instanceId: string; instanceName: string; planId: string; summary: string } | null>(null)

const wizard = reactive({
  name: '',
  product: 'valkey',
  productVersion: '9.1.4',
  deployTarget: 'host-binary',
  spec: {} as Record<string, unknown>,
})

const dynamicSchema: PluginJSONSchema = {
  title: '实例核心参数',
  properties: {
    topology: { type: 'string', enum: ['standalone', 'replication-sentinel', 'cluster'], default: 'replication-sentinel', 'x-kinstall-ui': { group: 'basic', order: 1, label: '高可用拓扑' } },
    port: { type: 'integer', default: 6379, minimum: 1024, maximum: 55535, 'x-kinstall-ui': { group: 'network', order: 2, label: '监听端口' } },
    tlsEnabled: { type: 'boolean', default: true, 'x-kinstall-ui': { group: 'security', order: 3, label: '启用全链路 TLS 1.3' } },
    credentialRef: { type: 'string', default: '', 'x-kinstall-ui': { group: 'security', order: 4, widget: 'credential-select', label: '凭据库引用 ID' } },
  },
}

async function loadData() {
  if (!ws.currentProjectId) return
  const [pRes, iRes] = await Promise.all([
    apiRequest<{ items: PluginItem[] }>('GET', '/api/v1/plugins'),
    apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/instances`),
  ])
  plugins.value = pRes.data.items || []
  instances.value = iRes.data.items || []
}

function openDeploy(prod: ProductRange) {
  wizard.product = prod.product
  wizard.productVersion = prod.defaultVersion || '1.0.0'
  wizard.deployTarget = prod.targets?.[0] || 'host-binary'
  wizard.name = `${prod.product}-${ws.currentEnv}`
  showWizard.value = true
}

async function submitCreateInstance() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/instances`, {
      body: {
        env: ws.currentEnv,
        name: wizard.name,
        product: wizard.product,
        productVersion: wizard.productVersion,
        deployTarget: wizard.deployTarget,
        desiredSpec: wizard.spec,
      },
      idempotent: true,
    })
    ElMessage.success('实例已创建，可执行 Plan 审查')
    showWizard.value = false
    activeTab.value = 'instances'
    await loadData()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function runPlan(inst: Record<string, unknown>) {
  try {
    const { data } = await apiRequest<{ plan: { id: string } }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/instances/${inst.id}:plan`,
      { body: { action: 'install' } }
    )
    activePlan.value = {
      instanceId: String(inst.id),
      instanceName: String(inst.name),
      planId: data.plan.id,
      summary: `${inst.product} ${inst.productVersion} (${inst.deployTarget})`,
    }
    showDiff.value = true
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function applyPlan(forceOverride = false) {
  if (!activePlan.value) return
  try {
    const { data } = await apiRequest<{ id: string; state: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/instances/${activePlan.value.instanceId}:apply`,
      {
        body: { planId: activePlan.value.planId, action: 'install', forceOverride },
        idempotent: true,
      }
    )
    showDiff.value = false
    await ws.refreshApprovalBadge()
    if (data.state === 'awaiting_approval') {
      ElMessage.warning('生产环境变更已进入四眼审批队列')
      await router.push('/operations/approvals')
    } else {
      ElMessage.success(`操作 ${data.id} 已入队执行`)
      await router.push(`/operations?id=${data.id}`)
    }
  } catch (e) {
    ElMessage.error(String(e))
  }
}

watch(() => ws.currentProjectId, loadData)
onMounted(loadData)
</script>

<template>
  <div>
    <div class="ki-page-header">
      <div>
        <h1 class="ki-page-title">实例管理与全栈插件市场</h1>
        <div class="ki-muted" style="font-size: 12px">两段式 Plan → 3-Way Diff → Approve → Apply 变更工作流 + JSON-Schema 动态表单</div>
      </div>
      <el-button type="primary" @click="activeTab = 'catalog'">浏览插件市场 ({{ plugins.length }})</el-button>
    </div>

    <el-tabs v-model="activeTab">
      <el-tab-pane label="项目实例列表" name="instances">
        <div class="ki-panel">
          <el-table :data="instances">
            <el-table-column prop="name" label="实例名称">
              <template #default="{ row }"><span class="ki-mono" style="font-weight: 600">{{ row.name }}</span></template>
            </el-table-column>
            <el-table-column prop="product" label="组件" width="130" />
            <el-table-column prop="productVersion" label="版本" width="100">
              <template #default="{ row }"><span class="ki-mono">{{ row.productVersion }}</span></template>
            </el-table-column>
            <el-table-column prop="deployTarget" label="部署形态" width="150">
              <template #default="{ row }"><KiTargetBadge :target="String(row.deployTarget)" /></template>
            </el-table-column>
            <el-table-column prop="state" label="状态" width="130">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state)" /></template>
            </el-table-column>
            <el-table-column label="两段式变更" width="160">
              <template #default="{ row }">
                <el-button size="small" type="primary" @click="runPlan(row)">Plan & Diff</el-button>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </el-tab-pane>

      <el-tab-pane label="插件市场 (P1-P10 全生态)" name="catalog">
        <div class="ki-grid-2">
          <div v-for="plg in plugins" :key="plg.name" class="ki-panel">
            <div class="ki-flex-between" style="margin-bottom: 6px">
              <span class="ki-mono" style="font-weight: 600; font-size: 14px">{{ plg.name }}</span>
              <span class="ki-badge ki-mono" style="background: var(--ki-bg-subtle); color: var(--ki-text-secondary)">v{{ plg.version }}</span>
            </div>
            <div class="ki-secondary" style="font-size: 12px; margin-bottom: 10px">{{ plg.description }}</div>
            <div style="display: flex; flex-wrap: wrap; gap: 6px">
              <el-button
                v-for="pr in plg.products || []"
                :key="pr.product"
                size="small"
                plain
                @click="openDeploy(pr)"
              >
                {{ pr.licenseTier === 'red' ? '🔴' : pr.licenseTier === 'yellow' ? '🟡' : '🟢' }}
                {{ pr.product }} {{ pr.defaultVersion || '' }}
              </el-button>
            </div>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="showWizard" :title="`部署 ${wizard.product} [${ws.currentEnv.toUpperCase()}]`" width="680px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="实例名"><el-input v-model="wizard.name" class="ki-mono" /></el-form-item>
          <el-form-item label="目标版本"><el-input v-model="wizard.productVersion" class="ki-mono" /></el-form-item>
        </div>
        <el-form-item label="三元部署形态 (ADR-066)">
          <el-radio-group v-model="wizard.deployTarget">
            <el-radio-button value="host-binary">host-binary</el-radio-button>
            <el-radio-button value="host-container">host-container</el-radio-button>
            <el-radio-button value="kubernetes">kubernetes</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <DynamicSchemaForm v-model="wizard.spec" :schema="dynamicSchema" />
      <template #footer>
        <el-button @click="showWizard = false">取消</el-button>
        <el-button type="primary" @click="submitCreateInstance">创建实例</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showDiff" title="Plan 预检与 3-Way Diff 审查" width="900px">
      <div v-if="activePlan" style="margin-bottom: 12px" class="ki-mono">
        Plan ID: {{ activePlan.planId }} · {{ activePlan.summary }}
      </div>
      <ThreeWayDiff
        v-if="activePlan"
        :instance-name="activePlan.instanceName"
        :path="`/etc/kinstall/${activePlan.instanceName}/config.conf`"
        base-text="# baseline v1\nmaxmemory 4096mb\ntls-replication yes"
        live-text="# baseline v1\nmaxmemory 4096mb\ntls-replication yes"
        desired-text="# desired v2\nmaxmemory 9318mb\ntls-replication yes\naclfile users.acl"
        :has-drift="false"
        @decision="(m) => applyPlan(m === 'force-override')"
      />
      <template #footer>
        <el-button @click="showDiff = false">取消</el-button>
        <el-button type="primary" @click="applyPlan(false)">执行 Apply</el-button>
      </template>
    </el-dialog>
  </div>
</template>
