<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Play, ShieldAlert, Terminal, Package, Server, Layers } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'
import KiOperationTerminal from '@/components/domain/KiOperationTerminal.vue'

interface JobTemplate {
  id: string
  name: string
  category: string
  description: string
  sudo: boolean
  timeoutSec: number
  script: string
}

interface HostItem {
  id: string
  name: string
  address: string
  arch?: string
  state?: string
  businessLine?: string
}

interface GroupItem {
  id: string
  name: string
  matchedCount: number
  matchedHostIds: string[]
}

interface ArtifactOption {
  logicalPath: string
  fileName: string
  product: string
  version: string
  sizeBytes: number
}

const router = useRouter()
const ws = useWorkspaceStore()

const loading = ref(false)
const running = ref(false)
const templates = ref<JobTemplate[]>([])
const hosts = ref<HostItem[]>([])
const groups = ref<GroupItem[]>([])
const artifacts = ref<ArtifactOption[]>([])
const activeOpId = ref('')
const activeOpName = ref('')

const form = reactive({
  name: '系统资源与健康全面巡检',
  targetMode: 'hosts' as 'hosts' | 'group',
  hostIds: [] as string[],
  groupId: '',
  artifactPath: '',
  remoteTargetDir: '/opt/apps/release',
  serial: false,
  timeoutSec: 120,
  script: '',
})

const selectedHostCount = computed(() => {
  if (form.targetMode === 'group') {
    const g = groups.value.find((x) => x.id === form.groupId)
    return g?.matchedCount || 0
  }
  return form.hostIds.length
})

const catMap: Record<string, string> = {
  inspect: '巡检',
  ops: '日常运维',
  'app-deploy': '应用发布',
  security: '安全合规',
}

async function loadAll() {
  if (!ws.currentProjectId) return
  loading.value = true
  try {
    const pid = ws.currentProjectId
    const [tRes, hRes, gRes, aRes] = await Promise.all([
      apiRequest<{ items: JobTemplate[] }>('GET', `/api/v1/projects/${pid}/jobs/templates`),
      apiRequest<{ items: HostItem[] }>('GET', `/api/v1/projects/${pid}/hosts`),
      apiRequest<{ items: GroupItem[] }>('GET', `/api/v1/projects/${pid}/cmdb/groups`).catch(() => ({ data: { items: [] } })),
      apiRequest<{ items: ArtifactOption[] }>('GET', `/api/v1/projects/${pid}/artifacts`).catch(() => ({ data: { items: [] } })),
    ])
    templates.value = tRes.data.items || []
    hosts.value = hRes.data.items || []
    groups.value = gRes.data.items || []
    artifacts.value = aRes.data.items || []
    if (!form.script && templates.value.length > 0) {
      applyTemplate(templates.value[0])
    }
    if (form.hostIds.length === 0 && hosts.value.length > 0) {
      form.hostIds = hosts.value.map((h) => h.id)
    }
  } finally {
    loading.value = false
  }
}

function applyTemplate(tpl: JobTemplate) {
  form.name = tpl.name
  form.script = tpl.script
  form.timeoutSec = tpl.timeoutSec || 120
}

function toggleSelectAllHosts() {
  if (form.hostIds.length === hosts.value.length) {
    form.hostIds = []
  } else {
    form.hostIds = hosts.value.map((h) => h.id)
  }
}

async function runJob() {
  if (selectedHostCount.value === 0) {
    ElMessage.warning('请至少选择 1 台目标主机或包含主机的 CMDB 动态分组')
    return
  }
  if (!form.script.trim()) {
    ElMessage.warning('请输入要执行的作业脚本')
    return
  }
  running.value = true
  try {
    const payload: Record<string, unknown> = {
      name: form.name.trim() || 'batch-job',
      script: form.script,
      serial: form.serial,
      timeoutSec: form.timeoutSec,
      artifactPath: form.artifactPath || undefined,
      remoteTargetDir: form.remoteTargetDir || '/opt/apps/release',
    }
    if (form.targetMode === 'group') {
      payload.groupId = form.groupId
    } else {
      payload.hostIds = form.hostIds
    }
    const { data } = await apiRequest<{ id: string }>('POST', `/api/v1/projects/${ws.currentProjectId}/jobs:run`, {
      body: payload,
    })
    activeOpId.value = data.id
    activeOpName.value = form.name
    ElMessage.success(`批量作业已下发执行（${selectedHostCount.value} 台主机），正在实时回传输出`)
  } catch (e) {
    ElMessage.error(String(e))
  } finally {
    running.value = false
  }
}

watch(() => ws.currentProjectId, () => {
  activeOpId.value = ''
  form.hostIds = []
  void loadAll()
})
onMounted(loadAll)
</script>

<template>
  <div v-loading="loading">
    <PageHeader
      title="批量作业中心"
      subtitle="多主机并发巡检、应用包批量分发与受控脚本执行（自动内置高危指令门禁与双哈希审计）"
      :refresh="loadAll"
      :refreshing="loading"
    >
      <template #actions>
        <el-button @click="router.push('/operations')">
          <Terminal :size="14" class="btn-ico" /> 查看作业历史
        </el-button>
        <el-button type="primary" :loading="running" @click="runJob">
          <Play :size="14" class="btn-ico" /> 立即下发执行 ({{ selectedHostCount }})
        </el-button>
      </template>
    </PageHeader>

    <div class="jobs-grid ki-mb-4">
      <!-- 左侧：内置运维与发布模板 -->
      <div class="ki-card tpl-rail">
        <div class="section-title">内置运维与巡检模板</div>
        <div class="tpl-list">
          <div
            v-for="tpl in templates"
            :key="tpl.id"
            class="tpl-card"
            :class="{ 'tpl-card--active': form.name === tpl.name }"
            @click="applyTemplate(tpl)"
          >
            <div class="tpl-card__head">
              <strong>{{ tpl.name }}</strong>
              <span class="ki-badge tpl-cat">{{ catMap[tpl.category] || tpl.category }}</span>
            </div>
            <p class="tpl-card__desc">{{ tpl.description }}</p>
          </div>
        </div>
      </div>

      <!-- 右侧：目标选择 + 可选包下发 + 脚本编辑器 -->
      <div class="ki-card">
        <div class="section-title">作业目标与执行脚本</div>
        <el-form label-position="top">
          <div class="ki-grid-2">
            <el-form-item label="作业名称">
              <el-input v-model="form.name" placeholder="例如：全网磁盘巡检 / HIS 业务包发布" />
            </el-form-item>
            <el-form-item label="目标主机圈选方式">
              <div class="ki-flex-row">
                <el-radio-group v-model="form.targetMode" size="small">
                  <el-radio-button value="hosts"><Server :size="12" class="btn-ico" /> 指定主机</el-radio-button>
                  <el-radio-button value="group"><Layers :size="12" class="btn-ico" /> 动态分组</el-radio-button>
                </el-radio-group>
                <el-button v-if="form.targetMode === 'hosts' && hosts.length > 0" link type="primary" size="small" @click="toggleSelectAllHosts">
                  {{ form.hostIds.length === hosts.length ? '取消全选' : '全选主机' }}
                </el-button>
              </div>
            </el-form-item>
          </div>

          <el-form-item v-if="form.targetMode === 'hosts'" label="选择目标主机">
            <el-select v-model="form.hostIds" multiple filterable class="ki-w-full" placeholder="选择一台或多台 CMDB 主机">
              <el-option
                v-for="h in hosts"
                :key="h.id"
                :label="`${h.name} (${h.address}${h.arch ? ' · ' + h.arch : ''})`"
                :value="h.id"
              />
            </el-select>
            <div v-if="hosts.length === 0" class="ki-muted empty-host-hint">
              当前项目暂无纳管主机，请先前往 <el-button link type="primary" @click="router.push('/hosts')">CMDB 资产中心</el-button> 添加或导入主机。
            </div>
          </el-form-item>

          <el-form-item v-else label="选择 CMDB 动态分组（执行时按规则实时圈选）">
            <el-select v-model="form.groupId" class="ki-w-full" placeholder="选择 CMDB 动态分组">
              <el-option
                v-for="g in groups"
                :key="g.id"
                :label="`${g.name}（实时命中 ${g.matchedCount} 台）`"
                :value="g.id"
              />
            </el-select>
          </el-form-item>

          <div class="ki-grid-2">
            <el-form-item label="附带下发离线制品/应用包（可选）">
              <el-select v-model="form.artifactPath" clearable class="ki-w-full" placeholder="不附带文件（纯脚本执行）">
                <el-option
                  v-for="a in artifacts"
                  :key="a.logicalPath"
                  :label="`${a.product} ${a.version} · ${a.fileName}`"
                  :value="a.logicalPath"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="远端下发目录（附带包时生效）">
              <el-input v-model="form.remoteTargetDir" class="ki-mono" :disabled="!form.artifactPath" placeholder="/opt/apps/release" />
            </el-form-item>
          </div>

          <el-form-item label="POSIX Shell 执行脚本（自动通过 sudo 提权并在目标机受控执行）">
            <el-input v-model="form.script" type="textarea" :rows="9" class="ki-mono" placeholder="输入 Shell 巡检或发布脚本…" />
          </el-form-item>

          <div class="run-bar">
            <div class="ki-flex-row">
              <el-checkbox v-model="form.serial">逐台串行执行（灰度防雪崩）</el-checkbox>
              <span class="ki-muted safety-note">
                <ShieldAlert :size="13" /> 已启用高危命令防破坏门禁（自动拦截 <code>rm -rf /</code>、<code>mkfs</code>、<code>dd</code> 与路径穿越）
              </span>
            </div>
            <div class="ki-flex-row">
              <el-button v-if="form.artifactPath" link type="primary" @click="router.push('/artifacts/bundles')">
                <Package :size="13" class="btn-ico" /> 管理离线包
              </el-button>
              <el-button type="primary" :loading="running" @click="runJob">
                <Play :size="14" class="btn-ico" /> 下发执行 ({{ selectedHostCount }} 台)
              </el-button>
            </div>
          </div>
        </el-form>
      </div>
    </div>

    <!-- 实时作业输出终端 -->
    <div v-if="activeOpId" class="ki-card">
      <div class="ki-card__title">
        <div class="ki-flex-row">
          <span>实时作业终端 · {{ activeOpName }}</span>
          <KiStatusBadge status="running" label="SSE 实时追踪" />
        </div>
        <el-button size="small" @click="router.push(`/operations?opId=${activeOpId}`)">
          在执行中心打开全屏视图
        </el-button>
      </div>
      <KiOperationTerminal
        :key="activeOpId"
        :project-id="ws.currentProjectId"
        :op-id="activeOpId"
      />
    </div>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.jobs-grid {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
}

.tpl-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tpl-card {
  padding: 10px 12px;
  border-radius: var(--ki-radius-md);
  border: 1px solid var(--ki-border-subtle);
  background-color: var(--ki-bg-subtle);
  cursor: pointer;
  transition: border-color var(--ki-dur-fast) var(--ki-ease), background-color var(--ki-dur-fast) var(--ki-ease);
}

.tpl-card:hover,
.tpl-card--active {
  border-color: var(--ki-brand);
  background-color: var(--ki-brand-subtle);
}

.tpl-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  font-size: var(--ki-fs-sm);
  color: var(--ki-text-primary);
}

.tpl-cat {
  background-color: var(--ki-bg-surface);
  color: var(--ki-text-secondary);
  border: 1px solid var(--ki-border-subtle);
  flex-shrink: 0;
}

.tpl-card__desc {
  margin: 4px 0 0;
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-secondary);
  line-height: 1.45;
}

.empty-host-hint {
  margin-top: 4px;
  font-size: var(--ki-fs-xs);
}

.run-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding-top: 8px;
  border-top: 1px solid var(--ki-border-subtle);
}

.safety-note {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--ki-fs-xs);
}

@media (max-width: 1100px) {
  .jobs-grid {
    grid-template-columns: 1fr;
  }
}
</style>
