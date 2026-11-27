<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Box, ChevronDown, GitCompare, Layers, Plus, Search, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiDomainTag from '@/components/ki/KiDomainTag.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiDrawer from '@/components/ki/KiDrawer.vue'
import KiBanner from '@/components/ki/KiBanner.vue'
import KiDynamicSchemaForm, { type PluginJSONSchema } from '@/components/domain/KiDynamicSchemaForm.vue'
import KiThreeWayDiffViewer from '@/components/domain/KiThreeWayDiffViewer.vue'
import KiDangerConfirmModal from '@/components/domain/KiDangerConfirmModal.vue'

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
  operations?: string[]
  paramSchema?: PluginJSONSchema
}

interface ConfigDiffItem {
  path: string
  baseText: string
  liveText: string
  desiredText: string
  hasDrift: boolean
  conflictCode?: string
  outcome: string
}

interface PlanDiffEnvelope {
  summary?: string
  configDiffs?: ConfigDiffItem[]
  hasDrift?: boolean
  warnings?: Array<{ code?: string; message?: string; severity?: string }>
}

const pluginMetaMap: Record<string, { title: string; category: string; summary: string }> = {
  kubernetes: {
    title: 'Kubernetes 集群引擎',
    category: '容器编排',
    summary: '支持 K3s / kubeadm 高可用集群、Cilium / Calico 网络与离线镜像预载。',
  },
  postgresql: {
    title: 'PostgreSQL 关系数据库',
    category: '关系数据库',
    summary: '内置 Patroni 自动主从高可用、pgBackRest 热备与按时间点恢复 (PITR)。',
  },
  mysql: {
    title: 'MySQL / MariaDB 数据库',
    category: '关系数据库',
    summary: '支持半同步主从复制、组复制与 InnoDB 生产调优基线。',
  },
  valkey: {
    title: 'Valkey 内存缓存',
    category: '缓存与消息',
    summary: 'Redis 开源社区延续版本，支持 Sentinel 哨兵与 Cluster 分片集群及透明大页校验。',
  },
  kafka: {
    title: 'Apache Kafka 消息队列',
    category: '缓存与消息',
    summary: '原生 KRaft 无 ZooKeeper 架构，支持多机架副本分布与 SCRAM-SHA-512 鉴权。',
  },
  opensearch: {
    title: 'OpenSearch 检索引擎',
    category: '搜索与日志',
    summary: '分布式全文检索与日志分析集群，内置 TLS 双向认证与堆内存安全限额。',
  },
  'elk-pipeline': {
    title: '日志采集与处理管道',
    category: '搜索与日志',
    summary: '统一编排 Data Prepper、Fluent Bit 与 Filebeat，对接 Kafka 与对象存储归档。',
  },
  'object-storage': {
    title: '企业对象存储 (S3)',
    category: '存储与监控',
    summary: '基于 SeaweedFS 与 RustFS 的高性能 S3 兼容存储，支持纠删码容灾。',
  },
  skywalking: {
    title: 'APM 与全栈可观测平台',
    category: '存储与监控',
    summary: '涵盖 SkyWalking、VictoriaMetrics、Grafana 与 Alertmanager 离线监控闭环。',
  },
  'messaging-tx': {
    title: '消息队列与分布式事务',
    category: '缓存与消息',
    summary: '提供 RocketMQ、RabbitMQ、EMQX、Nacos 与 Seata 分布式事务与服务治理。',
  },
  'devops-stack': {
    title: 'DevOps 研效与制品库',
    category: '研发交付',
    summary: '涵盖 Harbor、GitLab、Forgejo、Jenkins、SonarQube、Reposilite 与国密网关。',
  },
  'datastack-apps': {
    title: '数据智能与企业应用套件',
    category: '应用生态',
    summary: '提供 Milvus 向量库、MongoDB、Neo4j、OnlyOffice、Rancher 与 Zabbix 等组件。',
  },
}

const auth = useAuthStore()
const ws = useWorkspaceStore()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const plugins = ref<PluginItem[]>([])
const instances = ref<Array<Record<string, unknown>>>([])
const hosts = ref<Array<{ id: string; name: string; address: string; arch?: string; pageSize?: number; faultZone?: string }>>([])
const credentials = ref<Array<{ id: string; name: string }>>([])
const activeTab = ref(route.query.tab === 'catalog' ? 'catalog' : 'instances')
const filterTarget = ref('ALL')
const keyword = ref('')
const catalogKeyword = ref(typeof route.query.q === 'string' ? route.query.q : '')
const catalogCategory = ref('全部')
const initialTabSettled = ref(route.query.tab === 'catalog')
const showWizard = ref(false)
const showDiff = ref(false)
const selectedDiffIdx = ref(0)

const activePlan = ref<{
  instanceId: string
  instanceName: string
  action: string
  planId: string
  summary: string
  configDiffs: ConfigDiffItem[]
  warnings: Array<{ code?: string; message?: string; severity?: string }>
} | null>(null)

const dangerModal = reactive({
  open: false,
  mode: 'delete' as 'delete' | 'uninstall',
  instance: null as Record<string, unknown> | null,
  loading: false,
})

const pitrDialog = reactive({
  open: false,
  instance: null as Record<string, unknown> | null,
  targetTime: '',
})

const wizard = reactive({
  name: '',
  product: 'valkey',
  productVersion: '9.1.4',
  deployTarget: 'host-binary',
  spec: {} as Record<string, unknown>,
})

const opLabels: Record<string, string> = {
  install: '安装 / 变更 (install)',
  upgrade: '平滑升级 (upgrade)',
  scale: '弹性扩缩容 (scale)',
  backup: '热备份 (backup)',
  restore: '恢复 / PITR (restore)',
  failover: '主从切换 (failover)',
  switchover: '计划切主 (switchover)',
  'rotate-certs': '证书轮换 (rotate-certs)',
  'ec-encode': 'EC 纠删编码 (ec-encode)',
  reload: '配置重载 (reload)',
  uninstall: '安全卸载 (uninstall)',
}

function getPluginMeta(plg: PluginItem) {
  return (
    pluginMetaMap[plg.name] || {
      title: plg.name,
      category: '扩展组件',
      summary: plg.description,
    }
  )
}

const catalogCategories = computed(() => {
  const counts = new Map<string, number>()
  for (const plg of plugins.value) {
    const cat = getPluginMeta(plg).category
    counts.set(cat, (counts.get(cat) || 0) + 1)
  }
  return [
    { name: '全部', count: plugins.value.length },
    ...Array.from(counts.entries()).map(([name, count]) => ({ name, count })),
  ]
})

const filteredPlugins = computed(() => {
  const q = catalogKeyword.value.trim().toLowerCase()
  return plugins.value.filter((plg) => {
    const meta = getPluginMeta(plg)
    if (catalogCategory.value !== '全部' && meta.category !== catalogCategory.value) return false
    if (!q) return true
    const prodNames = (plg.products || []).map((p) => p.product).join(' ')
    return `${plg.name} ${meta.title} ${meta.summary} ${meta.category} ${prodNames}`.toLowerCase().includes(q)
  })
})

function findPluginByProduct(product: string): PluginItem | undefined {
  const low = product.toLowerCase()
  return plugins.value.find(
    (p) => p.name.toLowerCase() === low || (p.products || []).some((pr) => pr.product.toLowerCase() === low)
  )
}

function getOpsForInstance(inst: Record<string, unknown>): string[] {
  const plg = findPluginByProduct(String(inst.product || ''))
  if (plg?.operations && plg.operations.length > 0) return plg.operations
  return ['install', 'upgrade', 'scale', 'backup', 'uninstall']
}

const activeSchema = computed<PluginJSONSchema>(() => {
  const plg = findPluginByProduct(wizard.product)
  const rawProps = plg?.paramSchema?.properties || {}
  const merged: PluginJSONSchema['properties'] = {
    deployTarget: {
      type: 'string',
      default: wizard.deployTarget,
      'x-kinstall-ui': { group: 'basic', order: 0, widget: 'deploy-target-cards', label: '目标部署形态' },
    },
    ...rawProps,
  }
  return {
    title: plg?.paramSchema?.title || `${wizard.product} 核心参数`,
    properties: merged,
  }
})

const currentDiff = computed<ConfigDiffItem | null>(() => {
  const list = activePlan.value?.configDiffs || []
  if (list.length === 0) return null
  return list[selectedDiffIdx.value] || list[0]
})

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
    const [pRes, iRes, hRes, cRes] = await Promise.all([
      apiRequest<{ items: PluginItem[] }>('GET', '/api/v1/plugins'),
      apiRequest<{ items: Array<Record<string, unknown>> }>('GET', `/api/v1/projects/${ws.currentProjectId}/instances`),
      apiRequest<{ items: Array<{ id: string; name: string; address: string; arch?: string; pageSize?: number; faultZone?: string }> }>(
        'GET',
        `/api/v1/projects/${ws.currentProjectId}/hosts`
      ).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: Array<{ id: string; name: string }> }>(
        'GET',
        `/api/v1/projects/${ws.currentProjectId}/credentials`
      ).catch(() => ({ data: { items: [] } })),
    ])
    plugins.value = pRes.data.items || []
    instances.value = iRes.data.items || []
    hosts.value = hRes.data.items || []
    credentials.value = cRes.data.items || []
    if (!initialTabSettled.value) {
      initialTabSettled.value = true
      if (instances.value.length === 0) activeTab.value = 'catalog'
    }
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
    ElMessage.success('实例已创建，可执行 Plan 三向差异审查')
    showWizard.value = false
    activeTab.value = 'instances'
    await loadData()
  } catch (e) {
    ElMessage.error(String(e))
  }
}

async function runPlan(inst: Record<string, unknown>, action = 'install', params?: Record<string, unknown>) {
  try {
    const body: Record<string, unknown> = { action }
    if (params && Object.keys(params).length > 0) body.params = params
    const { data } = await apiRequest<{
      plan: { id: string; diff?: PlanDiffEnvelope | string; warnings?: Array<{ code?: string; message?: string; severity?: string }> }
    }>('POST', `/api/v1/projects/${ws.currentProjectId}/instances/${inst.id}:plan`, {
      body,
    })

    let env: PlanDiffEnvelope = {}
    if (typeof data.plan.diff === 'string') {
      try {
        env = JSON.parse(data.plan.diff) as PlanDiffEnvelope
      } catch {
        env = {}
      }
    } else if (data.plan.diff && typeof data.plan.diff === 'object') {
      env = data.plan.diff
    }

    const diffs = env.configDiffs && env.configDiffs.length > 0
      ? env.configDiffs
      : [
          {
            path: `/etc/kinstall/${inst.product}/${inst.name}/spec.json`,
            baseText: '',
            liveText: '',
            desiredText: JSON.stringify(inst.desiredSpec || {}, null, 2),
            hasDrift: false,
            outcome: 'create',
          },
        ]

    selectedDiffIdx.value = 0
    activePlan.value = {
      instanceId: String(inst.id),
      instanceName: String(inst.name),
      action,
      planId: data.plan.id,
      summary: env.summary || `${inst.product} ${inst.productVersion} (${inst.deployTarget}) · ${action}`,
      configDiffs: diffs,
      warnings: env.warnings || data.plan.warnings || [],
    }
    showDiff.value = true
  } catch (e) {
    ElMessage.error(String(e))
  }
}

function handleDay2Command(inst: Record<string, unknown>, op: string) {
  if (op === 'uninstall') {
    dangerModal.mode = 'uninstall'
    dangerModal.instance = inst
    dangerModal.open = true
    return
  }
  if (op === 'restore' && String(inst.product) === 'postgresql') {
    pitrDialog.instance = inst
    pitrDialog.targetTime = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z')
    pitrDialog.open = true
    return
  }
  void runPlan(inst, op)
}

function openDeleteInstance(inst: Record<string, unknown>) {
  dangerModal.mode = 'delete'
  dangerModal.instance = inst
  dangerModal.open = true
}

async function confirmDangerAction() {
  if (!dangerModal.instance) return
  const inst = dangerModal.instance
  dangerModal.loading = true
  try {
    if (dangerModal.mode === 'delete') {
      await apiRequest('DELETE', `/api/v1/projects/${ws.currentProjectId}/instances/${inst.id}`)
      ElMessage.success(`实例 ${inst.name} 元数据已删除`)
      dangerModal.open = false
      await loadData()
    } else {
      dangerModal.open = false
      await runPlan(inst, 'uninstall')
    }
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    dangerModal.loading = false
  }
}

async function submitPitrRestore() {
  if (!pitrDialog.instance) return
  const inst = pitrDialog.instance
  const targetTime = pitrDialog.targetTime
  pitrDialog.open = false
  ElMessage.info(`已附带 PITR 时间点 ${targetTime} 生成恢复 Plan`)
  await runPlan(inst, 'restore', { targetTime })
}

async function applyPlan(forceOverride = false) {
  if (!activePlan.value) return
  try {
    const { data } = await apiRequest<{ id: string; state: string }>(
      'POST',
      `/api/v1/projects/${ws.currentProjectId}/instances/${activePlan.value.instanceId}:apply`,
      {
        body: { planId: activePlan.value.planId, action: activePlan.value.action, forceOverride },
        idempotent: true,
      }
    )
    showDiff.value = false
    await ws.refreshApprovalBadge()
    if (data.state === 'awaiting_approval') {
      ElMessage.warning('生产环境或高危变更已进入变更审批队列')
      await router.push('/operations/approvals')
    } else {
      ElMessage.success(`操作 ${data.id} (${activePlan.value.action}) 已入队执行`)
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
    <PageHeader title="实例与组件" :refresh="loadData" :refreshing="loading" />

    <!-- 点击筛选统计条 -->
    <div class="ki-chips">
      <div class="ki-chip" :class="{ 'ki-chip--active': activeTab === 'instances' && filterTarget === 'ALL' }" @click="filterTarget = 'ALL'; activeTab = 'instances'">
        <span class="ki-chip__bar" />
        <div><div class="ki-chip__label">全部实例</div><div class="ki-chip__val ki-num">{{ instances.length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': activeTab === 'instances' && filterTarget === 'host-binary' }" @click="filterTarget = 'host-binary'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--running" />
        <div><div class="ki-chip__label">主机二进制</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'host-binary').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': activeTab === 'instances' && filterTarget === 'host-container' }" @click="filterTarget = 'host-container'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--warn" />
        <div><div class="ki-chip__label">单机容器</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'host-container').length }}</div></div>
      </div>
      <div class="ki-chip" :class="{ 'ki-chip--active': activeTab === 'instances' && filterTarget === 'kubernetes' }" @click="filterTarget = 'kubernetes'; activeTab = 'instances'">
        <span class="ki-chip__bar ki-chip__bar--ok" />
        <div><div class="ki-chip__label">Kubernetes 编排</div><div class="ki-chip__val ki-num">{{ instances.filter((x) => x.deployTarget === 'kubernetes').length }}</div></div>
      </div>
    </div>

    <div class="ki-card">
      <div class="table-heading">
        <span class="ki-secondary">声明式组件部署向导 · 两段式 Plan 与三向漂移审查 · 全生命周期 Day-2 运维</span>
        <div class="table-actions">
          <el-button type="primary" @click="activeTab = 'catalog'">
            <Plus :size="14" class="btn-ico" /> 浏览组件市场 ({{ plugins.length }})
          </el-button>
        </div>
      </div>

      <el-tabs v-model="activeTab">
        <el-tab-pane :label="`项目实例 (${filteredInstances.length})`" name="instances">
          <div v-if="instances.length > 0" class="filter-bar">
            <el-input v-model="keyword" clearable placeholder="搜索实例名 / 组件 / 版本" class="search-kw">
              <template #prefix><Search :size="14" /></template>
            </el-input>
          </div>
          <el-table v-if="filteredInstances.length > 0 || loading" v-loading="loading" :data="filteredInstances">
            <el-table-column prop="name" label="实例名称" min-width="180">
              <template #default="{ row }"><b class="ki-mono">{{ row.name }}</b></template>
            </el-table-column>
            <el-table-column prop="product" label="组件" width="130" />
            <el-table-column label="版本" width="100">
              <template #default="{ row }"><span class="ki-mono">{{ row.productVersion }}</span></template>
            </el-table-column>
            <el-table-column label="交付形态" width="146">
              <template #default="{ row }"><KiDomainTag kind="target" :value="String(row.deployTarget || 'host-binary')" /></template>
            </el-table-column>
            <el-table-column label="状态" width="124">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'created')" /></template>
            </el-table-column>
            <el-table-column label="Day-1 / Day-2 操作" width="286" fixed="right">
              <template #default="{ row }">
                <div class="ki-row-actions">
                  <el-button
                    v-if="auth.can('instance:plan', ws.currentProjectId)"
                    size="small"
                    type="primary"
                    plain
                    @click="runPlan(row, 'install')"
                  >
                    <GitCompare :size="13" class="btn-ico" /> Plan & Diff
                  </el-button>
                  <el-dropdown
                    v-if="auth.can('instance:plan', ws.currentProjectId)"
                    trigger="click"
                    @command="(cmd: string) => handleDay2Command(row, cmd)"
                  >
                    <el-button size="small">
                      Day-2 <ChevronDown :size="13" class="btn-ico-r" />
                    </el-button>
                    <template #dropdown>
                      <el-dropdown-menu>
                        <el-dropdown-item
                          v-for="op in getOpsForInstance(row)"
                          :key="op"
                          :command="op"
                          :divided="op === 'uninstall'"
                        >
                          {{ opLabels[op] || op }}
                        </el-dropdown-item>
                      </el-dropdown-menu>
                    </template>
                  </el-dropdown>
                  <el-button
                    v-if="auth.can('instance:write', ws.currentProjectId)"
                    size="small"
                    type="danger"
                    text
                    aria-label="删除实例"
                    @click="openDeleteInstance(row)"
                  >
                    <Trash2 :size="13" />
                  </el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
          <div v-else class="ki-empty-state">
            <div class="ki-empty-state__icon"><Box :size="20" /></div>
            <div class="ki-empty-state__title">{{ instances.length === 0 ? '当前项目尚未创建交付实例' : '没有符合条件的实例' }}</div>
            <p class="ki-empty-state__desc">从右侧组件市场挑选所需中间件，系统将依据后端插件 Schema 实时渲染参数配置向导。</p>
            <div class="ki-empty-state__actions">
              <el-button type="primary" size="small" @click="activeTab = 'catalog'">
                <Layers :size="13" class="btn-ico" /> 打开组件市场
              </el-button>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="`组件市场 (${plugins.length})`" name="catalog">
          <div class="catalog-toolbar ki-mb-4">
            <div class="ki-pill-tabs">
              <button
                v-for="cat in catalogCategories"
                :key="cat.name"
                type="button"
                class="ki-pill-tab"
                :class="{ 'ki-pill-tab--active': catalogCategory === cat.name }"
                @click="catalogCategory = cat.name"
              >
                <span>{{ cat.name }}</span>
                <span class="ki-pill-tab__count ki-num">{{ cat.count }}</span>
              </button>
            </div>
            <el-input v-model="catalogKeyword" clearable placeholder="搜索插件或产品名…" class="catalog-search">
              <template #prefix><Search :size="14" /></template>
            </el-input>
          </div>

          <div class="ki-grid-2">
            <div v-for="plg in filteredPlugins" :key="plg.name" class="ki-surface plugin-card">
              <div class="plugin-head">
                <div class="plugin-head__title">
                  <strong>{{ getPluginMeta(plg).title }}</strong>
                  <span class="ki-mono plugin-slug">{{ plg.name }}</span>
                </div>
                <div class="ki-flex-row">
                  <span class="ki-badge plugin-cat">{{ getPluginMeta(plg).category }}</span>
                  <span class="ki-badge ki-mono plugin-ver">v{{ plg.version }}</span>
                </div>
              </div>
              <p class="ki-secondary plugin-desc">{{ getPluginMeta(plg).summary }}</p>
              <div class="plugin-prods">
                <div v-for="pr in plg.products || []" :key="pr.product" class="prod-row">
                  <div class="prod-info">
                    <div class="prod-name-line">
                      <b class="ki-mono">{{ pr.product }}</b>
                      <span class="ki-mono ki-muted">{{ pr.defaultVersion || pr.versions }}</span>
                      <KiDomainTag kind="license" :value="licenseLabel(pr.licenseTier)" />
                    </div>
                    <div v-if="pr.targets?.length" class="prod-targets">
                      <KiDomainTag v-for="t in pr.targets" :key="t" kind="target" :value="t" />
                    </div>
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

    <!-- 部署向导抽屉 -->
    <KiDrawer
      v-model="showWizard"
      :title="`部署 ${wizard.product}`"
      :subtitle="`当前环境：${ws.currentEnv.toUpperCase()} · 实时 JSON-Schema 动态参数向导`"
      size="md"
    >
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="实例名称"><el-input v-model="wizard.name" class="ki-mono" /></el-form-item>
          <el-form-item label="目标版本"><el-input v-model="wizard.productVersion" class="ki-mono" /></el-form-item>
        </div>
      </el-form>
      <KiDynamicSchemaForm
        v-model="wizard.spec"
        :schema="activeSchema"
        :hosts="hosts"
        :credentials="credentials"
      />
      <template #footer>
        <el-button @click="showWizard = false">取消</el-button>
        <el-button type="primary" @click="submitCreateInstance">创建实例</el-button>
      </template>
    </KiDrawer>

    <!-- Plan & 3-Way Diff 审查抽屉 -->
    <KiDrawer
      v-model="showDiff"
      title="Plan 预检与 3-Way Diff 审查"
      :subtitle="activePlan ? `Plan ID: ${activePlan.planId} · Action: ${activePlan.action} · ${activePlan.summary}` : ''"
      size="lg"
    >
      <template v-if="activePlan">
        <KiBanner
          v-for="(w, idx) in activePlan.warnings"
          :key="idx"
          variant="warn"
          :text="`[${w.code || 'WARN'}] ${w.message || ''}`"
          class="ki-mb-3"
        />

        <div v-if="activePlan.configDiffs.length > 1" class="diff-file-tabs ki-mb-3">
          <button
            v-for="(df, idx) in activePlan.configDiffs"
            :key="df.path"
            type="button"
            class="diff-file-pill ki-mono"
            :class="{ 'is-active': selectedDiffIdx === idx, 'is-drift': df.hasDrift }"
            @click="selectedDiffIdx = idx"
          >
            {{ df.path }}
            <span class="pill-outcome">{{ df.outcome }}</span>
          </button>
        </div>

        <KiThreeWayDiffViewer
          v-if="currentDiff"
          :instance-name="activePlan.instanceName"
          :path="currentDiff.path"
          :base-text="currentDiff.baseText"
          :live-text="currentDiff.liveText"
          :desired-text="currentDiff.desiredText"
          :has-drift="currentDiff.hasDrift"
          @decision="(m) => m !== 'abort' && applyPlan(m === 'force-override')"
        />
      </template>
      <template #footer>
        <el-button @click="showDiff = false">取消</el-button>
        <el-button type="primary" @click="applyPlan(false)">执行 Apply ({{ activePlan?.action || 'install' }})</el-button>
      </template>
    </KiDrawer>

    <!-- PostgreSQL PITR 恢复弹窗 -->
    <el-dialog v-model="pitrDialog.open" title="PostgreSQL PITR 按时间点恢复" width="460px">
      <el-form label-position="top">
        <el-form-item label="目标恢复时间点 (RFC3339)">
          <el-input v-model="pitrDialog.targetTime" class="ki-mono" placeholder="2026-11-27T12:00:00Z" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pitrDialog.open = false">取消</el-button>
        <el-button type="primary" @click="submitPitrRestore">生成恢复 Plan</el-button>
      </template>
    </el-dialog>

    <!-- 高危删除 / 卸载确认窗 -->
    <KiDangerConfirmModal
      v-model="dangerModal.open"
      :env="ws.currentEnv"
      :title="dangerModal.mode === 'delete' ? '确认删除实例元数据' : '确认生成卸载 (uninstall) Plan'"
      :resource-name="String(dangerModal.instance?.name || '')"
      :risk-summary="dangerModal.mode === 'delete'
        ? '删除实例将释放主机端口/目录占用并从控制面移除该实例记录。'
        : '卸载操作将停止远端服务并按拓扑逆序清理托管单元（生产环境需双人审批）。'"
      :confirm-label="dangerModal.mode === 'delete' ? '解锁并删除实例' : '解锁并生成卸载 Plan'"
      :loading="dangerModal.loading"
      @confirm="confirmDangerAction"
    />
  </div>
</template>

<style scoped>
.search-kw {
  width: 280px;
}

.catalog-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.catalog-search {
  width: 240px;
}

.btn-ico {
  margin-right: 4px;
}

.btn-ico-r {
  margin-left: 4px;
}

.plugin-card {
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.plugin-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.plugin-head__title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.plugin-head__title strong {
  font-size: var(--ki-fs-section);
  color: var(--ki-text-primary);
}

.plugin-slug {
  font-size: 11px;
  color: var(--ki-text-muted);
}

.plugin-cat {
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.plugin-ver {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.plugin-desc {
  margin: 0;
  font-size: var(--ki-fs-xs);
  line-height: 1.55;
}

.plugin-prods {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: auto;
}

.prod-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border-radius: var(--ki-radius-sm);
  background-color: var(--ki-bg-subtle);
}

.prod-info {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}

.prod-name-line {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.prod-targets {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.diff-file-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.diff-file-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  border-radius: var(--ki-radius-sm);
  border: 1px solid var(--ki-border-default);
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  font-size: var(--ki-fs-xs);
  cursor: pointer;
}

.diff-file-pill.is-active {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
  color: var(--ki-brand);
}

.diff-file-pill.is-drift {
  border-color: var(--ki-status-warn-border);
}

.pill-outcome {
  padding: 1px 5px;
  border-radius: var(--ki-radius-xs);
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-muted);
}
</style>
