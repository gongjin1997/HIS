<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Eye, Filter, Plus, Rocket, Trash2 } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

export interface CMDBGroupRule {
  field: string
  op: string
  value: string
}

export interface DynamicGroupItem {
  id: string
  name: string
  description: string
  rules: CMDBGroupRule[]
  matchedCount: number
  matchedHostIds: string[]
}

const props = defineProps<{
  projectId: string
  canWrite: boolean
  groups: DynamicGroupItem[]
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
  (e: 'select-group', g: DynamicGroupItem): void
}>()

const router = useRouter()

const dialog = reactive({
  open: false,
  editingId: '',
  name: '',
  description: '',
  rules: [{ field: 'arch', op: 'eq', value: 'arm64' }] as CMDBGroupRule[],
  saving: false,
  previewing: false,
  previewHosts: [] as Array<{ id: string; name: string; address: string; arch?: string; faultZone?: string; state?: string }>,
  previewed: false,
})

const deletingId = ref('')

const fieldOptions = [
  { label: '业务系统 (biz)', value: 'biz' },
  { label: '集群环境 (set)', value: 'set' },
  { label: '业务模块 (module)', value: 'module' },
  { label: '区域机房 (region)', value: 'region' },
  { label: '安全域 (security_area)', value: 'security_area' },
  { label: '故障域 (fault_zone)', value: 'fault_zone' },
  { label: '机架 (fault_rack)', value: 'fault_rack' },
  { label: 'CPU 架构 (arch)', value: 'arch' },
  { label: '内核页大小 (page_size)', value: 'page_size' },
  { label: '操作系统家族 (os_family)', value: 'os_family' },
  { label: '主机状态 (state)', value: 'state' },
  { label: 'CPU 核数 (cpu_cores)', value: 'cpu_cores' },
  { label: '内存 MB (mem_mb)', value: 'mem_mb' },
  { label: '主机名 (name)', value: 'name' },
  { label: '负责人 (owner)', value: 'owner' },
]

const opOptions = [
  { label: '等于 (eq)', value: 'eq' },
  { label: '不等于 (neq)', value: 'neq' },
  { label: '包含 (contains)', value: 'contains' },
  { label: '属于列表 (in)', value: 'in' },
  { label: '前缀匹配 (prefix)', value: 'prefix' },
  { label: '大于等于 (gte)', value: 'gte' },
  { label: '小于等于 (lte)', value: 'lte' },
  { label: '存在非空 (exists)', value: 'exists' },
]

function openCreate() {
  dialog.editingId = ''
  dialog.name = ''
  dialog.description = ''
  dialog.rules = [{ field: 'biz', op: 'contains', value: 'HIS' }]
  dialog.previewHosts = []
  dialog.previewed = false
  dialog.open = true
}

function openEdit(g: DynamicGroupItem) {
  dialog.editingId = g.id
  dialog.name = g.name
  dialog.description = g.description || ''
  dialog.rules = g.rules?.length ? g.rules.map((r) => ({ ...r })) : [{ field: 'arch', op: 'eq', value: 'amd64' }]
  dialog.previewHosts = []
  dialog.previewed = false
  dialog.open = true
}

function addRule() {
  dialog.rules.push({ field: 'fault_zone', op: 'eq', value: 'az-1' })
}

function removeRule(idx: number) {
  dialog.rules.splice(idx, 1)
}

async function runPreview() {
  dialog.previewing = true
  try {
    const { data } = await apiRequest<{ items: typeof dialog.previewHosts; count: number }>(
      'POST',
      `/api/v1/projects/${props.projectId}/cmdb/groups:preview`,
      { body: { rules: dialog.rules } }
    )
    dialog.previewHosts = data.items || []
    dialog.previewed = true
    ElMessage.success(`规则命中 ${data.count} 台主机`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    dialog.previewing = false
  }
}

async function saveGroup() {
  if (!dialog.name.trim()) {
    ElMessage.warning('请输入动态分组名称')
    return
  }
  dialog.saving = true
  try {
    if (dialog.editingId) {
      await apiRequest('PUT', `/api/v1/projects/${props.projectId}/cmdb/groups/${dialog.editingId}`, {
        body: { name: dialog.name.trim(), description: dialog.description.trim(), rules: dialog.rules },
      })
      ElMessage.success('动态分组已更新')
    } else {
      await apiRequest('POST', `/api/v1/projects/${props.projectId}/cmdb/groups`, {
        body: { name: dialog.name.trim(), description: dialog.description.trim(), rules: dialog.rules },
        idempotent: true,
      })
      ElMessage.success('动态分组已创建')
    }
    dialog.open = false
    emit('refresh')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    dialog.saving = false
  }
}

async function removeGroup(g: DynamicGroupItem) {
  deletingId.value = g.id
  try {
    await apiRequest('DELETE', `/api/v1/projects/${props.projectId}/cmdb/groups/${g.id}`)
    ElMessage.success(`已删除动态分组 ${g.name}`)
    emit('refresh')
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    deletingId.value = ''
  }
}

function deployWithGroup(g: DynamicGroupItem) {
  void router.push({ path: '/instances', query: { tab: 'catalog', group: g.id } })
}
</script>

<template>
  <div>
    <div class="ki-flex-between ki-mb-3">
      <span class="ki-secondary">按业务拓扑、CPU 架构、内核页大小、安全域与硬件条件动态圈选主机组，可直接联动组件部署与批量运维。</span>
      <el-button v-if="canWrite" type="primary" size="small" @click="openCreate">
        <Plus :size="13" class="btn-ico" /> 新建动态分组
      </el-button>
    </div>

    <el-table v-if="groups.length > 0" :data="groups" size="small">
      <el-table-column label="分组名称" min-width="150">
        <template #default="{ row }">
          <div class="grp-name">{{ row.name }}</div>
          <div v-if="row.description" class="ki-muted grp-desc">{{ row.description }}</div>
        </template>
      </el-table-column>
      <el-table-column label="匹配规则 (AND)" min-width="260">
        <template #default="{ row }">
          <div class="rule-tags">
            <span v-for="(r, i) in row.rules || []" :key="i" class="ki-badge ki-mono rule-pill">
              {{ r.field }} {{ r.op }} {{ r.value }}
            </span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="实时命中" width="96">
        <template #default="{ row }">
          <b class="ki-num">{{ row.matchedCount }} 台</b>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <div class="ki-row-actions">
            <el-button link type="primary" @click="emit('select-group', row)">
              <Filter :size="13" class="btn-ico" /> 筛选台账
            </el-button>
            <el-button link type="primary" @click="deployWithGroup(row)">
              <Rocket :size="13" class="btn-ico" /> 去部署
            </el-button>
            <el-button v-if="canWrite" link @click="openEdit(row)">编辑</el-button>
            <el-button v-if="canWrite" link type="danger" :loading="deletingId === row.id" @click="removeGroup(row)">
              删除
            </el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <div v-else class="ki-empty-state">
      <div class="ki-empty-state__title">暂无 CMDB 动态分组</div>
      <p class="ki-empty-state__desc">例如创建「鲲鹏 64K 数据库节点」(arch=arm64 &amp; page_size=65536)，即可实时追踪符合条件的资产并一键下发部署。</p>
      <el-button v-if="canWrite" type="primary" size="small" @click="openCreate">
        <Plus :size="13" class="btn-ico" /> 创建首个动态分组
      </el-button>
    </div>

    <el-dialog v-model="dialog.open" :title="dialog.editingId ? '编辑动态分组' : '新建 CMDB 动态分组'" width="680px">
      <el-form label-position="top">
        <div class="ki-grid-2">
          <el-form-item label="分组名称">
            <el-input v-model="dialog.name" placeholder="如：HIS 核心鲲鹏数据库节点" />
          </el-form-item>
          <el-form-item label="用途说明">
            <el-input v-model="dialog.description" placeholder="可选：用于批量巡检或中间件部署选机" />
          </el-form-item>
        </div>

        <div class="ki-flex-between ki-mb-3">
          <span class="rule-title">匹配规则条件（全部满足 AND）</span>
          <div class="ki-flex-row">
            <el-button size="small" @click="addRule"><Plus :size="12" class="btn-ico" /> 添加条件</el-button>
            <el-button size="small" type="primary" plain :loading="dialog.previewing" @click="runPreview">
              <Eye :size="12" class="btn-ico" /> 实时预览命中
            </el-button>
          </div>
        </div>

        <div class="rule-list ki-mb-3">
          <div v-for="(r, idx) in dialog.rules" :key="idx" class="rule-row">
            <el-select v-model="r.field" size="small" filterable allow-create class="rule-field">
              <el-option v-for="f in fieldOptions" :key="f.value" :label="f.label" :value="f.value" />
            </el-select>
            <el-select v-model="r.op" size="small" class="rule-op">
              <el-option v-for="o in opOptions" :key="o.value" :label="o.label" :value="o.value" />
            </el-select>
            <el-input
              v-model="r.value"
              size="small"
              :disabled="r.op === 'exists'"
              placeholder="值（in 运算用英文逗号分隔）"
              class="rule-val ki-mono"
            />
            <el-button link type="danger" :disabled="dialog.rules.length <= 1" @click="removeRule(idx)">
              <Trash2 :size="14" />
            </el-button>
          </div>
        </div>

        <div v-if="dialog.previewed">
          <div class="section-title">实时命中预览<span class="sub">共 {{ dialog.previewHosts.length }} 台主机</span></div>
          <el-table v-if="dialog.previewHosts.length > 0" :data="dialog.previewHosts" size="small" max-height="180">
            <el-table-column prop="name" label="主机名" min-width="140">
              <template #default="{ row }"><span class="ki-mono">{{ row.name }}</span></template>
            </el-table-column>
            <el-table-column prop="address" label="IP" width="130">
              <template #default="{ row }"><span class="ki-mono">{{ row.address }}</span></template>
            </el-table-column>
            <el-table-column prop="arch" label="架构" width="90" />
            <el-table-column label="状态" width="96">
              <template #default="{ row }"><KiStatusBadge :status="String(row.state || 'pending')" /></template>
            </el-table-column>
          </el-table>
          <div v-else class="ki-muted">当前规则未命中任何主机，请调整条件后重试。</div>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="dialog.open = false">取消</el-button>
        <el-button type="primary" :loading="dialog.saving" @click="saveGroup">保存动态分组</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.grp-name {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.grp-desc {
  font-size: var(--ki-fs-xs);
}

.rule-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.rule-pill {
  background-color: var(--ki-bg-subtle);
  color: var(--ki-text-secondary);
}

.rule-title {
  font-size: var(--ki-fs-xs);
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.rule-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.rule-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rule-field {
  width: 200px;
}

.rule-op {
  width: 140px;
}

.rule-val {
  flex: 1;
}

.btn-ico {
  margin-right: 4px;
}
</style>
