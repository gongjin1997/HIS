<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import KsBanner from '@/components/ks/KsBanner.vue'
import { api, ApiError } from '@/api/client'

interface UserRow {
  user_id: string
  username: string
  display_name: string
  email: string
  status: string
  failed_attempts: number
  roles: string[]
}

interface RoleRow {
  role_id: string
  code: string
  name: string
  scope_level: string
  description: string
  permissions: string[]
}

interface PolicyRow {
  policy_id: string
  method: string
  path_pattern: string
  required_perm: string
  scope_level: string
  enabled: boolean
  description: string
}

const activeTab = ref('users')
const loading = ref(false)
const keyword = ref('')
const users = ref<UserRow[]>([])
const roles = ref<RoleRow[]>([])
const policies = ref<PolicyRow[]>([])
const userDlg = ref(false)
const userForm = ref({ username: '', displayName: '', email: '', password: '', roleId: 'role-workspace-op' })

const filteredUsers = computed(() => {
  const q = keyword.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(
    (u) => u.username.toLowerCase().includes(q) || u.display_name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q)
  )
})

function scopeLabel(lv: string) {
  return lv === 'PLATFORM' ? '平台级' : lv === 'WORKSPACE' ? '企业空间级' : '项目级'
}

async function load() {
  loading.value = true
  try {
    const [u, r, p] = await Promise.all([
      api.get<{ items: UserRow[] }>('/api/v1/iam/users'),
      api.get<{ items: RoleRow[] }>('/api/v1/iam/roles'),
      api.get<{ items: PolicyRow[] }>('/api/v1/iam/route-policies'),
    ])
    users.value = u.data.items || []
    roles.value = r.data.items || []
    policies.value = p.data.items || []
  } finally {
    loading.value = false
  }
}

async function createUser() {
  try {
    await api.post('/api/v1/iam/users', userForm.value)
    ElMessage.success('账号创建成功')
    userDlg.value = false
    userForm.value = { username: '', displayName: '', email: '', password: '', roleId: 'role-workspace-op' }
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError ? e.message : String(e))
  }
}

onMounted(load)
</script>

<template>
  <div class="ks-page">
    <KsBanner
      title="访问控制"
      description="集中管理平台成员账号、角色权限模板与接口默认拒绝访问策略，保障最小权限原则。"
      :icon="ShieldCheck"
      :tabs="[
        { key: 'users', label: '用户账号', count: users.length },
        { key: 'roles', label: '角色管理', count: roles.length },
        { key: 'policies', label: '接口鉴权规则', count: policies.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="userDlg = true"><Plus :size="14" /><span class="ks-gap">创建用户</span></el-button>
      </template>
    </KsBanner>

    <div v-if="activeTab === 'users'" class="ki-card">
      <div class="ks-bar">
        <el-input v-model="keyword" placeholder="按用户名、姓名或邮箱搜索..." clearable style="max-width: 300px" />
      </div>
      <el-table :data="filteredUsers" size="small">
        <el-table-column label="用户" min-width="180">
          <template #default="{ row }">
            <div class="ks-cell-main">{{ row.display_name }}</div>
            <div class="ks-cell-sub ki-mono">{{ row.username }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="email" label="联系邮箱" min-width="190" />
        <el-table-column label="绑定角色" min-width="180">
          <template #default="{ row }">
            <span v-for="rc in row.roles || []" :key="rc" class="ki-badge ks-pill-blue ks-mr">{{ rc }}</span>
          </template>
        </el-table-column>
        <el-table-column label="登录状态" width="120">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.status === 'ACTIVE' ? 'ks-pill-ok' : 'ks-pill-danger'">
              {{ row.status === 'ACTIVE' ? '正常' : '已禁用' }}
            </span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else-if="activeTab === 'roles'" class="ki-card">
      <el-table :data="roles" size="small">
        <el-table-column label="角色名称" width="200">
          <template #default="{ row }">
            <div class="ks-cell-main">{{ row.name }}</div>
            <div class="ks-cell-sub ki-mono">{{ row.code }}</div>
          </template>
        </el-table-column>
        <el-table-column label="管理层级" width="120">
          <template #default="{ row }">{{ scopeLabel(row.scope_level) }}</template>
        </el-table-column>
        <el-table-column prop="description" label="职责说明" min-width="220" />
        <el-table-column label="权限项数" width="110">
          <template #default="{ row }">
            <span class="ki-badge ks-pill-neutral">
              {{ row.permissions?.includes('*') ? '全部权限' : `${row.permissions?.length || 0} 项权限` }}
            </span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else class="ki-card">
      <el-table :data="policies" size="small">
        <el-table-column prop="method" label="方法" width="90">
          <template #default="{ row }"><span class="ki-badge ks-pill-blue ki-mono">{{ row.method }}</span></template>
        </el-table-column>
        <el-table-column prop="path_pattern" label="受控接口路径" min-width="230">
          <template #default="{ row }"><span class="ki-mono">{{ row.path_pattern }}</span></template>
        </el-table-column>
        <el-table-column prop="required_perm" label="所需权限标识" width="200">
          <template #default="{ row }"><span class="ki-mono">{{ row.required_perm }}</span></template>
        </el-table-column>
        <el-table-column label="作用域" width="110">
          <template #default="{ row }">{{ scopeLabel(row.scope_level) }}</template>
        </el-table-column>
        <el-table-column prop="description" label="策略说明" min-width="180" />
      </el-table>
    </div>

    <el-dialog v-model="userDlg" title="创建平台用户" width="460px">
      <el-form label-position="top">
        <el-form-item label="登录用户名" required><el-input v-model="userForm.username" placeholder="例如: deploy-ops" /></el-form-item>
        <el-form-item label="姓名"><el-input v-model="userForm.displayName" placeholder="例如: 张工" /></el-form-item>
        <el-form-item label="邮箱"><el-input v-model="userForm.email" placeholder="ops@hospital.local" /></el-form-item>
        <el-form-item label="初始密码（8-32位，需包含大小写字母、数字和特殊符号）" required>
          <el-input v-model="userForm.password" type="password" show-password placeholder="例如: OpsPass#2026!" />
        </el-form-item>
        <el-form-item label="分配角色">
          <el-select v-model="userForm.roleId" style="width: 100%">
            <el-option v-for="r in roles" :key="r.role_id" :label="`${r.name} (${r.code})`" :value="r.role_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDlg = false">取消</el-button>
        <el-button type="primary" @click="createUser">确认创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.ks-gap {
  margin-left: 6px;
}

.ks-mr {
  margin-right: 4px;
}

.ks-bar {
  margin-bottom: var(--ki-space-3);
}

.ks-cell-main {
  font-weight: var(--ki-fw-semibold);
  color: var(--ki-text-primary);
}

.ks-cell-sub {
  font-size: var(--ki-fs-xs);
  color: var(--ki-text-muted);
}

.ks-pill-ok {
  background-color: var(--ki-status-ok-subtle);
  border: 1px solid var(--ki-status-ok-border);
  color: var(--ki-status-ok-text);
}

.ks-pill-blue {
  background-color: var(--ki-status-running-subtle);
  border: 1px solid var(--ki-status-running-border);
  color: var(--ki-status-running-text);
}

.ks-pill-danger {
  background-color: var(--ki-status-danger-subtle);
  border: 1px solid var(--ki-status-danger-border);
  color: var(--ki-status-danger-text);
}

.ks-pill-neutral {
  background-color: var(--ki-status-neutral-subtle);
  border: 1px solid var(--ki-status-neutral-border);
  color: var(--ki-status-neutral-text);
}
</style>
