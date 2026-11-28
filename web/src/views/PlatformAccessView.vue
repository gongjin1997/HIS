<script setup lang="ts">
import { onMounted, ref } from 'vue'
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
const users = ref<UserRow[]>([])
const roles = ref<RoleRow[]>([])
const policies = ref<PolicyRow[]>([])
const userDlg = ref(false)
const userForm = ref({ username: '', displayName: '', email: '', password: '', roleId: 'role-workspace-op' })

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
    ElMessage.success('账号创建成功（已通过 8-32 位四类密码复杂度校验与 Argon2id 哈希）')
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
      title="账号、角色与 Default-Deny 路由策略"
      description="统一身份认证底座：Argon2id 口令存储、3/6/9 次渐进锁定、动态 ScopeResolver 实时权限收敛与默认拒绝路由策略表。"
      :icon="ShieldCheck"
      badge="Default-Deny RBAC"
      :tabs="[
        { key: 'users', label: '平台账号', count: users.length },
        { key: 'roles', label: '角色与权限集', count: roles.length },
        { key: 'policies', label: '路由鉴权策略', count: policies.length },
      ]"
      :active-tab="activeTab"
      @update:active-tab="activeTab = $event"
    >
      <template #actions>
        <el-button :loading="loading" @click="load"><RefreshCw :size="14" /><span class="ks-gap">刷新</span></el-button>
        <el-button type="primary" @click="userDlg = true"><Plus :size="14" /><span class="ks-gap">新建账号</span></el-button>
      </template>
    </KsBanner>

    <div v-if="activeTab === 'users'" class="ki-card">
      <el-table :data="users" size="small" stripe>
        <el-table-column prop="username" label="用户名" width="140">
          <template #default="{ row }"><span class="ki-mono">{{ row.username }}</span></template>
        </el-table-column>
        <el-table-column prop="display_name" label="显示名称" width="150" />
        <el-table-column prop="email" label="邮箱" min-width="180" />
        <el-table-column label="绑定角色" min-width="180">
          <template #default="{ row }">
            <span v-for="rc in row.roles || []" :key="rc" class="ki-badge ks-pill-blue ks-mr">{{ rc }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="failed_attempts" label="连续失败" width="100" />
        <el-table-column prop="status" label="状态" width="110">
          <template #default="{ row }">
            <span class="ki-badge" :class="row.status === 'ACTIVE' ? 'ks-pill-ok' : 'ks-pill-danger'">{{ row.status }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else-if="activeTab === 'roles'" class="ki-card">
      <el-table :data="roles" size="small" stripe>
        <el-table-column prop="code" label="角色编码" width="180">
          <template #default="{ row }"><span class="ki-mono">{{ row.code }}</span></template>
        </el-table-column>
        <el-table-column prop="name" label="角色名称" width="160" />
        <el-table-column prop="scope_level" label="层级" width="120" />
        <el-table-column label="授权权限码" min-width="320">
          <template #default="{ row }">
            <div class="ks-perm-wrap">
              <span v-for="p in row.permissions || []" :key="p" class="ki-badge ks-pill-neutral ki-mono">{{ p }}</span>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else class="ki-card">
      <el-table :data="policies" size="small" stripe>
        <el-table-column prop="method" label="HTTP 方法" width="100">
          <template #default="{ row }"><span class="ki-badge ks-pill-blue ki-mono">{{ row.method }}</span></template>
        </el-table-column>
        <el-table-column prop="path_pattern" label="路径模式" min-width="220">
          <template #default="{ row }"><span class="ki-mono">{{ row.path_pattern }}</span></template>
        </el-table-column>
        <el-table-column prop="required_perm" label="必需权限码" width="210">
          <template #default="{ row }"><span class="ki-mono">{{ row.required_perm }}</span></template>
        </el-table-column>
        <el-table-column prop="scope_level" label="作用域" width="110" />
        <el-table-column prop="description" label="策略说明" min-width="180" />
      </el-table>
    </div>

    <el-dialog v-model="userDlg" title="新建平台账号" width="460px">
      <el-form label-position="top">
        <el-form-item label="用户名" required><el-input v-model="userForm.username" placeholder="例如: deploy-ops" /></el-form-item>
        <el-form-item label="显示名称"><el-input v-model="userForm.displayName" placeholder="例如: 张工" /></el-form-item>
        <el-form-item label="邮箱"><el-input v-model="userForm.email" placeholder="ops@hospital.local" /></el-form-item>
        <el-form-item label="初始密码 (8-32位，含大小写+数字+特殊符号)" required>
          <el-input v-model="userForm.password" type="password" show-password placeholder="例如: OpsPass#2026!" />
        </el-form-item>
        <el-form-item label="初始角色">
          <el-select v-model="userForm.roleId" style="width: 100%">
            <el-option v-for="r in roles" :key="r.role_id" :label="`${r.name} (${r.code})`" :value="r.role_id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userDlg = false">取消</el-button>
        <el-button type="primary" @click="createUser">创建账号</el-button>
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

.ks-perm-wrap {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
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
