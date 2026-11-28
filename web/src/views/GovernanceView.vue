<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { KeyRound, Plus, ShieldCheck, Trash2, UserPlus } from 'lucide-vue-next'
import { apiRequest } from '@/api/client'
import { useWorkspaceStore } from '@/stores/workspace'
import PageHeader from '@/components/PageHeader.vue'
import KiStatusBadge from '@/components/ki/KiStatusBadge.vue'

interface UserRow {
  id: string
  username: string
  displayName: string
  email: string
  role: string
  enabled: boolean
  createdAt: string
}

interface PATRow {
  id: string
  name: string
  tokenPrefix: string
  role: string
  revoked: boolean
  createdAt: string
  plainToken?: string
}

interface CredRow {
  id: string
  name: string
  kind: string
  username: string
  description: string
  createdBy: string
  createdAt: string
}

interface AuditRow {
  id: string
  actor: string
  actorRole: string
  action: string
  targetType: string
  detail: string
  hash: string
  createdAt: string
}

const ws = useWorkspaceStore()
const loading = ref(false)
const activeTab = ref<'iam' | 'creds' | 'audit' | 'prefs'>('iam')

const users = ref<UserRow[]>([])
const tokens = ref<PATRow[]>([])
const creds = ref<CredRow[]>([])
const audits = ref<AuditRow[]>([])
const verifyRes = ref<{ valid: boolean; totalCount: number; reason: string } | null>(null)

const showUserDialog = ref(false)
const userForm = reactive({ username: '', displayName: '', email: '', role: 'operator', password: '' })

const showPATDialog = ref(false)
const patForm = reactive({ name: 'CI/CD Pipeline Token', role: 'operator', ttlDays: 90 })
const issuedPAT = ref('')

const showCredDialog = ref(false)
const credForm = reactive({ name: '', kind: 'ssh_password', username: 'root', secret: '', passphrase: '', description: '' })

async function loadAll() {
  loading.value = true
  try {
    const [uRes, tRes, cRes, aRes] = await Promise.all([
      apiRequest<{ items: UserRow[] }>('GET', '/api/v1/iam/users'),
      apiRequest<{ items: PATRow[] }>('GET', '/api/v1/iam/tokens'),
      apiRequest<{ items: CredRow[] }>('GET', '/api/v1/iam/credentials'),
      apiRequest<{ items: AuditRow[] }>('GET', '/api/v1/audit/logs?limit=100'),
    ])
    users.value = uRes.data.items || []
    tokens.value = tRes.data.items || []
    creds.value = cRes.data.items || []
    audits.value = aRes.data.items || []
  } finally {
    loading.value = false
  }
}

async function createUser() {
  try {
    await apiRequest('POST', '/api/v1/iam/users', { body: userForm })
    ElMessage.success('成员账号已创建')
    showUserDialog.value = false
    Object.assign(userForm, { username: '', displayName: '', email: '', role: 'operator', password: '' })
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '创建用户失败')
  }
}

async function createPAT() {
  const { data } = await apiRequest<PATRow>('POST', '/api/v1/iam/tokens', { body: patForm })
  issuedPAT.value = data.plainToken || ''
  showPATDialog.value = false
  ElMessage.success('个人 API 令牌已签发（仅展示一次，请妥善保存）')
  await loadAll()
}

async function revokePAT(id: string) {
  await apiRequest('POST', `/api/v1/iam/tokens/${id}/revoke`)
  ElMessage.warning('令牌已吊销')
  await loadAll()
}

async function createCred() {
  try {
    await apiRequest('POST', '/api/v1/iam/credentials', { body: credForm })
    ElMessage.success('凭据已通过 KMS 信封加密存储')
    showCredDialog.value = false
    Object.assign(credForm, { name: '', kind: 'ssh_password', username: 'root', secret: '', passphrase: '', description: '' })
    await loadAll()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '录入失败')
  }
}

async function deleteCred(id: string) {
  await apiRequest('DELETE', `/api/v1/iam/credentials/${id}`)
  ElMessage.success('凭据已删除')
  await loadAll()
}

async function verifyAuditChain() {
  const { data } = await apiRequest<{ valid: boolean; totalCount: number; reason: string }>('POST', '/api/v1/audit/verify')
  verifyRes.value = data
  if (data.valid) ElMessage.success(`审计链完整性校验通过：全部 ${data.totalCount} 条哈希连续无篡改`)
  else ElMessage.error(`审计链发现篡改风险: ${data.reason}`)
}

onMounted(loadAll)
</script>

<template>
  <div class="gov-page">
    <PageHeader
      title="权限审计、凭据保险箱与平台设置"
      subtitle="Default-Deny RBAC · KMS 加密凭据 · SHA-256 审计哈希链"
      :refresh="loadAll"
      :refreshing="loading"
    />

    <div class="ki-card">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="用户账号与 API 令牌" name="iam">
          <div class="table-heading">
            <div class="section-title">平台账号与角色 ({{ users.length }})</div>
            <div class="table-actions">
              <el-button size="small" @click="showPATDialog = true"><KeyRound :size="13" class="btn-ico" />签发 API 令牌</el-button>
              <el-button type="primary" size="small" @click="showUserDialog = true"><UserPlus :size="13" class="btn-ico" />新增账号</el-button>
            </div>
          </div>

          <el-alert v-if="issuedPAT" :title="`新签发令牌（仅显示一次）: ${issuedPAT}`" type="success" show-icon class="pat-alert" @close="issuedPAT = ''" />

          <el-table :data="users" size="small">
            <el-table-column prop="username" label="用户名" width="140" />
            <el-table-column prop="displayName" label="姓名" width="140" />
            <el-table-column prop="role" label="RBAC 角色" width="110">
              <template #default="{ row }"><el-tag size="small">{{ row.role }}</el-tag></template>
            </el-table-column>
            <el-table-column prop="email" label="邮箱" min-width="170" />
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <KiStatusBadge :status="row.enabled ? 'ok' : 'cancelled'" :label="row.enabled ? '启用' : '停用'" />
              </template>
            </el-table-column>
          </el-table>

          <div class="section-title">个人访问令牌 PAT ({{ tokens.length }})</div>
          <el-table :data="tokens" size="small" empty-text="尚未签发 API 令牌">
            <el-table-column prop="name" label="令牌名称" min-width="160" />
            <el-table-column prop="tokenPrefix" label="前缀" width="150">
              <template #default="{ row }"><code class="ki-mono">{{ row.tokenPrefix }}…</code></template>
            </el-table-column>
            <el-table-column prop="role" label="授权角色" width="100" />
            <el-table-column label="状态" width="90">
              <template #default="{ row }">
                <KiStatusBadge :status="row.revoked ? 'failed' : 'ok'" :label="row.revoked ? '已吊销' : '有效'" />
              </template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="right">
              <template #default="{ row }">
                <el-button v-if="!row.revoked" link type="danger" size="small" @click="revokePAT(row.id)">吊销</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="`KMS 凭据保险箱 (${creds.length})`" name="creds">
          <div class="table-heading">
            <span class="ki-muted">所有 SSH 密码、私钥与镜像仓口令均采用 AES-256-GCM / SM4-GCM 信封加密存储，列表永不回显明文。</span>
            <el-button type="primary" size="small" @click="showCredDialog = true"><Plus :size="13" class="btn-ico" />录入加密凭据</el-button>
          </div>
          <el-table :data="creds" size="small" empty-text="尚未录入 SSH 或镜像仓库凭据">
            <el-table-column prop="name" label="凭据名称" min-width="150" />
            <el-table-column prop="kind" label="类型" width="130" />
            <el-table-column prop="username" label="用户名" width="120" />
            <el-table-column prop="description" label="用途说明" min-width="160" />
            <el-table-column label="操作" width="76" align="right">
              <template #default="{ row }">
                <el-button link type="danger" size="small" @click="deleteCred(row.id)"><Trash2 :size="12" /></el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="`防篡改审计哈希链 (${audits.length})`" name="audit">
          <div class="table-heading">
            <div>
              <el-tag v-if="verifyRes" :type="verifyRes.valid ? 'success' : 'danger'">
                {{ verifyRes.valid ? `哈希链完整有效 (${verifyRes.totalCount} 条连续校验通过)` : `告警: ${verifyRes.reason}` }}
              </el-tag>
            </div>
            <el-button type="primary" size="small" @click="verifyAuditChain">
              <ShieldCheck :size="14" class="btn-ico" />执行全链防篡改校验
            </el-button>
          </div>

          <el-table :data="audits" size="small">
            <el-table-column prop="createdAt" label="时间" width="175" />
            <el-table-column prop="actor" label="操作者" width="95" />
            <el-table-column prop="action" label="动作" width="165">
              <template #default="{ row }"><code class="ki-mono">{{ row.action }}</code></template>
            </el-table-column>
            <el-table-column prop="detail" label="审计详情" min-width="220" show-overflow-tooltip />
            <el-table-column label="链式 SHA-256" width="135">
              <template #default="{ row }"><code class="ki-mono">{{ row.hash.slice(0, 12) }}…</code></template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="主题与显示偏好" name="prefs">
          <div class="ki-setting-list">
            <div class="ki-setting-row">
              <div class="ki-setting-row__info">
                <span class="ki-setting-row__label">界面主题外观 (WCAG 2.2 AA)</span>
                <span class="ki-setting-row__desc">默认采用清晰浅色工作台，支持一键切换暗色模式</span>
              </div>
              <div class="ki-setting-row__control">
                <el-radio-group :model-value="ws.themePreference" @update:model-value="(v: unknown) => ws.setTheme(v as 'light' | 'dark' | 'system')">
                  <el-radio-button value="light">浅色默认</el-radio-button>
                  <el-radio-button value="dark">深色护眼</el-radio-button>
                  <el-radio-button value="system">跟随系统</el-radio-button>
                </el-radio-group>
              </div>
            </div>
            <div class="ki-setting-row">
              <div class="ki-setting-row__info">
                <span class="ki-setting-row__label">表格信息密度</span>
                <span class="ki-setting-row__desc">大批量资产盘点时可切换紧凑行高提升同屏吞吐</span>
              </div>
              <div class="ki-setting-row__control">
                <el-radio-group v-model="ws.density">
                  <el-radio-button value="comfortable">舒适</el-radio-button>
                  <el-radio-button value="compact">紧凑</el-radio-button>
                </el-radio-group>
              </div>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <el-dialog v-model="showUserDialog" title="新增平台账号" width="440px">
      <el-form label-position="top">
        <el-form-item label="用户名"><el-input v-model="userForm.username" /></el-form-item>
        <el-form-item label="显示名称"><el-input v-model="userForm.displayName" /></el-form-item>
        <el-form-item label="RBAC 角色">
          <el-select v-model="userForm.role" style="width: 100%">
            <el-option label="系统管理员 (admin)" value="admin" />
            <el-option label="运维工程师 (operator)" value="operator" />
            <el-option label="安全审计员 (auditor)" value="auditor" />
            <el-option label="只读访客 (viewer)" value="viewer" />
          </el-select>
        </el-form-item>
        <el-form-item label="初始密码 (不少于 8 位)"><el-input v-model="userForm.password" type="password" show-password /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showUserDialog = false">取消</el-button>
        <el-button type="primary" @click="createUser">创建账号</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showPATDialog" title="签发个人 API 令牌 (PAT)" width="420px">
      <el-form label-position="top">
        <el-form-item label="令牌名称"><el-input v-model="patForm.name" /></el-form-item>
        <el-form-item label="有效期 (天)"><el-input-number v-model="patForm.ttlDays" :min="1" :max="365" style="width: 100%" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showPATDialog = false">取消</el-button>
        <el-button type="primary" @click="createPAT">立即签发</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showCredDialog" title="录入 KMS 加密凭据" width="460px">
      <el-form label-position="top">
        <el-form-item label="凭据名称"><el-input v-model="credForm.name" placeholder="prod-linux-root" /></el-form-item>
        <el-form-item label="凭据类型">
          <el-select v-model="credForm.kind" style="width: 100%">
            <el-option label="SSH 密码 (ssh_password)" value="ssh_password" />
            <el-option label="SSH 私钥 (ssh_key)" value="ssh_key" />
            <el-option label="OCI 仓库账密 (registry_basic)" value="registry_basic" />
          </el-select>
        </el-form-item>
        <el-form-item label="登录用户名"><el-input v-model="credForm.username" /></el-form-item>
        <el-form-item :label="credForm.kind === 'ssh_key' ? '私钥 PEM 内容' : '密码 / Secret'">
          <el-input v-model="credForm.secret" :type="credForm.kind === 'ssh_key' ? 'textarea' : 'password'" :rows="4" show-password />
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="credForm.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCredDialog = false">取消</el-button>
        <el-button type="primary" @click="createCred">加密保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.btn-ico {
  margin-right: 4px;
}

.pat-alert {
  margin-bottom: 12px;
}
</style>
