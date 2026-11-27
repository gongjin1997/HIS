<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Search, GitCompare } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'
import KiDynamicSchemaForm, { type PluginJSONSchema } from '@/components/domain/KiDynamicSchemaForm.vue'
import KiThreeWayDiffViewer from '@/components/domain/KiThreeWayDiffViewer.vue'

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

const auth = useAuthStore()
const ws = useWorkspaceStore()
const router = useRouter()

const loading = ref(false)
const plugins = ref<PluginItem[]>([])
const instances = ref<Array<Record<string, unknown>>>([])
const hosts = ref<Array<{ id: string; name: string; address: string; arch?: string; pageSize?: number; faultZone?: string }>>([])
const activeTab = ref('instances')
const filterTarget = ref('ALL')
const keyword = ref('')
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
    deployTarget: { type: 'string', default: 'host-binary', 'x-kinstall-ui': { group: 'basic', order: 1, widget: 'deploy-target-cards', label: '三元部署形态 (ADR-066)' } },
    topology: { type: 'string', enum: ['standalone', 'replication-sentinel', 'cluster'], default: 'replication-sentinel', 'x-kinstall-ui': { group: 'basic', order: 2, label: '高可用拓扑' } },
    targetHosts: { type: 'string', default: '', 'x-kinstall-ui': { group: 'network', order: 3, widget: 'host-role-picker', label: '目标节点选择（按故障域分组）' } },
    podCidr: { type: 'string', default: '10.244.0.0/16', 'x-kinstall-ui': { group: 'network', order: 4, widget: 'cidr-allocator', label: '服务/集群网段 CIDR' } },
    port: { type: 'integer', default: 6379, minimum: 1024, maximum: 55535, 'x-kinstall-ui': { group: 'network', order: 5, label: '监听端口' } },
    tlsEnabled: { type: 'boolean', default: true, 'x-kinstall-ui': { group: 'security', order: 6, label: '启用全链路 TLS 1.3' } },
    credentialRef: { type: 'string', default: '', 'x-kinstall-ui': { group: 'security', order: 7, widget: 'credential-select', label: '加密凭据引用 ID' } },
    forceUnsafeSkipVerify: { type: 'boolean', default: false, 'x-kinstall-ui': { group: 'advanced', order: 8, label: '跳过远端自检校验', riskWarning: '高危选项：跳过预检可能导致节点内核或 64K 页不兼容故障！' } },
  },
}

const filteredInstances = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  return instances.value.filter((it) => {
    if (filterTarget.value !== 'ALL' && String(it.deployTarget || '') !== filterTarget.value) return false
    if (!q) return true
    return `${it.name} ${it.product} ${it.productVersion}`.toLowerCase().includes(q)
  })
})

async function loadData() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const [pRes, iRes, hRes] = await Promise.all([
      apiRequest<{ items: PluginItem[] }>('GET', '/api/v1/plugins'),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/instances`),
      apiRequest<{ items: Array<{ id: string; name: string; address: string; arch?: string; pageSize?: number; faultZone?: string }> }>('GET', `/api/v1/projects/${ws.currentProjectId}/hosts`).catch(() => ({ data: { items: [] } })),
    ])
    plugins.value = pRes.data.items || []
    instances.value = iRes.data.items || []
    hosts.value = hRes.data.items || []
  } finally {
    loading.value = false
  }
}

function openDeploy(prod: ProductRange) {
  wizard.product = prod.product
  wizard.productVersion = prod.defaultVersion || '1.0.0'
  wizard.deployTarget = prod.targets?.[0] || 'host-binary'
  wizard.name = `${prod.product}-${ws.currentEnv}`
  wizard.spec = { deployTarget: wizard.deployTarget }
  showWizard.value = true
}

function licenseLabel(tier?: string): string {
  if (tier === 'red') return 'SSPL/Commercial'
  if (tier === 'yellow') return 'GPL/MPL'
  return 'Apache-2.0/BSD'
}

async function submitCreateInstance() {
  try {
    await apiRequest('POST', `/api/v1/projects/${ws.currentProjectId}/instances`, {
      body: {
        env: ws.currentEnv,
        name: wizard.name,
        product: wizard.product,
        productVersion: wizard.productVersion,
        deployTarget: String(wizard.spec.deployTarget || wizard.deployTarget),
        desiredSpec: wizard.spec,
      },
      idempotent: true,
    })
    ElMessage.success('实例已创建，可执行 Plan 3-Way Diff 审查')
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
    <PageHeader title="实例与插件" :refresh="loadData" :refreshing="loading" />

    <!-- 点击筛选统计条 -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': filterTarget === 'ALL' }" @click="filterTarget = 'ALL'; activeTab = 'instances'">
        <span class="ki-chip__bar" />
        <div><div class="ki-chip__label">全部实例</div><div class="ki-chip__val ki-num">{{ instances.length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterTarget === 'host-binary' }" @click="filterTarget = 'host-binary'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--running" />
        <div><div class="ki-chip__label">主机二进制</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'host-binary').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterTarget === 'host-container' }" @click="filterTarget = 'host-container'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div><div class="ki-chip__label">单机容器</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'host-container').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': filterTarget === 'kubernetes' }" @click="filterTarget = 'kubernetes'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div><div class="ki-chip__label">Kubernetes 编排</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'kubernetes').length }}</div></div>
      </div>
    </div>

    <div class="ki-card">
      <div class="table-heading">
        <span class="ki-secondary">两段式 Plan → 3-Way Diff → 四眼审批 → Apply 交付工作流</span>
        <div class="table-actions">
          <el-button type="primary" @click="activeTab = 'catalog'">
            <Plus :size="14" class="btn-ico" /> 浏览插件市场 ({{ plugins.length }})
          </el-button>
        </div>
      </div>

      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`项目实例 (${filteredInstances.length})`" name="instances">
          <div class="filter-bar">
            <el-input v-model="keyword" clearable placeholder="搜索实例名 / 组件 / 版本" class="search-kw">
              <template #prefix><Search :size="14" /></template>
            </el-input>
          </div>
          <el-table v-loading="loading" :data="filteredInstances">
            <el-table-column prop="name" label="实例名称" min-width="180">
              <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
            </el-table-column>
            <el-table-column prop="product" label="组件" width="130" />
            <el-table-column label="版本" width="100">
              <template #default="{ row }"><span class="ki-mono">{{ row.productVersion }}</span></template>
            </el-table-column>
            <el-table-column label="交付形态" width="150">
              <template #default="{ row }"><KiDomainTag kind="target" :value="String(row.deployTarget || 'host-binary')" /></template>
            </el-table-column>
            <el-table-column label="状态" width="130">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'created')" /></template>
            </el-table-column>
            <el-table-column label="操作" width="160" fixed="right">
              <template #default="{ row }">
                <el-button
                  v-if="auth.can('instance:plan', ws.currentProjectId)"
                  size="small"
                  type="primary"
                  plain
                  @click="runPlan(row)"
                >
                  <GitCompare :size="13" class="btn-ico" /> Plan & Diff
                </el-button>
              </template>
            </el-table-column>
            <template #empty><el-empty description="当前项目暂无实例，请切换到插件市场一键部署" :image-size="70" /></template>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="`插件市场 P1–P10 (${plugins.length})`" name="catalog">
          <div class="ki-grid-2">
            <div v-for="plg in plugins" :key="plg.name" class="ki-surface plugin-card">
              <div class="ki-flex-between ki-mb-3">
                <strong class="ki-mono plugin-title">{{ plg.name }}</strong>
                <span class="ki-badge ki-mono plugin-ver">v{{ plg.version }}</span>
              </div>
              <p class="ki-secondary plugin-desc">{{ plg.description }}</p>
              <div class="plugin-prods">
                <div v-for="pr in plg.products || []" :key="pr.product" class="prod-row">
                  <div class="ki-flex-row">
                    <b class="ki-mono">{{ pr.product }}</b>
                    <span class="ki-mono ki-muted">{{ pr.defaultVersion || pr.versions }}</span>
                    <KiDomainTag kind="license" :value="licenseLabel(pr.licenseTier)" />
                  </div>
                  <el-button
                    v-if="auth.can('instance:write', ws.currentProjectId)"
                    size="small"
                    type="primary"
                    plain
                    @click="openDeploy(pr)"
                  >
                    部署
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 部署向导抽屉（7 关键字动态表单） -->
    <KiDrawer v-model="showWizard" :title="`部署 ${wizard.product}`" :subtitle="`当前环境：${ws.currentEnv.toUpperCase()} · JSON-Schema 动态表单`" size="md">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="实例名称"><el-input v-model="wizard.name" class="ki-mono" /></el-form-item>
          <el-form-item label="目标版本"><el-input v-model="wizard.productVersion" class="ki-mono" /></el-form-item>
        </div>
      </el-form>
      <KiDynamicSchemaForm v-model="wizard.spec" :schema="dynamicSchema" :hosts="hosts" />
      <template #footer>
        <el-button @click="showWizard = false">取消</el-button>
        <el-button type="primary" @click="submitCreateInstance">创建实例</el-button>
      </template>
    </KiDrawer>

    <!-- Plan & 3-Way Diff 审查抽屉 -->
    <KiDrawer v-model="showDiff" title="Plan 预检与 3-Way Diff 审查" :subtitle="activePlan ? `Plan ID: ${activePlan.planId} · ${activePlan.summary}` : ''" size="lg">
      <KiThreeWayDiffViewer
        v-if="activePlan"
        :instance-name="activePlan.instanceName"
        :path="`/etc/kinstall/${activePlan.instanceName}/config.yaml`"
        base-text="# baseline v1\nmaxmemory: 4096mb\ntlsReplication: true"
        live-text="# baseline v1\nmaxmemory: 4096mb\ntlsReplication: true\n# manual ops tweak"
        desired-text="# desired v2\nmaxmemory: 9318mb\ntlsReplication: true\naclFile: users.acl"
        :has-drift="true"
        @decision="(m) => m !== 'abort' && applyPlan(m === 'force-override')"
      />
      <template #footer>
        <el-button @click="showDiff = false">取消</el-button>
        <el-button type="primary" @click="applyPlan(false)">直接执行 Apply</el-button>
      </template>
    </KiDrawer>
  </div>
</template>

<style scoped>
.search-kw {
  width: 280px;
}

.btn-ico {
  margin-right: 4px;
}

.plugin-card {
  padding: 16px 18px;
}

.plugin-title {
  font-size: var(--ki-fs-sub);
  color: var(--ki-text-primary);
}

.plugin-ver {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.plugin-desc {
  margin: 0 0 12px;
  font-size: var(--ki-fs-xs);
}

.plugin-prods {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.prod-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
}
</style>
