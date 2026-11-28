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

const ALL_PROJECT_PERMS = [
  'assets:read',
  'assets:write',
  'config:read',
  'config:write',
  'config:secret:reveal',
  'deploy.job.read',
  'deploy.job.submit',
  'deploy.job.observe',
  'deploy.job.cancel',
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
    ElMessage.success(`项目角色权限已原子全量替换 (If-Match CAS -> permissionVersion v${data.permissionVersion})`)
    await loadProjects()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e))
  }
}

async function createProject() {
  try {
    await api.post('/api/v1/iam/projects', form.value)
    ElMessage.success('项目已创建')
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
      title="项目治理与原子权限编排"
      description="对齐 LiveOps 企业/项目级授权模型：通过 If-Match 携带 permissionVersion 对项目角色权限执行全量原子替换，并递增全局 scopeVersion。"
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
        <div class="ki-card__title"><span>项目列表 ({{ scope.appId }})</span></div>
        <el-table :data="projects" size="small" stripe highlight-current-row @row-click="selectProject">
          <el-table-column prop="name" label="项目名称" min-width="140" />
          <el-table-column prop="code" label="编码" width="130"><template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template></el-table-column>
          <el-table-column label="权限版本" width="90"><template #default="{ row }"><span class="ki-mono">v{{ row.permission_version }}</span></template></el-table-column>
          <el-table-column label="操作" width="110">
            <template #default="{ row }">
              <el-button size="small" type="primary" @click.stop="enterProject(row.project_id)">进入工作台</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <div class="ki-card">
        <div class="ki-card__title">
          <span>项目角色权限 CAS 编排 ({{ selectedProject?.name || '-' }})</span>
          <span class="ki-badge ks-pill-blue ki-mono">If-Match: "{{ permVer }}"</span>
        </div>
        <div v-if="roles.length > 0" class="ks-perm-box">
          <div class="ks-perm-role">
            <ShieldCheck :size="15" />
            <span>{{ roles[0].role_name }} ({{ roles[0].role_id }})</span>
          </div>
          <el-checkbox-group v-model="editPerms" class="ks-check-grid">
            <el-checkbox v-for="pc in ALL_PROJECT_PERMS" :key="pc" :value="pc">
              <span class="ki-mono">{{ pc }}</span>
            </el-checkbox>
          </el-checkbox-group>
          <div class="ks-perm-foot">
            <el-button type="primary" @click="savePerms">原子替换权限 (CAS)</el-button>
          </div>
        </div>
        <div v-else class="ki-muted">请选择左侧项目查看角色权限</div>
      </div>
    </div>

    <el-dialog v-model="createDlg" title="新建项目" width="440px">
      <el-form label-position="top">
        <el-form-item label="项目编码" required><el-input v-model="form.code" placeholder="例如: dr-standby" /></el-form-item>
        <el-form-item label="项目名称" required><el-input v-model="form.name" placeholder="例如: 异地容灾区" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createDlg = false">取消</el-button>
        <el-button type="primary" @click="createProject">创建项目</el-button>
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
  grid-template-columns: 1.15fr 1fr;
  gap: var(--ki-space-4);
}

@media (max-width: 1024px) {
  .ks-two-col {
    grid-template-columns: 1fr;
  }
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
  gap: 8px;
}

.ks-perm-foot {
  display: flex;
  justify-content: flex-end;
  padding-top: 10px;
  border-top: 1px solid var(--ki-border-subtle);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}
</style>
