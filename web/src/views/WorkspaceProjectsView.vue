<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { FolderKanban, Plus, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'
import { useScopeStore, type ProjectItem } from '@/stores/scope'

interface ProjectRole {
  role_id: string
  role_name: string
  description: string
  permissions: string[]
}

const PROJECT_PERMS = [
  { code: 'assets:read', label: '资产库查看' },
  { code: 'assets:write', label: '资产与拓扑维护' },
  { code: 'config:read', label: '配置集查看' },
  { code: 'config:write', label: '配置版本发布' },
  { code: 'config:secret:reveal', label: '敏感密文查阅' },
  { code: 'deploy.job.read', label: '发布记录查看' },
  { code: 'deploy.job.submit', label: '发起发布作业' },
  { code: 'deploy.job.observe', label: '实时日志观测' },
  { code: 'deploy.job.cancel', label: '中止运行作业' },
]

const router = useRouter()
const scope = useScopeStore()
const projects = ref<ProjectItem[]>([])
const selectedProject = ref<ProjectItem | null>(null)
const roles = ref<ProjectRole[]>([])
const permVer = ref(1)
const editPerms = ref<string[]>([])
const loading = ref(false)
const createDlg = ref(false)
const form = ref({ code: '', name: '', description: '' })

async function loadProjects() {
  loading.value = true
  try {
    const { data } = await api.get<{ items: ProjectItem[] }>('/api/v1/iam/projects')
    projects.value = data.items || []
    if (projects.value.length > 0) {
      const target = projects.value.find((p) => p.project_id === selectedProject.value?.project_id) || projects.value[0]
      await selectProject(target)
    } else {
      selectedProject.value = null
      roles.value = []
    }
  } finally {
    loading.value = false
  }
}

async function selectProject(p: ProjectItem) {
  selectedProject.value = p
  const { data } = await api.get<{ items: ProjectRole[]; permissionVersion: number }>(
    `/api/v1/iam/projects/${p.project_id}/roles`
  )
  roles.value = data.items || []
  permVer.value = data.permissionVersion || p.permission_version
  editPerms.value = [...(roles.value[0]?.permissions || [])]
}

async function savePerms() {
  if (!selectedProject.value || roles.value.length === 0) return
  try {
    const { data } = await api.put<{ permissionVersion: number }>(
      `/api/v1/iam/projects/${selectedProject.value.project_id}/roles/${roles.value[0].role_id}/permissions`,
      { permissions: editPerms.value },
      { ifMatch: permVer.value }
    )
    permVer.value = data.permissionVersion
    ElMessage.success('项目角色权限已更新并立即生效')
    await loadProjects()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function createProject() {
  try {
    await api.post('/api/v1/iam/projects', form.value)
    ElMessage.success('项目创建成功')
    createDlg.value = false
    form.value = { code: '', name: '', description: '' }
    await Promise.all([loadProjects(), scope.refreshMe()])
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

async function enterProject(pid: string) {
  await scope.switchProject(pid)
  router.push('/project/overview')
}

onMounted(loadProjects)
watch(() => [scope.tenantId, scope.appId], loadProjects)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="项目管理"
      description="按环境或业务子域划分独立项目工作台，精细配置各项目角色的操作权限范围。"
      :icon="FolderKanban"
      :badge="`${projects.length} 个项目`"
    >
      <template #actions>
        <el-button :loading="loading" @click="loadProjects"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="createDlg = true"><Plus :size="14" /><span class="ks-gap">新建项目</span></el-button>
      </template>
    </KsBanner>

    <div class="ks-two-col">
      <div class="ki-card">
        <div class="ki-card__title"><span>项目列表</span></div>
        <el-table :data="projects" size="small" highlight-current-row @row-click="selectProject">
          <el-table-column label="项目名称" min-width="170">
            <template #default="{ row }">
              <div class="ks-cell-main">{{ row.name }}</div>
              <div class="ks-cell-sub ki-mono">{{ row.code }}</div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button size="small" type="primary" @click.stop="enterProject(row.project_id)">进入工作台</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>角色权限配置（{{ selectedProject?.name || '-' }}）</span>
          <span class="ki-badge ks-pill-blue">版本 v{{ permVer }}</span>
        </div>
        <div v-if="roles.length > 0" class="ks-perm-box">
          <div class="ks-perm-role">
            <ShieldCheck :size="15" />
            <span>{{ roles[0].role_name }}</span>
          </div>
          <el-checkbox-group v-model="editPerms" class="ks-check-grid">
            <el-checkbox v-for="item in PROJECT_PERMS" :key="item.code" :value="item.code">
              {{ item.label }}
            </el-checkbox>
          </el-checkbox-group>
          <div class="ks-perm-foot">
            <el-button type="primary" @click="savePerms">保存权限配置</el-button>
          </div>
        </div>
        <div v-else class="ki-muted">请选择左侧项目查看角色权限</div>
      </div>
    </div>

    <el-dialog v-model="createDlg" title="新建项目" width="440px">
      <el-form label-position="top">
        <el-form-item label="项目标识" required><el-input v-model="form.code" placeholder="例如: dr-standby" /></el-form-item>
        <el-form-item label="项目名称" required><el-input v-model="form.name" placeholder="例如: 异地容灾区" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="createProject">确认创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-two-col {
  display: grid;
  grid-template-columns: 1.1fr 1fr;
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
}

.ks-cell-main {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-cell-sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-perm-box {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ks-perm-role {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-check-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.ks-perm-foot {
  display: flex;
  justify-content: flex-end;
  padding-top: 12px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}
</style>
